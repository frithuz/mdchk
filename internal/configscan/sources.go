package configscan

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// Git source discovery intentionally does not execute Git or arbitrary includes.
// Relevant unsupported configuration fails explicitly instead of claiming parity.
func (s *scanner) sources() ([]ignoreSource, error) {
	env := s.options.Environment
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS"} {
		if env[key] != "" {
			return nil, fmt.Errorf("unsupported Git environment override %s", key)
		}
	}
	for dir := s.options.Root; ; dir = filepath.Dir(dir) {
		if err := s.check(); err != nil {
			return nil, err
		}
		if err := s.options.Budget.ChargeWork(int64(len(dir) + 1)); err != nil {
			return nil, err
		}
		marker := filepath.Join(dir, ".git")
		info, err := s.access.lstat(marker)
		if err == nil {
			s.repository = dir
			if info.IsDir() {
				s.gitdir = marker
			} else if info.Mode().IsRegular() {
				data, e := s.read(marker)
				if e != nil {
					return nil, e
				}
				value, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
				if !ok || value == "" {
					return nil, errors.New("invalid Git worktree marker")
				}
				s.gitdir = resolvePath(dir, value)
			} else {
				return nil, errors.New("git marker must be a nonsymlink directory or gitfile")
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if s.repository == "" {
		return nil, nil
	}
	s.common = s.gitdir
	data, err := s.read(filepath.Join(s.gitdir, "commondir"))
	if err == nil {
		value := strings.TrimSpace(string(data))
		if value == "" {
			return nil, errors.New("empty Git commondir")
		}
		s.common = resolvePath(s.gitdir, value)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	settings := map[string]gitSetting{}
	configs := []string{}
	noSystem := false
	if value := env["GIT_CONFIG_NOSYSTEM"]; value != "" {
		noSystem, err = gitBool(value)
		if err != nil {
			return nil, err
		}
	}
	if !noSystem {
		system := env["GIT_CONFIG_SYSTEM"]
		if system == "" {
			system = "/etc/gitconfig"
		}
		configs = append(configs, system)
	}
	home := env["HOME"]
	xdg := env["XDG_CONFIG_HOME"]
	if xdg == "" && home != "" {
		xdg = filepath.Join(home, ".config")
	}
	if global, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		if global != "" {
			configs = append(configs, global)
		}
	} else {
		if xdg != "" {
			configs = append(configs, filepath.Join(xdg, "git", "config"))
		}
		if home != "" {
			configs = append(configs, filepath.Join(home, ".gitconfig"))
		}
	}
	configs = append(configs, filepath.Join(s.common, "config"))
	for _, path := range configs {
		if err := s.gitConfig(path, settings); err != nil {
			return nil, err
		}
	}
	if setting := settings["extensions.worktreeconfig"]; setting.value != "" {
		enabled, e := gitBool(setting.value)
		if e != nil {
			return nil, setting.failure("extensions.worktreeconfig", e)
		}
		if enabled {
			if err := s.gitConfig(filepath.Join(s.gitdir, "config.worktree"), settings); err != nil {
				return nil, err
			}
		}
	}
	if setting := settings["core.ignorecase"]; setting.value != "" {
		s.ignoreCase, err = gitBool(setting.value)
		if err != nil {
			return nil, setting.failure("core.ignorecase", err)
		}
	}
	excludesSetting, explicitExcludes := settings["core.excludesfile"]
	excludes := excludesSetting.value
	if !explicitExcludes && xdg != "" {
		excludes = filepath.Join(xdg, "git", "ignore")
	}
	var sources []ignoreSource
	if excludes != "" {
		if strings.HasPrefix(excludes, "~/") {
			if home == "" {
				return nil, excludesSetting.failure("core.excludesfile", errors.New("core.excludesFile needs HOME"))
			}
			excludes = filepath.Join(home, excludes[2:])
		}
		if !filepath.IsAbs(excludes) {
			return nil, excludesSetting.failure("core.excludesfile", errors.New("relative core.excludesFile is unsupported; use an absolute or ~/ path"))
		}
		source, present, e := s.load(excludes, "")
		if e != nil {
			return nil, e
		}
		if present {
			sources = append(sources, source)
		}
	}
	source, present, err := s.load(filepath.Join(s.common, "info", "exclude"), "")
	if err != nil {
		return nil, err
	}
	if present {
		sources = append(sources, source)
	}
	// Load parent sources incrementally: a child source cannot reopen a parent
	// directory that has already been pruned.
	if s.options.Root != s.repository {
		rel, _ := filepath.Rel(s.repository, s.options.Root)
		dir := s.repository
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			source, present, e := s.load(filepath.Join(dir, ".gitignore"), s.logical(dir))
			if e != nil {
				return nil, e
			}
			if present {
				sources = append(sources, source)
			}
			dir = filepath.Join(dir, part)
			skip, ignored, e := s.ignored(dir, true, sources)
			if e != nil {
				return nil, e
			}
			if ignored {
				s.result.Skipped = append(s.result.Skipped, skip)
				return nil, errRootIgnored
			}
		}
	}
	return sources, nil
}

var errRootIgnored = errors.New("scan root is Git ignored")

func resolvePath(base, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(base, value)
}

func gitBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1", "":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("invalid Git boolean %q", value)
}
func (s *scanner) gitConfig(path string, settings map[string]gitSetting) (resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = Failure{Path: path, Operation: "git configuration", Role: IgnoreSource, Err: resultErr}
		}
	}()
	data, err := s.read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("git config %s: %w", path, err)
	}
	section := ""
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return fmt.Errorf("malformed Git config %s", path)
			}
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if strings.HasPrefix(section, "include") {
				return fmt.Errorf("git config includes unsupported in %s", path)
			}
			continue
		}
		key, value, hasValue := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if section != "core" && section != "extensions" {
			continue
		}
		full := section + "." + key
		if full != "core.ignorecase" && full != "core.excludesfile" && full != "extensions.worktreeconfig" {
			continue
		}
		if !hasValue {
			value = "true"
		}
		value = strings.TrimSpace(value)
		if strings.Contains(value, "\\\n") || strings.HasSuffix(value, "\\") {
			return fmt.Errorf("git config continuation unsupported in %s", path)
		}
		if strings.HasPrefix(value, "\"") {
			if strings.ContainsAny(value, "\n\r") {
				return fmt.Errorf("unsupported Git config string in %s", path)
			}
			for i := 1; i < len(value)-1; i++ {
				if value[i] == '\\' {
					i++
					if i >= len(value)-1 || !strings.ContainsRune("ntb\\\"", rune(value[i])) {
						return fmt.Errorf("unsupported Git config escape in %s", path)
					}
				}
			}
			decoded, e := strconv.Unquote(value)
			if e != nil {
				return fmt.Errorf("unsupported Git config quoting in %s", path)
			}
			value = decoded
		} else {
			if i := strings.IndexAny(value, "#;"); i >= 0 {
				value = strings.TrimSpace(value[:i])
			}
			if strings.ContainsAny(value, "\"\\") {
				return fmt.Errorf("unsupported Git config escaping in %s", path)
			}
		}
		settings[full] = gitSetting{value: value, path: path}
	}
	return nil
}

// GitSettingError retains the winning setting source and key after precedence
// resolution, including semantic failures discovered after parsing all sources.
type GitSettingError struct {
	Path, Key string
	Err       error
}

func (e *GitSettingError) Error() string { return fmt.Sprintf("%s %s: %v", e.Path, e.Key, e.Err) }
func (e *GitSettingError) Unwrap() error { return e.Err }

type gitSetting struct{ value, path string }

func (s gitSetting) failure(key string, err error) error {
	return Failure{Path: s.path, Operation: "git configuration", Role: IgnoreSource, Err: &GitSettingError{s.path, key, err}}
}
