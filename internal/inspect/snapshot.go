// Package inspect acquires and projects immutable inspection data independently
// of command reporting and presentation filtering.
package inspect

import (
	"context"
	"crypto/sha1" //nolint:gosec // G505: required descriptive checksum; never used for authentication.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/parser"
)

// Options separates invocation-relative explicit configuration from discovery
// at the canonical target parent. BoundaryDirectory optionally narrows discovery.
// The caller's budget is shared by acquisition, configuration and future parsing.
type Options struct {
	WorkingDirectory, BoundaryDirectory, ConfigPath, Version string
	Budget                                                   *parser.OperationBudget
	TotalContentBytes, ConfigBytes, TotalConfigBytes         int64
	StructuredDepth                                          int
	Registry                                                 *engine.Registry
	Providers                                                engine.ValidationProviders
	Capabilities                                             config.Capabilities
}

// File identifies exactly one acquired immutable byte snapshot.
type File struct {
	Path, Version, SizeDisplay, SHA1, SHA256 string
	SizeBytes                                int64
}

// Exclusion distinguishes unresolved configuration from a known selection
// decision. Origins are the definitions that exclude the explicitly acquired file.
type Exclusion struct {
	Known, Excluded bool
	Origins         []config.Origin
}

// Snapshot owns immutable source and configuration authority. Returned views may
// be changed without changing later readers. Disk changes after acquisition have
// no effect. A configuration observation never fabricates default settings.
type Snapshot struct {
	budget                  *parser.OperationBudget
	file                    File
	source                  parser.Source
	lines                   parser.LineIndex
	selection               *config.Selection
	capabilities            config.Capabilities
	plan                    *config.ExecutionPlan
	configErr, admissionErr error
	exclusion               Exclusion
}

func (s *Snapshot) File() File              { return s.file }
func (s *Snapshot) Source() parser.Source   { return s.source }
func (s *Snapshot) Lines() parser.LineIndex { return s.lines }
func (s *Snapshot) Configuration() *config.Configuration {
	if s.selection == nil {
		return nil
	}
	return s.selection.Configuration(s.capabilities)
}
func (s *Snapshot) Sources() []config.SourceSnapshot {
	if s.selection != nil {
		return s.selection.Sources()
	}
	var acquired *config.SourceError
	if errors.As(s.configErr, &acquired) {
		return acquired.Sources()
	}
	return nil
}
func (s *Snapshot) ConfigurationError() error { return s.configErr }
func (s *Snapshot) AdmissionError() error     { return s.admissionErr }
func (s *Snapshot) Exclusion() Exclusion {
	out := s.exclusion
	out.Origins = slices.Clone(out.Origins)
	for i := range out.Origins {
		if p := out.Origins[i].Position; p != nil {
			copy := *p
			out.Origins[i].Position = &copy
		}
	}
	return out
}

// Acquire performs one bounded acquisition attempt. Target IO failures return no
// snapshot. Configuration failures remain observations; interruption returns the
// metadata already acquired together with its typed cause. It never writes.
func Acquire(ctx context.Context, target string, options Options) (*Snapshot, error) {
	if ctx == nil {
		return nil, errors.New("nil inspection context")
	}
	budget := options.Budget
	if budget == nil {
		var err error
		budget, err = parser.NewOperationBudget(parser.DefaultLimits())
		if err != nil {
			return nil, err
		}
	}
	check := func(n int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return budget.ChargeWork(n)
	}
	if err := check(0); err != nil {
		return nil, err
	}
	path := target
	if !filepath.IsAbs(path) && options.WorkingDirectory != "" {
		// Joining lexically would clean symlink/.. before filesystem resolution.
		path = options.WorkingDirectory + string(filepath.Separator) + path
	}
	acquired, err := files.AcquireExplicit(ctx, path, files.AcquireOptions{DocumentBytes: budget.Limits().DocumentBytes, TotalContentBytes: options.TotalContentBytes, Budget: budget})
	if err != nil {
		return nil, err
	}
	if len(acquired.Errors) > 0 {
		return nil, acquired.Errors[0]
	}
	if len(acquired.PreflightErrors) > 0 {
		return nil, acquired.PreflightErrors[0]
	}
	if len(acquired.Interruptions) > 0 {
		return nil, acquired.Interruptions[0].Err
	}
	if len(acquired.Documents) != 1 {
		return nil, errors.New("inspection acquired no target")
	}
	document := acquired.Documents[0]
	size := document.Source.Len()
	// Reserve all checksum, encoding/index scans, byte copies and worst-case line
	// records before materialization. Physical lines are at most one per byte.
	if err := check(32*int64(size) + 1024); err != nil {
		return nil, err
	}
	if err := budget.ChargeNodes(int64(size) + 1); err != nil {
		return nil, err
	}
	data := document.Source.Bytes()
	//nolint:gosec // G401: inspection requires a descriptive SHA-1 checksum, not a security decision.
	shaOne := sha1.Sum(data)
	shaTwo := sha256.Sum256(data)
	version := options.Version
	if version == "" {
		version = "development"
	}
	s := &Snapshot{budget: budget, file: File{Path: document.Path, Version: version, SizeBytes: int64(size), SizeDisplay: displaySize(int64(size)), SHA1: hex.EncodeToString(shaOne[:]), SHA256: hex.EncodeToString(shaTwo[:])}, source: document.Source, lines: parser.NewLineIndex(document.Source), capabilities: options.Capabilities}
	if err := check(0); err != nil {
		return s, err
	}
	selection, err := config.ResolveValidatedBounded(config.SelectionOptions{WorkingDirectory: options.WorkingDirectory, StartDirectory: filepath.Dir(document.Path), BoundaryDirectory: options.BoundaryDirectory, ExplicitPath: options.ConfigPath}, options.Registry, options.Providers, config.DiscoveryBudget{Context: ctx, Budget: budget, SourceBytes: options.ConfigBytes, TotalBytes: options.TotalConfigBytes, StructuredDepth: options.StructuredDepth})
	if err != nil {
		s.configErr = err
		if interrupted(err) {
			return s, err
		}
		return s, nil
	}
	// Reserve owned configuration/provenance/permission views and admission work
	// from same-read source sizes plus the fixed catalog before copying them.
	var work int64 = 262144
	for _, source := range selection.Sources() {
		work += 128 * source.SizeBytes
	}
	if err := check(work); err != nil {
		return s, err
	}
	s.selection = selection
	origins, err := selection.ExclusionOriginsBounded(ctx, budget, document.Path)
	if err != nil {
		return s, err
	}
	s.exclusion = Exclusion{Known: true, Excluded: len(origins) > 0, Origins: origins}
	admissionCapabilities := options.Capabilities
	if represent := admissionCapabilities.RepresentIndexFile; represent != nil {
		admissionCapabilities.RepresentIndexFile = func(name string) error {
			if err := check(int64(len(name)) + 1); err != nil {
				return err
			}
			err := represent(name)
			if err != nil {
				return err
			}
			return check(0)
		}
	}
	resolution, admissionErr := selection.Admit(admissionCapabilities)
	s.admissionErr = admissionErr
	if resolution != nil {
		s.plan = resolution.ExecutionPlan()
	}
	if err := check(0); err != nil {
		return s, err
	}
	if interrupted(s.admissionErr) {
		return s, s.admissionErr
	}
	return s, nil
}
func interrupted(err error) bool {
	var limit *parser.LimitExceeded
	return errors.As(err, &limit) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
func displaySize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d bytes (%d bytes)", n, n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB"}
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == "GiB" {
			return fmt.Sprintf("%.1f %s (%d bytes)", value, unit, n)
		}
	}
	return ""
}

// ExecutionPlan returns the immutable admitted bridge, when admission succeeded.
// Capability errors do not erase the separate validated Configuration view.
func (s *Snapshot) ExecutionPlan() *config.ExecutionPlan { return s.plan }

// Boundary returns the canonical configuration discovery scope when resolved.
func (s *Snapshot) Boundary() (config.Boundary, bool) {
	if s.selection == nil {
		return config.Boundary{}, false
	}
	return s.selection.Boundary, true
}
