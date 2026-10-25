package resource

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// filesystemReceipt is an operation/key-bound native identity/type proof, not an
// open handle or a snapshot of content that was never requested. It deliberately
// excludes size and mtime: a later first content read may observe newer bytes of
// the same object, subject to that read's existing stability checks.
type filesystemReceipt struct {
	operation *Operation
	key       Key
	existence Existence
	identity  string
}

var errFilesystemReceiptChanged = errors.New("native resource identity or type changed after its first observation")

// readReceipt is the service's native capability upgrade seam. It does not alter
// raw Read semantics when prior is zero. A prior proof is usable only for this
// operation and logical key; selected snapshots require no native receipt. The
// acquisition compares identity/type before opening or reading content and keeps
// its own exact-name/no-follow/stability/cleanup guarantees. No handles survive.
func (r *FilesystemReader) readReceipt(ctx context.Context, request Request, prior filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
	if r == nil || r.operation == nil || ctx == nil || request.view.operation != r.operation || request.identity == nil {
		return ObservationSpec{}, prior, ErrContract
	}
	if prior != (filesystemReceipt{}) && (prior.operation != r.operation || prior.key != request.Key() || prior.identity == "" || prior.existence != File && prior.existence != Directory || request.Class() != Local) {
		return ObservationSpec{}, prior, ErrContract
	}
	receipt := prior
	out, err := r.read(ctx, request, &receipt)
	return out, receipt, err
}

// retainFilesystemReceipt keeps already established existence separate from a
// failed upgrade. Incompatibility is an acquisition failure, not a new absence or
// a replacement first fact. Other real I/O and independent stops remain intact.
func retainFilesystemReceipt(prior filesystemReceipt, lookup filesystemLookup, err error) (filesystemLookup, error) {
	if prior.identity == "" {
		return lookup, err
	}
	incompatible := lookup.policy || lookup.existence == Missing || lookup.identity != "" && (lookup.identity != prior.identity || lookup.existence != prior.existence)
	if incompatible {
		var fault *FilesystemReadError
		if errors.As(err, &fault) {
			err = &FilesystemReadError{IO: errors.Join(errFilesystemReceiptChanged, fault.IO), Stop: fault.Stop}
		} else if _, stop := InterruptionProblem(err); stop {
			err = &FilesystemReadError{IO: errFilesystemReceiptChanged, Stop: err}
		} else {
			err = &FilesystemReadError{IO: errors.Join(errFilesystemReceiptChanged, err)}
		}
		lookup.policy = false
		lookup.source, lookup.validated = parser.Source{}, ContentEvidence{}
	}
	lookup.existence, lookup.identity = prior.existence, prior.identity
	return lookup, err
}
