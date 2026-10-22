package files

import (
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// ContentLedger is a single-owner operation allowance shared by successive
// bounded reads and already observed Sources. Exhaustion is sticky. It does not
// replace the operation's work/node/time budget or grant source/write authority.
type ContentLedger struct {
	limit, used int64
	failure     *parser.LimitExceeded
}

func NewContentLedger(limit int64) (*ContentLedger, error) {
	if limit < 1 || limit > 1<<30 {
		return nil, errors.New("unsupported aggregate content allowance")
	}
	return &ContentLedger{limit: limit}, nil
}

// Check admits an upcoming observation before publication or retention.
func (l *ContentLedger) Check(size int64) error {
	if l == nil || size < 0 {
		return errors.New("invalid content admission")
	}
	if l.failure != nil {
		return l.failure
	}
	if size > l.limit-l.used {
		l.failure = &parser.LimitExceeded{Name: "total-content-bytes", Observed: l.used + size, Allowed: l.limit}
		return l.failure
	}
	return nil
}

// Reserve retains an observed Source against the same aggregate as fresh reads.
func (l *ContentLedger) Reserve(size int64) error {
	if err := l.Check(size); err != nil {
		return err
	}
	l.used += size
	return nil
}
