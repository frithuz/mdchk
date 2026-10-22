package cli

import (
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/frithuz/mdchk/internal/result"
)

// githubPaths is trusted presentation authority, never inferred from findings.
// Root and CWD must be absolute in the explicitly selected flavor (unix/windows).
// Mapping is lexical: callers establish repository identity; no files are opened.
// The zero value deliberately renders all findings without a source location.
type githubPaths struct{ Root, CWD, Flavor string }
type githubPathsKey struct{}
type githubBoundKey struct{}

func withGitHubPaths(ctx context.Context, paths githubPaths) context.Context {
	return context.WithValue(ctx, githubPathsKey{}, paths)
}

func githubPath(ctx context.Context, name string) (string, error) {
	authority, _ := ctx.Value(githubPathsKey{}).(githubPaths)
	bound, ok := ctx.Value(githubBoundKey{}).(int64)
	if !ok {
		bound = limitDefaults["response-bytes"]
	}
	// Refuse optional location detail before allocation. Scalars are scanned with
	// cancellation checkpoints even when they cannot be mapped under the bound.
	for _, text := range []string{authority.Root, authority.CWD, name} {
		for i := 0; i < len(text); i += 1024 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		if int64(len(text)) > bound {
			return "", nil
		}
	}
	root, cwd := authority.Root, authority.CWD
	if name == "" || root == "" || cwd == "" {
		return "", ctx.Err()
	}
	windows := authority.Flavor == "windows"
	if !windows && authority.Flavor != "unix" {
		return "", ctx.Err()
	}
	volume := func(s string) (string, string) {
		if len(s) >= 3 && s[1] == ':' && s[2] == '/' && (s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z') {
			return s[:2], s[2:]
		}
		if strings.HasPrefix(s, "//") {
			parts := strings.SplitN(s[2:], "/", 3)
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				rest := "/"
				if len(parts) == 3 {
					rest += parts[2]
				}
				return "//" + parts[0] + "/" + parts[1], rest
			}
		}
		return "", s
	}
	var rootVolume string
	if windows {
		root = strings.ReplaceAll(root, `\`, "/")
		cwd = strings.ReplaceAll(cwd, `\`, "/")
		name = strings.ReplaceAll(name, `\`, "/")
		var cwdVolume string
		rootVolume, root = volume(root)
		cwdVolume, cwd = volume(cwd)
		if rootVolume == "" || !strings.EqualFold(rootVolume, cwdVolume) {
			return "", ctx.Err()
		}
		v, rest := volume(name)
		if v != "" {
			if !strings.EqualFold(rootVolume, v) {
				return "", ctx.Err()
			}
			name = rest
		} else if strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
			return "", ctx.Err()
		}
	} else {
		// Foreign drive and UNC spelling is ambiguous under Unix; a literal Unix
		// backslash elsewhere remains a filename byte, never a directory separator.
		if len(name) >= 3 && name[1] == ':' && (name[2] == '/' || name[2] == '\\') || strings.HasPrefix(name, `\\`) || strings.HasPrefix(name, "//") {
			return "", ctx.Err()
		}
	}
	if !strings.HasPrefix(root, "/") || !strings.HasPrefix(cwd, "/") {
		return "", ctx.Err()
	}
	root, cwd = path.Clean(root), path.Clean(cwd)
	if !strings.HasPrefix(name, "/") {
		name = cwd + "/" + name
	}
	name = path.Clean(name)
	prefix := strings.TrimSuffix(root, "/") + "/"
	if len(name) < len(prefix) {
		return "", ctx.Err()
	}
	inside := strings.HasPrefix(name, prefix)
	if windows {
		inside = strings.EqualFold(name[:len(prefix)], prefix)
	}
	if !inside {
		return "", ctx.Err()
	}
	relative := name[len(prefix):]
	if relative == "" || relative == "." {
		return "", ctx.Err()
	}
	return relative, ctx.Err()
}

// GitHub's percent codec is separate from visible terminal safety. Protocol
// CR/LF preserve multiline data; other controls and bidi are visibly spelled.
// Human fields additionally neutralize both modern and legacy command openers.
func githubField(ctx context.Context, w io.Writer, s string, property, human bool) error {
	var chunk [1024]byte
	n := 0
	flush := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if n > 0 {
			if _, err := w.Write(chunk[:n]); err != nil {
				return err
			}
		}
		n = 0
		return nil
	}
	for _, r := range s {
		var encoded string
		switch {
		case human && r == ':':
			encoded = `\x3a`
		case human && r == '[':
			encoded = `\x5b`
		case !human && r == '%':
			encoded = "%25"
		case !human && r == '\r':
			encoded = "%0D"
		case !human && r == '\n':
			encoded = "%0A"
		case !human && property && r == ':':
			encoded = "%3A"
		case !human && property && r == ',':
			encoded = "%2C"
		case vscodeBidiControl(r):
			encoded = fmt.Sprintf(`\u%04x`, r)
		case terminalControl(r):
			encoded = textField(string(r))
		default:
			encoded = string(r)
		}
		if n+len(encoded) > len(chunk) {
			if err := flush(); err != nil {
				return err
			}
		}
		n += copy(chunk[n:], encoded)
	}
	return flush()
}

func renderGitHub(ctx context.Context, stdout, stderr io.Writer, r result.Result) error {
	annotation := func(name string, at *result.Position, title string, parts ...string) error {
		mapped, err := githubPath(ctx, name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(stdout, "::error "); err != nil {
			return err
		}
		if mapped != "" && at != nil && at.Line > 0 {
			if _, err := io.WriteString(stdout, "file="); err != nil {
				return err
			}
			if err := githubField(ctx, stdout, mapped, true, false); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(stdout, ",line=%d,", at.Line); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(stdout, "title="); err != nil {
			return err
		}
		if err := githubField(ctx, stdout, title, true, false); err != nil {
			return err
		}
		if _, err := io.WriteString(stdout, "::"); err != nil {
			return err
		}
		// A proved target without a source line still has useful identity.
		// Keep it in data rather than a file-only property, whose interpretation
		// could imply a default source line in downstream consumers.
		if mapped != "" && (at == nil || at.Line <= 0) {
			if err := githubField(ctx, stdout, mapped, false, false); err != nil {
				return err
			}
			if _, err := io.WriteString(stdout, ": "); err != nil {
				return err
			}
		}
		if name != "" && mapped == "" {
			parts = append([]string{"Location unavailable (outside repository or unproved path)."}, parts...)
		}
		for i, part := range parts {
			if i > 0 {
				if _, err := io.WriteString(stdout, " "); err != nil {
					return err
				}
			}
			if err := githubField(ctx, stdout, part, false, false); err != nil {
				return err
			}
		}
		_, err = io.WriteString(stdout, "\n")
		return err
	}
	human := func(parts ...string) error {
		for i, part := range parts {
			if i > 0 {
				if _, err := io.WriteString(stderr, " "); err != nil {
					return err
				}
			}
			if err := githubField(ctx, stderr, part, false, true); err != nil {
				return err
			}
		}
		_, err := io.WriteString(stderr, "\n")
		return err
	}
	for _, d := range r.Diagnostics {
		if err := annotation(d.Path, &d.Range.Start, d.Rule, d.Message); err != nil {
			return err
		}
	}
	for _, e := range r.Errors {
		var at *result.Position
		if e.Range != nil {
			at = &e.Range.Start
		}
		if err := annotation(e.Path, at, e.Code, string(e.Category), e.Message); err != nil {
			return err
		}
	}
	for _, c := range r.Coverage {
		var at *result.Position
		if c.Range != nil {
			at = &c.Range.Start
		}
		if err := annotation(c.Path, at, "coverage", c.Rule, c.Reason, c.Message); err != nil {
			return err
		}
	}
	for _, s := range r.Interruptions {
		parts := []string{s.Phase, s.Reason, s.Message}
		if s.Limit != nil {
			parts = append(parts, "limit", s.Limit.Name, "observed", strconv.FormatInt(s.Limit.Observed, 10), "allowed", strconv.FormatInt(s.Limit.Allowed, 10), s.Limit.Unit)
		}
		if err := annotation(s.Path, nil, "interrupted", parts...); err != nil {
			return err
		}
	}
	for _, o := range r.Omissions {
		count := "unknown"
		if o.Count != nil {
			count = strconv.Itoa(*o.Count)
		}
		if err := annotation(o.Path, nil, "omitted", count, o.Section, o.Reason, o.Message); err != nil {
			return err
		}
	}
	partial := r.Operation == "fix" && (!r.Complete || len(r.Errors) > 0 || len(r.Interruptions) > 0)
	for _, f := range r.Files {
		if partial || f.WriteStatus == "failed" || f.WriteStatus == "unknown" || f.WriteStatus == "refused" {
			if err := human("outcome", f.Path, f.WriteStatus); err != nil {
				return err
			}
		}
		for _, name := range f.Leftovers {
			if err := human("owned staging leftover", f.Path, name); err != nil {
				return err
			}
		}
	}
	if !r.Complete {
		if err := annotation("", nil, "incomplete", "Requested processing is incomplete."); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func encodeGitHub(ctx context.Context, out result.Result, bound int64) (encodedReport, error) {
	return encodeEditor(context.WithValue(ctx, githubBoundKey{}, bound), out, bound, renderGitHub)
}
