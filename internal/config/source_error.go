package config

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
)

// SourceSnapshot identifies the exact bytes successfully acquired for a
// configuration definition. It contains no content and requires no later IO.
type SourceSnapshot struct {
	Path      string
	SHA256    string
	SizeBytes int64
}

// SourceError retains acquisition identities for a failed configuration
// selection. Unwrap preserves the original decoding, validation or IO error;
// its origins remain authoritative for which snapshots locate the failure.
// Sources includes acquired ancestors so governing policy origins can also be
// attributed. An unread or missing file never receives an invented snapshot.
type SourceError struct {
	err     error
	sources []SourceSnapshot
}

func (e *SourceError) Error() string { return e.err.Error() }
func (e *SourceError) Unwrap() error { return e.err }

// Sources returns owned metadata in acquisition order. Mutating the returned
// slice does not alter the error's retained identity.
func (e *SourceError) Sources() []SourceSnapshot { return slices.Clone(e.sources) }

func snapshotSource(path string, content []byte) SourceSnapshot {
	sum := sha256.Sum256(content)
	return SourceSnapshot{Path: path, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(content))}
}

func sourceError(err error, sources []SourceSnapshot) error {
	if err == nil || len(sources) == 0 {
		return err
	}
	return &SourceError{err: err, sources: slices.Clone(sources)}
}
