package config

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrUnavailable means raw JSON configuration decoding is not implemented. It is distinct
// from invalid user configuration; errors.Is recognizes this development boundary.
var ErrUnavailable = errors.New("raw JSON configuration decoding is not implemented")

// DecodeJSON is the future standalone supplied-object adapter. Raw bytes keep
// duplicate names and invalid Unicode available for validation before decoding.
// It currently rejects every input and performs no filesystem access.
func DecodeJSON(content []byte) (*Definition, error) {
	return nil, ErrUnavailable
}

// Load reads one explicitly named nonsymlink regular YAML file and decodes its
// syntax. It does not establish operation scope, validate domains or resolve
// policies. Operation adapters must use Resolve for bounded selection/admission.
func Load(path string) (*Definition, error) {
	if path == "" {
		return nil, pathError(path, errors.New("configuration path is empty"))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, pathError(path, err)
	}
	base := filepath.VolumeName(absolute) + string(filepath.Separator)
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, pathError(path, err)
	}
	defer closeReadHandle(root)
	access := &rootAccess{root: root, base: base}
	raw := path
	if !filepath.IsAbs(raw) {
		cwd, e := os.Getwd()
		if e != nil {
			return nil, pathError(path, e)
		}
		raw = cwd + string(filepath.Separator) + raw
	}
	if err := checkExplicitComponents(access, raw, base); err != nil {
		return nil, pathError(absolute, err)
	}
	content, err := access.readRegular(absolute)
	if err != nil {
		return nil, pathError(absolute, err)
	}
	definition, err := DecodeYAML(absolute, content)
	return definition, sourceError(err, []SourceSnapshot{snapshotSource(absolute, content)})
}
