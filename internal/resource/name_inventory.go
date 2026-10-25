package resource

import (
	"context"
	"errors"
	"io"
	"math/bits"
	"os"
	"sort"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
)

// NameFact is a literal authorized name and established type. It carries no
// source contents, lint selection, or site-build eligibility.
type NameFact struct {
	key       Key
	existence Existence
}

func (f NameFact) Key() Key             { return f.key }
func (f NameFact) Existence() Existence { return f.existence }

// NameInventory is an adapter-produced enumeration witness. Completeness is
// private and means all names below this exact directory/view were inspected;
// it does not mean those names are eligible MkDocs pages.
type NameInventory struct {
	source    *projectSourceState
	directory Location
	complete  bool
	facts     []NameFact
	problems  []Problem
}

func (i NameInventory) KnownCount() int { return len(i.facts) }
func (i NameInventory) KnownFact(index int) (NameFact, bool) {
	if index < 0 || index >= len(i.facts) {
		return NameFact{}, false
	}
	return i.facts[index], true
}
func (i NameInventory) KnownProblemCount() int { return len(i.problems) }
func (i NameInventory) KnownProblem(index int) (Problem, bool) {
	if index < 0 || index >= len(i.problems) {
		return Problem{}, false
	}
	return copyProblem(i.problems[index]), true
}
func (i NameInventory) Complete() bool { return i.source != nil && i.complete }
func (i NameInventory) Facts(ctx context.Context, p ProjectSource) ([]NameFact, error) {
	if i.source == nil || p.state != i.source {
		return nil, ErrContract
	}
	if err := i.source.service.Checkpoint(ctx, int64(len(i.facts))+1); err != nil {
		return nil, err
	}
	if err := i.source.service.operation.budget.ChargeNodes(int64(len(i.facts))); err != nil {
		return nil, err
	}
	return append([]NameFact(nil), i.facts...), nil
}
func (i NameInventory) Problems(ctx context.Context, p ProjectSource) ([]Problem, error) {
	if i.source == nil || p.state != i.source {
		return nil, ErrContract
	}
	if err := i.source.service.Checkpoint(ctx, int64(len(i.problems))+1); err != nil {
		return nil, err
	}
	if err := i.source.service.operation.budget.ChargeNodes(int64(len(i.problems))); err != nil {
		return nil, err
	}
	return append([]Problem(nil), i.problems...), nil
}

// Enumerate obtains a bounded complete-name witness from the registered adapter.
// It runs synchronously outside the service worker and uses the same native gate,
// original root, cumulative counters, caller controls and service lifetime.
func (p ProjectSource) Enumerate(ctx context.Context, directory Location) (out NameInventory, err error) {
	if p.state == nil || directory.root != p.state.view.Root() || !directory.rootDirectory && directory.key.value == "" {
		return out, ErrContract
	}
	s := p.state.service
	out.source = p.state
	out.directory = directory
	charge := func(work, nodes int64) error {
		if e := s.Checkpoint(ctx, work); e != nil {
			return e
		}
		return s.operation.budget.ChargeNodes(nodes)
	}
	if err = charge(1, 1); err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			out.complete = false
			out.problems = append(out.problems, Problem{Kind: PartialInventory, Message: "Inventory conflicts with the first observed resource type."})
		}
		if e := s.Checkpoint(ctx, 0); e != nil {
			err = errors.Join(err, e)
			out.complete = false
		}
	}()
	rootFact, rootErr := p.Inspect(ctx, directory, DirectoryType)
	if rootErr != nil {
		return out, rootErr
	}
	if rootFact.Existence != Directory {
		out.problems = []Problem{{Kind: PartialInventory, Message: "Inventory root is not an available directory."}}
		return out, nil
	}
	within := func(name string) bool {
		return directory.rootDirectory || strings.HasPrefix(name, directory.key.value+"/")
	}
	maxPath := 0
	add := func(key Key, kind Existence) error {
		maxPath = max(maxPath, len(key.value))
		if e := charge(int64(len(key.value))+128, 2); e != nil {
			return e
		}
		s.mu.Lock()
		if s.stop != nil || s.closed || s.ctx.Err() != nil {
			stopped := s.stop
			if stopped == nil {
				stopped = context.Canceled
			}
			s.mu.Unlock()
			return stopped
		}
		if !s.identities[key] {
			if int64(len(s.identities))+s.unkeyed >= s.limits.Identities {
				limit := &parser.LimitExceeded{Name: "resource-identities", Observed: int64(len(s.identities)) + s.unkeyed + 1, Allowed: s.limits.Identities}
				s.stop = limit
				s.mu.Unlock()
				return limit
			}
			s.identities[key] = true
		}
		cacheKey := serviceKey{key: key}
		if _, selected := s.selected[key]; selected {
			cacheKey.view = p.state.view.identity
		}
		if cached := s.entries[cacheKey]; cached != nil && cached.hasExistence && cached.existence.Existence != kind {
			out.complete = false
		}
		s.mu.Unlock()
		if len(out.facts) == cap(out.facts) {
			if e := charge(int64(len(out.facts))+1, int64(len(out.facts))+1); e != nil {
				return e
			}
		}
		out.facts = append(out.facts, NameFact{key, kind})
		return nil
	}
	problem := func(message string) error {
		if e := charge(int64(len(message))+1, 1); e != nil {
			return e
		}
		out.problems = append(out.problems, Problem{Kind: PartialInventory, Message: message})
		out.complete = false
		return nil
	}
	if s.supplied != nil {
		r := s.supplied
		out.complete = r.complete
		for name, fact := range r.entries {
			if err = charge(int64(len(name))+1, 0); err != nil {
				return out, err
			}
			if !within(name) {
				continue
			}
			key := Key{value: name}
			if hasGitComponent(key) {
				continue
			}
			if selected, ok := s.selected[key]; ok && p.state.view.targets[selected].unavailable {
				if err = problem("Selected saved resource is unavailable: " + name); err != nil {
					return out, err
				}
				continue
			}
			switch fact.kind {
			case EntryFile:
				err = add(key, File)
			case EntryDirectory:
				err = add(key, Directory)
			case EntryMissing:
				// Explicit absence is not a positive inventory member.
			case EntryUnavailable:
				err = problem("Supplied subtree is unavailable: " + name)
			}
			if err != nil {
				return out, err
			}
		}
	} else {
		f := s.filesystem.filesystem
		if err = f.enter(ctx); err != nil {
			return out, err
		}
		defer f.leave()
		anchor, e := f.cloneAnchor()
		if e != nil {
			return out, e
		}
		defer func() { err = errors.Join(err, f.close(anchor)) }()
		out.complete = true
		var walk func(*os.File, string, []filesystemStep) error
		walk = func(parent *os.File, name string, steps []filesystemStep) (walkErr error) {
			if e := charge(int64(len(name))+1, 2); e != nil {
				return e
			}
			before, e := parent.Stat()
			if e != nil {
				return e
			}
			listing, e := f.open(parent, ".", true)
			if e != nil {
				return e
			}
			defer func() { walkErr = errors.Join(walkErr, f.close(listing)) }()
			for {
				if e := charge(1, 1); e != nil {
					return e
				}
				entries, readErr := listing.Readdirnames(1)
				for _, entry := range entries {
					f.entries++
					if e := normalizationBound("directory-entries", f.entries, f.limits.DirectoryEntries); e != nil {
						f.setFailure(e)
						return e
					}
					if e := charge(4*int64(len(name)+len(entry))+1, 1); e != nil {
						return e
					}
					child := entry
					if name != "" {
						child = name + "/" + entry
					}
					if e := f.callHook("inventory-entry", child); e != nil {
						return e
					}
					if entry == ".git" {
						continue
					}
					key, e := NewKey(child)
					if e != nil {
						return e
					}
					if e = f.keyBound(key); e != nil {
						return e
					}
					if !NativeNamePolicy(key) {
						if e = problem("Native name is blocked: " + child); e != nil {
							return e
						}
						continue
					}
					handle, meta, e := inspectFilesystemMetadata(parent, entry, f.metadata)
					if handle != nil {
						e = errors.Join(e, f.close(handle))
					}
					if e != nil {
						if filesystemPolicyError(e) {
							if e = problem("Native entry is blocked: " + child); e != nil {
								return e
							}
							continue
						}
						return e
					}
					if strings.EqualFold(entry, ".git") {
						blocked, e := f.gitAlias(parent, meta.identity)
						if e != nil {
							return e
						}
						if blocked {
							continue
						}
					}
					if !meta.info.IsDir() && !meta.info.Mode().IsRegular() {
						if e = problem("Native entry is not a regular file or directory: " + child); e != nil {
							return e
						}
						continue
					}
					kind := File
					if meta.info.IsDir() {
						kind = Directory
					}
					if e = add(key, kind); e != nil {
						return e
					}
					if e := charge(int64(len(steps))+1, int64(len(steps))+1); e != nil {
						return e
					}
					nextSteps := append(steps, filesystemStep{parent, entry, meta.identity, meta.info.IsDir()})
					if meta.info.IsDir() {
						if e = charge(int64(len(nextSteps))+1, int64(len(nextSteps))+1); e != nil {
							return e
						}
						next, e := f.open(parent, entry, true)
						if e != nil {
							return e
						}
						id, idErr := nativeIdentity(next)
						if idErr != nil || id != meta.identity {
							return errors.Join(idErr, errFilesystemUnstable, f.close(next))
						}
						e = walk(next, child, nextSteps)
						e = errors.Join(e, f.close(next))
						if e != nil {
							return e
						}
					}
					if e = f.verify(ctx, nextSteps, child); e != nil {
						return e
					}
				}
				if readErr != nil {
					if !errors.Is(readErr, io.EOF) {
						return readErr
					}
					break
				}
				if len(entries) == 0 {
					return io.ErrNoProgress
				}
			}
			after, e := parent.Stat()
			if e != nil {
				return e
			}
			if before.ModTime() != after.ModTime() {
				return errFilesystemUnstable
			}
			return charge(0, 0)
		}
		// Descend from the pinned anchor using exact spelling and no-follow handles.
		current := anchor
		var handles []*os.File
		var steps []filesystemStep
		defer func() {
			for i := len(handles) - 1; i >= 0; i-- {
				err = errors.Join(err, f.close(handles[i]))
			}
		}()
		if !directory.rootDirectory {
			for part := range strings.SplitSeq(directory.key.value, "/") {
				if err = charge(int64(len(part))+1, 3); err != nil {
					return out, err
				}
				found, e := f.exact(ctx, current, part, directory.key.value)
				if e != nil {
					return out, e
				}
				if !found {
					return out, problem("Documentation directory is missing.")
				}
				if part == ".git" || !nativeRepresentable(part) {
					return out, problem("Documentation directory is blocked.")
				}
				handle, meta, e := inspectFilesystemMetadata(current, part, f.metadata)
				if handle != nil {
					e = errors.Join(e, f.close(handle))
				}
				if e != nil {
					return out, e
				}
				if strings.EqualFold(part, ".git") {
					blocked, e := f.gitAlias(current, meta.identity)
					if e != nil {
						return out, e
					}
					if blocked {
						return out, problem("Documentation directory is metadata.")
					}
				}
				if !meta.info.IsDir() {
					return out, problem("Documentation root is not a directory.")
				}
				next, e := f.open(current, part, true)
				if e != nil {
					return out, e
				}
				handles = append(handles, next)
				id, e := nativeIdentity(next)
				if e != nil {
					return out, e
				}
				if id != meta.identity {
					return out, errFilesystemUnstable
				}
				steps = append(steps, filesystemStep{current, part, id, true})
				current = next
			}
		}
		err = walk(current, directory.key.value, steps)
		if err == nil {
			err = f.verify(ctx, steps, directory.key.value)
		}
		if err != nil {
			out.complete = false
			return out, err
		}
	}
	if err = charge(int64(maxPath+1)*int64(len(out.facts))*int64(bits.Len(uint(len(out.facts)))+1), 0); err != nil {
		return out, err
	}
	sort.Slice(out.facts, func(i, j int) bool { return out.facts[i].key.value < out.facts[j].key.value })
	return out, nil
}
