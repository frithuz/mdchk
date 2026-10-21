//go:build !unix

package files

// os.Root rejects Windows device paths. The common adapter checks both the
// pre-open and opened file identity and rejects nonregular files.
const acquireOpenFlags = 0
