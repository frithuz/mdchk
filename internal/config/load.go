package config

import "errors"

// ErrUnavailable means configuration loading is not implemented. It is distinct
// from invalid user configuration; errors.Is recognizes this development boundary.
var ErrUnavailable = errors.New("configuration loading is not implemented")

// DecodeJSON is the future standalone supplied-object adapter. Raw bytes keep
// duplicate names and invalid Unicode available for validation before decoding.
// It currently rejects every input and performs no filesystem access.
func DecodeJSON(content []byte) (*Definition, error) {
	return nil, ErrUnavailable
}

// Load is the future explicit-file loader boundary. It currently performs no IO
// and always fails, even for an empty or nonexistent path. Discovery, policy,
// capability validation and missing-file default selection remain unimplemented.
func Load(path string) (*Definition, error) {
	return nil, ErrUnavailable
}
