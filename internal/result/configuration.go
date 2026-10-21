package result

// Provenance follows the schema-v1 vocabulary. Pointer is an RFC 6901 JSON
// Pointer into effective configuration, not a dotted diagnostic property path.
// Range is optional; a known source point can use identical start/end positions.
type Provenance struct {
	Pointer    string `json:"pointer"`
	SourceKind string `json:"source_kind"`
	Path       string `json:"path,omitempty"`
	Range      *Range `json:"range,omitempty"`
	Inherited  bool   `json:"inherited"`
}
type ConfigurationSource struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// ConfigurationData is the shared schema-v1 Configuration record. Its producer
// owns Effective and all nested collections; no file/hash evidence is inferred.
type ConfigurationData struct {
	Effective  map[string]any        `json:"effective"`
	Sources    []ConfigurationSource `json:"sources"`
	Provenance []Provenance          `json:"provenance"`
}
