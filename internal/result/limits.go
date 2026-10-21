package result

// LimitUnit gives the canonical wire unit for a shared operation limit name.
func LimitUnit(name string) string {
	switch name {
	case "document-bytes", "config-bytes", "total-config-bytes", "total-content-bytes", "path-bytes":
		return "bytes"
	case "operation-time":
		return "nanoseconds"
	case "syntax-depth", "structured-input-depth", "path-depth":
		return "nested constructs"
	case "document-nodes", "total-nodes":
		return "nodes or source records"
	default:
		return "metered work units"
	}
}
