package resource

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

// RootMode distinguishes authority supplied by each operation adapter. There is
// deliberately no standalone filesystem mode.
type RootMode string

const (
	RootCLI     RootMode = "cli"
	RootInspect RootMode = "inspect"
	RootProject RootMode = "project"
)

// RootSelectionOptions contains trusted operation inputs, not link destinations
// or report paths. SelectedConfig is the already selected configuration filename;
// its contents are never loaded here. CanonicalTarget is the canonical path of an
// already acquired explicit inspection target, not permission to resolve a link.
type RootSelectionOptions struct {
	Mode             RootMode
	WorkingDirectory string
	CanonicalTarget  string
	StartupRoot      string
	SelectedConfig   string
	// PathLimits are operator-owned; zero selects the shared initial profile.
	PathLimits PathLimits
}

// ErrRootUnstable means selection did not establish a stable discovery directory.
// It must not be interpreted as proof that no repository exists.
var ErrRootUnstable = errors.New("resource root changed during selection")

// SelectFilesystemRoot selects an anchor; it neither opens resource contents nor
// grants a filesystem capability. The native adapter must independently pin the
// result with its strict no-follow backend. Trusted Unix CLI/startup anchors may resolve
// host aliases here; Windows anchors must already avoid reparses; this never applies to resource keys. Inspection requires its
// caller's already canonical target parent and never resolves the target again.
func SelectFilesystemRoot(ctx context.Context, budget *parser.OperationBudget, options RootSelectionOptions) (string, error) {
	if ctx == nil || budget == nil || budget.Limits().Validate() != nil {
		return "", ErrContract
	}
	limits := options.PathLimits
	if limits == (PathLimits{}) {
		limits = DefaultPathLimits()
	}
	if limits.Bytes <= 0 || limits.Bytes > 1<<30 || limits.Depth <= 0 || limits.Depth > 1<<20 {
		return "", ErrContract
	}
	check := func(work int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return budget.ChargeWork(work)
	}
	if err := check(0); err != nil {
		return "", err
	}
	var start string
	switch options.Mode {
	case RootProject:
		// A startup root is the full project authority even when a repository
		// or a different configuration lies inside or outside it.
		return rootAnchor(options.StartupRoot, false, limits, budget, check)
	case RootInspect:
		if int64(len(options.CanonicalTarget)) > limits.Bytes {
			return "", &parser.LimitExceeded{Name: "path-bytes", Observed: int64(len(options.CanonicalTarget)), Allowed: limits.Bytes}
		}
		if err := check(int64(len(options.CanonicalTarget))*4 + 1); err != nil {
			return "", err
		}
		if err := nativeValidateAnchor(options.CanonicalTarget); err != nil {
			return "", err
		}
		if !rootPathSyntax(options.CanonicalTarget) {
			return "", ErrContract
		}
		start = filepath.Dir(options.CanonicalTarget)
	case RootCLI:
		start = options.WorkingDirectory
		if start == "" {
			var err error
			start, err = os.Getwd()
			if err != nil {
				return "", err
			}
		}
	default:
		return "", ErrContract
	}
	start, err := rootAnchor(start, options.Mode == RootInspect, limits, budget, check)
	if err != nil {
		return "", err
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		found, err := rootRepositoryAt(dir, budget, check)
		if err != nil {
			return "", err
		}
		if found {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if options.SelectedConfig != "" {
		if err := check(int64(len(options.SelectedConfig))*4 + 1); err != nil {
			return "", err
		}
		// This is an already resolved configuration fact. First decide whether
		// its directory can own root authority, without probing an unrelated
		// location or contacting an unsupported external namespace.
		configDir := filepath.Dir(options.SelectedConfig)
		if rootContains(configDir, start) {
			if !rootPathSyntax(options.SelectedConfig) {
				return "", ErrContract
			}
			if err := nativeValidateAnchor(options.SelectedConfig); err != nil {
				return "", err
			}
			return rootAnchor(configDir, true, limits, budget, check)
		}
		// A selected configuration below or beside start contributes settings,
		// not root authority. Its directory is never reopened for this choice.
	}
	if err := check(0); err != nil {
		return "", err
	}
	return start, nil
}

func rootAnchor(path string, requireCanonical bool, limits PathLimits, budget *parser.OperationBudget, check func(int64) error) (string, error) {
	if int64(len(path)) > limits.Bytes {
		return "", &parser.LimitExceeded{Name: "path-bytes", Observed: int64(len(path)), Allowed: limits.Bytes}
	}
	if err := check(int64(len(path))*16 + 1); err != nil {
		return "", err
	}
	if err := nativeValidateAnchor(path); err != nil {
		return "", err
	}
	if !rootPathSyntax(path) {
		return "", ErrContract
	}
	if err := rootPathBound(path, limits); err != nil {
		return "", err
	}
	if err := budget.ChargeNodes(2*rootNativeDepth(path) + 2); err != nil {
		return "", err
	}
	// Unix host aliases may establish the trusted operation anchor, never a
	// root-relative resource key. Windows refuses reparses before even metadata
	// access: resolving a local junction here could otherwise contact a share.
	canonical := path
	if runtime.GOOS != "windows" {
		var err error
		canonical, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", errors.Join(err, check(0))
		}
	}
	if requireCanonical && canonical != path {
		return "", fmt.Errorf("%w: operation path must already be canonical", ErrContract)
	}
	if err := nativeValidateAnchor(canonical); err != nil {
		return "", err
	}
	if err := rootPathBound(canonical, limits); err != nil {
		return "", err
	}
	if err := check(0); err != nil {
		return "", err
	}
	file, err := nativeOpenAnchor(canonical)
	if err != nil {
		return "", errors.Join(err, check(0))
	}
	if err := check(0); err != nil {
		return "", errors.Join(err, file.Close())
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(statErr, closeErr, check(0)); err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: operation anchor is not a directory", ErrContract)
	}
	return canonical, nil
}

func rootRepositoryAt(dir string, budget *parser.OperationBudget, check func(int64) error) (found bool, outErr error) {
	marker := filepath.Join(dir, ".git")
	if err := check(int64(len(marker))*48 + 8); err != nil {
		return false, err
	}
	if err := budget.ChargeNodes(4*rootNativeDepth(dir) + 8); err != nil {
		return false, err
	}
	parent, err := nativeOpenAnchor(dir)
	if err != nil {
		return false, errors.Join(err, check(0))
	}
	defer func() { outErr = errors.Join(outErr, parent.Close()) }()
	if err := check(0); err != nil {
		return false, err
	}
	before, err := nativeIdentity(parent)
	if err != nil {
		return false, errors.Join(err, check(0))
	}
	if err := check(0); err != nil {
		return false, err
	}
	// This fixed marker probe has no content access. In particular, a symlink
	// cannot be resolved by an os.Root convenience method before type checks.
	markerMetadata, probeErr := nativeGitMetadataSnapshot(parent, ".git")
	markerInfo := markerMetadata.info
	if err := check(0); err != nil {
		return false, errors.Join(err, rootProbeFailure(probeErr))
	}
	after, identityErr := nativeIdentity(parent)
	if identityErr != nil {
		return false, errors.Join(identityErr, rootProbeFailure(probeErr), check(0))
	}
	if after != before {
		return false, errors.Join(ErrRootUnstable, rootProbeFailure(probeErr), check(0))
	}
	// The handle must still name the directory at this operation path. A
	// vanished/replaced parent is not evidence that its marker was absent.
	current, reopenErr := nativeOpenAnchor(dir)
	if reopenErr != nil {
		return false, errors.Join(ErrRootUnstable, reopenErr, rootProbeFailure(probeErr), check(0))
	}
	currentID, currentErr := nativeIdentity(current)
	closeErr := current.Close()
	if currentErr != nil || currentID != before {
		return false, errors.Join(ErrRootUnstable, currentErr, closeErr, rootProbeFailure(probeErr), check(0))
	}
	if err := errors.Join(closeErr, check(0)); err != nil {
		return false, errors.Join(err, rootProbeFailure(probeErr))
	}
	if rootOnlyNotExist(probeErr) {
		return false, nil
	}
	if probeErr != nil {
		return false, probeErr
	}
	if markerInfo.IsDir() || markerInfo.Mode().IsRegular() {
		return true, nil
	}
	return false, &fs.PathError{Op: "resource-root", Path: marker, Err: fmt.Errorf("%w: repository marker must be a directory or regular file", ErrContract)}
}

func rootProbeFailure(err error) error {
	if rootOnlyNotExist(err) {
		return nil
	}
	return err
}

// A joined native/close failure is not an absence assertion merely because
// one cause happens to be ENOENT. Keep every other actual failure visible.
func rootOnlyNotExist(err error) bool {
	if err == nil {
		return false
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		causes := group.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !rootOnlyNotExist(cause) {
				return false
			}
		}
		return true
	}
	if wrapper, ok := err.(interface{ Unwrap() error }); ok {
		return rootOnlyNotExist(wrapper.Unwrap())
	}
	return errors.Is(err, fs.ErrNotExist)
}

func rootPathSyntax(path string) bool {
	return path != "" && utf8.ValidString(path) && !strings.ContainsRune(path, 0) && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func rootPathBound(path string, limits PathLimits) error {
	if int64(len(path)) > limits.Bytes {
		return &parser.LimitExceeded{Name: "path-bytes", Observed: int64(len(path)), Allowed: limits.Bytes}
	}
	depth := rootNativeDepth(path)
	if depth > limits.Depth {
		return &parser.LimitExceeded{Name: "path-depth", Observed: depth, Allowed: limits.Depth}
	}
	return nil
}

func rootNativeDepth(path string) int64 {
	trimmed := strings.TrimPrefix(path, filepath.VolumeName(path))
	trimmed = strings.Trim(trimmed, string(filepath.Separator))
	if trimmed == "" {
		return 0
	}
	return int64(strings.Count(trimmed, string(filepath.Separator))) + 1
}

func rootContains(rootPath, target string) bool {
	rel, err := filepath.Rel(rootPath, target)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// FilesystemKey maps already acquired canonical paths into a selected namespace.
// It performs no I/O and is not proof of native spelling, type, identity or a safe
// open. The native adapter must provide that evidence. Outside targets and Git
// metadata have no resource key but may retain their selected self snapshots.
// A caller must never pass report IDs or unverified path strings as acquisition
// evidence, nor infer an enlarged root from a false result.
func FilesystemKey(rootPath, canonicalTarget string) (Key, bool, error) {
	if !rootPathSyntax(rootPath) || !rootPathSyntax(canonicalTarget) {
		return Key{}, false, ErrContract
	}
	if !rootContains(rootPath, canonicalTarget) {
		return Key{}, false, nil
	}
	rel, err := filepath.Rel(rootPath, canonicalTarget)
	if err != nil {
		return Key{}, false, err
	}
	if rel == "." {
		return Key{}, false, nil
	}
	logical := filepath.ToSlash(rel)
	for part := range strings.SplitSeq(logical, "/") {
		if part == ".git" {
			return Key{}, false, nil
		}
	}
	key, err := NewKey(logical)
	if err != nil {
		return Key{}, false, err
	}
	return key, true, nil
}
