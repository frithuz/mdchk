package resource

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"syscall"

	"github.com/frithuz/mdchk/internal/parser"
)

const serviceErrorNodes = 128
const serviceErrorDepth = 32
const serviceErrorEdges = 256
const serviceErrorBytes = 65536

// Supported mutable OS/limit carriers and standard fmt/join topology are
// detached. Unsupported custom errors are opaque frozen leaves: identity Is is
// preserved when comparable, but custom Is/As/Unwrap code is never delegated.
// Bounds and repeated-reference cuts keep the returned graph a finite tree.
type serviceErrorCopy struct {
	seen                      map[error]string
	nodes, work, bytes, edges int64
}
type frozenServiceLeaf struct {
	message  string
	identity error
}

func (e frozenServiceLeaf) Error() string { return e.message }
func (e frozenServiceLeaf) Is(target error) bool {
	return e.identity != nil && reflect.TypeOf(target) != nil && reflect.ValueOf(target).Comparable() && target == e.identity
}

type frozenServiceWrap struct {
	message  string
	child    error
	identity error
}

func (e frozenServiceWrap) Error() string { return e.message }
func (e frozenServiceWrap) Unwrap() error { return e.child }
func (e frozenServiceWrap) Is(target error) bool {
	return frozenServiceLeaf{identity: e.identity}.Is(target)
}

type frozenServiceGroup struct {
	message  string
	children []error
	identity error
}

func (e frozenServiceGroup) Error() string   { return e.message }
func (e frozenServiceGroup) Unwrap() []error { return append([]error(nil), e.children...) }
func (e frozenServiceGroup) Is(target error) bool {
	return frozenServiceLeaf{identity: e.identity}.Is(target)
}

func snapshotServiceError(err error) (int64, int64, error) {
	copy := serviceErrorCopy{seen: map[error]string{}}
	out := copy.visit(err, 0)
	return copy.nodes, copy.work, out
}
func (c *serviceErrorCopy) text(value string) string {
	n := min(int64(len(value)), serviceErrorBytes-c.bytes)
	c.bytes += n
	c.work += n
	if n < int64(len(value)) {
		return value[:n] + " [error metadata truncated]"
	}
	return value
}
func (c *serviceErrorCopy) visit(err error, depth int) error {
	if err == nil {
		return nil
	}
	c.work++
	if c.nodes >= serviceErrorNodes || depth >= serviceErrorDepth {
		return frozenServiceLeaf{message: "error metadata traversal bound"}
	}
	c.nodes++
	value := reflect.ValueOf(err)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return frozenServiceLeaf{message: "nil error metadata omitted"}
	}
	var identity error
	if reflect.ValueOf(err).Comparable() {
		identity = err
		if message, seen := c.seen[err]; seen {
			if message == "" {
				message = "cyclic error metadata omitted"
			}
			return frozenServiceLeaf{message: message, identity: identity}
		}
		c.seen[err] = ""
	}
	child := func(err error) error { return c.visit(err, depth+1) }
	requiredChild := func(err error) error {
		if err == nil {
			return frozenServiceLeaf{message: "nil error cause omitted"}
		}
		return child(err)
	}
	var out error
	switch e := err.(type) { //nolint:errorlint // Copy exact carrier nodes before any traversal; errors.As would follow cycles.
	case *os.PathError:
		out = &os.PathError{Op: c.text(e.Op), Path: c.text(e.Path), Err: requiredChild(e.Err)}
	case *os.LinkError:
		out = &os.LinkError{Op: c.text(e.Op), Old: c.text(e.Old), New: c.text(e.New), Err: requiredChild(e.Err)}
	case *os.SyscallError:
		out = &os.SyscallError{Syscall: c.text(e.Syscall), Err: requiredChild(e.Err)}
	case *parser.LimitExceeded:
		out = &parser.LimitExceeded{Name: c.text(e.Name), Observed: e.Observed, Allowed: e.Allowed}
	case syscall.Errno:
		out = e // immutable value; retain its standard sentinel Is behavior.
	case *filesystemDecodeError:
		out = e // private fieldless immutable acquisition sentinel.
	case *FilesystemReadError:
		out = &FilesystemReadError{IO: child(e.IO), Stop: child(e.Stop)}
	case *serviceReadError:
		// Canonical control records never compete with optional IO metadata for
		// traversal capacity. Their constant-size copy cannot become a cutoff marker.
		c.work += 4 // fixed-size private control record and fresh inspection carriers
		stop := e.controls.err()
		if !e.controls.present() {
			stop = child(e.stop)
		}
		out = &serviceReadError{io: child(e.io), stop: stop, controls: e.controls, causes: append([]serviceIOCause(nil), e.causes...)}
	case serviceIO:
		out = serviceIO{message: c.text(e.message), cause: child(e.cause)}
	case frozenServiceLeaf:
		out = frozenServiceLeaf{message: c.text(e.message), identity: e.identity}
	case frozenServiceWrap:
		out = frozenServiceWrap{message: c.text(e.message), identity: e.identity, child: child(e.child)}
	case frozenServiceGroup:
		children := c.children(e.children, depth)
		out = frozenServiceGroup{message: c.text(e.message), identity: e.identity, children: children}
	default:
		typ := reflect.TypeOf(err)
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.PkgPath() == "errors" && typ.Name() == "joinError" {
			// Standard joins compute Error by traversing children, so use the detached
			// finite children rather than invoke the possibly cyclic original method.
			group, _ := err.(interface{ Unwrap() []error })
			children := c.children(group.Unwrap(), depth)
			messages := make([]string, 0, len(children))
			for _, v := range children {
				messages = append(messages, c.text(v.Error()))
			}
			out = frozenServiceGroup{message: c.text(strings.Join(messages, "\n")), children: children, identity: identity}
		} else if typ.PkgPath() == "fmt" && typ.Name() == "wrapError" {
			wrapper, _ := err.(interface{ Unwrap() error })
			out = frozenServiceWrap{message: c.text(err.Error()), child: child(wrapper.Unwrap()), identity: identity}
		} else if typ.PkgPath() == "fmt" && typ.Name() == "wrapErrors" {
			group, _ := err.(interface{ Unwrap() []error })
			out = frozenServiceGroup{message: c.text(err.Error()), children: c.children(group.Unwrap(), depth), identity: identity}
		} else {
			out = frozenServiceLeaf{message: c.text(err.Error()), identity: identity}
		}
	}
	if identity != nil {
		c.seen[identity] = "repeated error metadata omitted"
	}
	return out
}
func (c *serviceErrorCopy) children(values []error, depth int) []error {
	n := min(len(values), serviceErrorNodes-int(c.nodes), serviceErrorEdges-int(c.edges))
	out := make([]error, 0, n+1)
	for _, v := range values {
		if c.edges >= serviceErrorEdges || c.nodes >= serviceErrorNodes {
			out = append(out, frozenServiceLeaf{message: "error metadata fanout bound"})
			break
		}
		c.edges++
		c.work++
		if v != nil {
			out = append(out, c.visit(v, depth+1))
		}
	}
	return out
}

func serviceCanonicalCause(err error) serviceIOCause {
	kind := ReadFailure
	var decode *filesystemDecodeError
	if errors.As(err, &decode) {
		kind = DecodeFailure
	} // detached finite tree only
	return serviceIOCause{kind: kind, message: err.Error()}
}
