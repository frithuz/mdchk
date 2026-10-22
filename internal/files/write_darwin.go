//go:build darwin && arm64 && cgo

package files

/*
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/mount.h>
#include <sys/xattr.h>
#include <sys/acl.h>
#include <fcntl.h>
#include <unistd.h>
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
static int md_openat(int parent, const char *name, int flags, int mode) { return openat(parent,name,flags|O_NOFOLLOW|O_CLOEXEC,mode); }
static int md_renameat(int parent,const char *old,const char *name){return renameat(parent,old,parent,name);}
static int md_unlinkat(int parent,const char *name){return unlinkat(parent,name,0);}
// Refuse ACLs and unsupported attributes, retaining only bounded provenance.
// Resource forks and security labels are never silently dropped.
static int md_metadata(int fd) {
 struct statfs fs; if(fstatfs(fd,&fs)) return errno;
 if(strcmp(fs.f_fstypename,"apfs") || !(fs.f_flags&MNT_LOCAL)) return ENOTSUP;
 struct stat st; if(fstat(fd,&st)) return errno;
 if(st.st_flags || !(st.st_mode&S_IWUSR) || (st.st_mode&(S_ISUID|S_ISGID|S_ISVTX))) return ENOTSUP;
 ssize_t n=flistxattr(fd,NULL,0,0); if(n<0)return errno; if(n>128)return ENOTSUP;
 char attrs[128]; if(n && flistxattr(fd,attrs,sizeof(attrs),0)!=n)return EIO;
 for(ssize_t i=0;i<n;){if(strcmp(attrs+i,"com.apple.provenance"))return ENOTSUP;i+=strlen(attrs+i)+1;}
 acl_t acl=acl_get_fd_np(fd,ACL_TYPE_EXTENDED); if(!acl)return errno==ENOENT?0:errno;
 acl_entry_t entry; int found=acl_get_entry(acl,ACL_FIRST_ENTRY,&entry); int saved=errno; acl_free(acl);
 if(found==0)return ENOTSUP; if(saved!=EINVAL && found<0)return saved;
 return 0;
}
static ssize_t md_provenance(int fd,void *out,size_t n){return fgetxattr(fd,"com.apple.provenance",out,n,0,0);}
static int md_setprovenance(int fd,void *value,size_t n){return fsetxattr(fd,"com.apple.provenance",value,n,0,0);}
static ssize_t md_aclbytes(int fd,void *out,size_t size){
 acl_t acl=acl_get_fd_np(fd,ACL_TYPE_EXTENDED);if(!acl){if(errno==ENOENT)return 0;return -1;}
 ssize_t n=acl_size(acl);if(n<0||(size_t)n>size){acl_free(acl);errno=ERANGE;return -1;}
 ssize_t result=acl_copy_ext(out,acl,size);int saved=errno;acl_free(acl);errno=saved;return result;
}
*/
import "C"

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/frithuz/mdchk/internal/parser"
)

type nativeObservation struct {
	path      string
	source    parser.Source
	original  parser.Source
	target    bool
	absent    bool
	uncertain bool
	parent    *os.File
	chain     []directoryIdentity
	info      fs.FileInfo
	security  string
	budget    *parser.OperationBudget
}
type nativeWriteSet struct {
	entries  map[string]*nativeObservation
	required map[string][]string
	// Test-only hooks surround real operations; production sets none.
	hook   func(string, string) error
	write  func(*os.File, []byte) (int, error)
	rename func(*os.File, string, string) error
}

func nativeOpen(parent *os.File, name string, flags int, mode uint32) (*os.File, error) {
	c := C.CString(name)
	defer C.free(unsafe.Pointer(c))
	fd, e := C.md_openat(C.int(parent.Fd()), c, C.int(flags), C.int(mode))
	if fd < 0 {
		return nil, e
	}
	return os.NewFile(uintptr(fd), name), nil
}
func nativeRename(parent *os.File, old, name string) error {
	a := C.CString(old)
	b := C.CString(name)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	n, e := C.md_renameat(C.int(parent.Fd()), a, b)
	if n < 0 {
		return e
	}
	return nil
}
func nativeRemove(parent *os.File, name string) error {
	c := C.CString(name)
	defer C.free(unsafe.Pointer(c))
	n, e := C.md_unlinkat(C.int(parent.Fd()), c)
	if n < 0 {
		return e
	}
	return nil
}
func nativeMetadata(f *os.File) error {
	if code := C.md_metadata(C.int(f.Fd())); code != 0 {
		return syscall.Errno(code)
	}
	return nil
}
func nativeSecurity(f *os.File) (string, error) {
	var buf [64]byte
	n, err := C.md_provenance(C.int(f.Fd()), unsafe.Pointer(&buf[0]), C.size_t(len(buf)))
	if n < 0 {
		if errors.Is(err, syscall.ENOATTR) {
			return "", nil
		}
		return "", err
	}
	return string(buf[:int(n)]), nil
}
func preserveNativeSecurity(f *os.File, security string) error {
	if security == "" {
		c := C.CString("com.apple.provenance")
		defer C.free(unsafe.Pointer(c))
		_, _ = C.fremovexattr(C.int(f.Fd()), c, 0)
		return nil
	}
	raw := []byte(security)
	n, err := C.md_setprovenance(C.int(f.Fd()), unsafe.Pointer(&raw[0]), C.size_t(len(raw)))
	if n < 0 {
		return err
	}
	return nil
}
func nativeParent(path string) (parent *os.File, chain []directoryIdentity, err error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." || strings.IndexByte(path, 0) >= 0 {
		return nil, nil, writeError(path, "noncanonical native path")
	}
	current, e := os.Open("/")
	if e != nil {
		return nil, nil, e
	}
	defer func() {
		if err != nil {
			_ = current.Close()
		}
	}()
	info, e := current.Stat()
	if e != nil {
		return nil, nil, e
	}
	acl, e := directoryACL(current)
	if e != nil {
		return nil, nil, e
	}
	chain = append(chain, directoryIdentity{info, acl})
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(path), "/"), "/") {
		if part == "" {
			continue
		}
		next, e := nativeOpen(current, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
		if e != nil {
			return nil, nil, e
		}
		info, e = next.Stat()
		if e != nil {
			_ = next.Close()
			return nil, nil, e
		}
		acl, e := directoryACL(next)
		if e != nil {
			_ = next.Close()
			return nil, nil, e
		}
		chain = append(chain, directoryIdentity{info, acl})
		_ = current.Close()
		current = next
	}
	return current, chain, nil
}
func sameChain(a, b []directoryIdentity) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].acl != b[i].acl || !os.SameFile(a[i].info, b[i].info) || a[i].info.Mode() != b[i].info.Mode() || a[i].info.Sys().(*syscall.Stat_t).Uid != b[i].info.Sys().(*syscall.Stat_t).Uid || a[i].info.Sys().(*syscall.Stat_t).Gid != b[i].info.Sys().(*syscall.Stat_t).Gid || a[i].info.Sys().(*syscall.Stat_t).Flags != b[i].info.Sys().(*syscall.Stat_t).Flags {
			return false
		}
	}
	return true
}
func sameSecurity(a, b fs.FileInfo) bool {
	x, ok := a.Sys().(*syscall.Stat_t)
	y, ok2 := b.Sys().(*syscall.Stat_t)
	return ok && ok2 && a.Mode() == b.Mode() && x.Uid == y.Uid && x.Gid == y.Gid && x.Flags == y.Flags && x.Nlink == y.Nlink
}
func (o *nativeObservation) reopen() (*os.File, error) {
	if o.uncertain {
		return nil, writeError(o.path, "saved state is uncertain; new acquisition required")
	}
	if err := reserveNativeParents(o.budget, o.path); err != nil {
		return nil, err
	}
	parent, chain, err := nativeParent(o.path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	if !sameChain(o.chain, chain) {
		return nil, writeError(o.path, "parent identity changed")
	}
	f, err := nativeOpen(parent, filepath.Base(o.path), syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if o.absent {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if f != nil {
			_ = f.Close()
		}
		if err != nil {
			return nil, err
		}
		return nil, writeError(o.path, "absent dependency appeared")
	}
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(o.info, info) || !sameSecurity(o.info, info) {
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		return nil, writeError(o.path, "identity, type or security metadata changed")
	}
	if err = nativeMetadata(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	security, err := nativeSecurity(f)
	if err != nil || security != o.security {
		_ = f.Close()
		return nil, errors.Join(err, writeError(o.path, "extended security metadata changed"))
	}
	return f, nil
}

type nativeRead struct {
	*os.File
	observation *nativeObservation
}

func (f *nativeRead) Verify() error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !sameSecurity(f.observation.info, info) {
		return errors.New("metadata changed during acquisition")
	}
	if err = nativeMetadata(f.File); err != nil {
		return err
	}
	other, err := f.observation.reopen()
	if other != nil {
		_ = other.Close()
	}
	return err
}
func acquireNativeWrite(ctx context.Context, selected *Selection, options WriteOptions) (out *nativeWriteSet, targets, resources *Acquisition, err error) {
	if err = checkpoint(ctx, options.Budget, int64(len(selected.Targets)+len(options.Absences))*8+1); err != nil {
		return
	}
	count := len(selected.Targets) + len(options.Absences)
	if options.Resources != nil {
		count += len(options.Resources.Targets)
	}
	if err = options.Budget.ChargeNodes(int64(count)*8 + 1); err != nil {
		return
	}
	out = &nativeWriteSet{entries: map[string]*nativeObservation{}, required: map[string][]string{}}
	defer func() {
		if err != nil {
			_ = out.close()
		}
	}()

	writable := map[string]bool{}
	all := &Selection{Targets: slices.Clone(selected.Targets)}
	for _, t := range selected.Targets {
		writable[t.Path] = true
	}
	if options.Resources != nil {
		all.Targets = append(all.Targets, options.Resources.Targets...)
	}
	combined, acquisitionErr := acquireFS(ctx, all, options.AcquireOptions, func(t Target) (readHandle, error) {
		if err := checkpoint(ctx, options.Budget, int64(len(t.Path))*3+int64(strings.Count(t.Path, "/"))*8192+1); err != nil {
			return nil, err
		}
		if t.inspection {
			return nil, writeError(t.Path, "inspection acquisition is read-only")
		}
		if out.entries[t.Path] != nil {
			return nil, writeError(t.Path, "duplicate native observation")
		}
		if err := reserveNativeParents(options.Budget, t.Path); err != nil {
			return nil, err
		}
		parent, chain, e := nativeParent(t.Path)
		if e != nil {
			return nil, e
		}
		if len(t.identities) != len(chain)+1 {
			_ = parent.Close()
			return nil, writeError(t.Path, "missing selection parent authority")
		}
		for i, info := range chain {
			if !os.SameFile(info.info, t.identities[i].info) {
				_ = parent.Close()
				return nil, writeError(t.Path, "selected parent identity changed")
			}
		}
		f, e := nativeOpen(parent, filepath.Base(t.Path), syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if e != nil {
			_ = parent.Close()
			return nil, e
		}
		info, e := f.Stat()
		if e == nil && !info.Mode().IsRegular() {
			e = errors.New("not regular")
		}
		if e == nil {
			e = nativeMetadata(f)
		}
		if e != nil {
			_ = f.Close()
			_ = parent.Close()
			return nil, e
		}
		security, e := nativeSecurity(f)
		if e != nil {
			_ = f.Close()
			_ = parent.Close()
			return nil, e
		}
		observation := &nativeObservation{budget: options.Budget, security: security, path: t.Path, parent: parent, chain: chain, info: info, target: writable[t.Path]}
		out.entries[t.Path] = observation
		return &nativeRead{f, observation}, nil
	})
	if acquisitionErr != nil {
		err = acquisitionErr
		return
	}
	targets = &Acquisition{Selection: selected}
	resources = &Acquisition{Selection: options.Resources}
	destination := func(path string) *Acquisition {
		if writable[path] {
			return targets
		}
		return resources
	}
	for _, d := range combined.Documents {
		a := destination(d.Path)
		a.Documents = append(a.Documents, d)
	}
	for _, e := range combined.Errors {
		a := destination(e.Path)
		a.Errors = append(a.Errors, e)
	}
	for _, e := range combined.PreflightErrors {
		a := destination(e.Path)
		a.PreflightErrors = append(a.PreflightErrors, e)
	}
	for _, e := range combined.Interruptions {
		a := destination(e.Path)
		a.Interruptions = append(a.Interruptions, e)
	}

	for _, a := range []*Acquisition{targets, resources} {
		if a == nil {
			continue
		}
		for _, d := range a.Documents {
			out.entries[d.Path].source = d.Source
			out.entries[d.Path].original = d.Source
		}
	}
	for _, path := range options.Absences {
		if e := ctx.Err(); e != nil {
			err = e
			return
		}
		if e := options.Budget.ChargeWork(int64(len(path)) + 1); e != nil {
			err = e
			return
		}
		if out.entries[path] != nil {
			err = writeError(path, "duplicate dependency")
			return
		}
		if e := reserveNativeParents(options.Budget, path); e != nil {
			err = e
			return
		}
		parent, chain, e := nativeParent(path)
		if e != nil {
			err = e
			return
		}
		o := &nativeObservation{budget: options.Budget, path: path, parent: parent, chain: chain, absent: true}
		out.entries[path] = o
		if _, e = o.reopen(); e != nil {
			err = e
			return
		}
	}
	for path, deps := range options.Required {
		if out.entries[path] == nil || !out.entries[path].target {
			err = writeError(path, "unknown dependency owner")
			return
		}
		out.required[path] = slices.Clone(deps)
		for _, dep := range deps {
			if out.entries[dep] == nil {
				err = writeError(dep, "unobserved dependency")
				return
			}
		}
	}
	return
}
func (s *nativeWriteSet) close() error {
	var errs []error
	for _, o := range s.entries {
		errs = append(errs, o.parent.Close())
	}
	s.entries = nil
	return errors.Join(errs...)
}
func (s *nativeWriteSet) at(stage, path string) error {
	if s.hook != nil {
		return s.hook(stage, path)
	}
	return nil
}
func checkpoint(ctx context.Context, b *parser.OperationBudget, n int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.ChargeWork(n)
}
func (s *nativeWriteSet) validate(ctx context.Context, o *nativeObservation, b *parser.OperationBudget) error {
	if err := checkpoint(ctx, b, int64(o.source.Len())*3+int64(len(o.path))*3+int64(strings.Count(o.path, "/"))*4096+1); err != nil {
		return err
	}
	f, err := o.reopen()
	if err != nil {
		return err
	}
	if f == nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	// Bound a growth probe independently of potentially hostile current file size.
	raw, err := io.ReadAll(io.LimitReader(f, int64(o.source.Len())+1))
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, o.source.Bytes()) {
		return writeError(o.path, "content changed")
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !sameSecurity(o.info, info) || !os.SameFile(o.info, info) || info.Size() != int64(len(raw)) || !info.ModTime().Equal(o.info.ModTime()) {
		return writeError(o.path, "metadata changed during validation")
	}
	return checkpoint(ctx, b, 1)
}
func parentKey(o *nativeObservation) string {
	st := o.chain[len(o.chain)-1].info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%020d:%020d", st.Dev, st.Ino)
}
func (s *nativeWriteSet) locks(ctx context.Context, observations []*nativeObservation, b *parser.OperationBudget) (func(), error) {
	parents := map[string]*os.File{}
	for _, o := range observations {
		parents[parentKey(o)] = o.parent
	}
	keys := make([]string, 0, len(parents))
	for key := range parents {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var held []*os.File
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = syscall.Flock(int(held[i].Fd()), syscall.LOCK_UN)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for _, key := range keys {
		f := parents[key]
		for {
			if err := checkpoint(ctx, b, 1); err != nil {
				release()
				return nil, err
			}
			err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				held = append(held, f)
				break
			}
			if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
				release()
				return nil, err
			}
			if err := s.at("lock-wait", observations[0].path); err != nil {
				release()
				return nil, err
			}
			if time.Now().After(deadline) {
				release()
				return nil, errors.New("cooperating parent lock wait expired")
			}
			timer := time.NewTimer(5 * time.Millisecond)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				release()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return release, nil
}
func (s *nativeWriteSet) publish(ctx context.Context, path string, original, candidate parser.Source, deps []string, b *parser.OperationBudget) (out Publication) {
	out.Path = path
	o := s.entries[path]
	if o == nil || !o.target || o.absent || o.source == (parser.Source{}) || o.source != original {
		out.Err = writeError(path, "source lacks target acquisition authority")
		return
	}
	if candidate == original {
		out.NoChange = true
		return
	}
	if candidate.Len() == original.Len() {
		if err := checkpoint(ctx, b, int64(original.Len())*3+1); err != nil {
			out.Err = err
			return
		}
		if bytes.Equal(candidate.Bytes(), original.Bytes()) {
			out.NoChange = true
			return
		}
	}
	if int64(candidate.Len()) > b.Limits().DocumentBytes {
		out.Err = writeError(path, "candidate exceeds document limit")
		return
	}
	if err := checkpoint(ctx, b, int64(len(deps)+len(s.required[path]))+int64(candidate.Len())*8+int64(strings.Count(path, "/"))*4096+1); err != nil {
		out.Err = err
		return
	}
	if err := reserveNativeParents(b, path); err != nil {
		out.Err = err
		return
	}
	if err := b.ChargeNodes(int64(len(deps)+len(s.required[path]))*3 + 3); err != nil {
		out.Err = err
		return
	}
	observations := []*nativeObservation{o}
	seen := map[string]bool{path: true}
	for _, list := range [][]string{deps, s.required[path]} {
		for _, dep := range list {
			if seen[dep] {
				continue
			}
			seen[dep] = true
			v := s.entries[dep]
			if v == nil || !v.absent && v.source == (parser.Source{}) {
				out.Err = writeError(dep, "dependency has no acquired observation")
				return
			}
			observations = append(observations, v)
		}
	}
	release, err := s.locks(ctx, observations, b)
	if err != nil {
		out.Err = err
		return
	}
	defer release()
	if err = s.at("locked", path); err != nil {
		out.Err = err
		return
	}
	for _, v := range observations {
		if err = s.validate(ctx, v, b); err != nil {
			out.Err = err
			return
		}
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		out.Err = err
		return
	}
	name := ".mdchk-stage-" + hex.EncodeToString(random[:])
	if err = s.at("create", path); err != nil {
		out.Err = err
		return
	}
	temp, err := nativeOpen(o.parent, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		out.Err = err
		return
	}
	createdInfo, createStatErr := temp.Stat()
	if createStatErr != nil {
		_ = temp.Close()
		out.Err = createStatErr
		out.Leftovers = []string{name}
		return
	}
	closed := false
	published := false
	defer func() {
		if !closed {
			out.Err = errors.Join(out.Err, temp.Close())
		}
		if !published {
			if e := s.at("cleanup", path); e != nil {
				out.Leftovers = append(out.Leftovers, name)
				out.Err = errors.Join(out.Err, e)
			} else if e := cleanupNativeStage(o.parent, name, createdInfo); e != nil {
				out.Leftovers = append(out.Leftovers, name)
				out.Err = errors.Join(out.Err, e)
			}
		}
	}()
	if err = nativeMetadata(temp); err != nil {
		out.Err = err
		return
	}
	if err = s.at("write", path); err != nil {
		out.Err = err
		return
	}
	data := candidate.Bytes()
	write := s.write
	if write == nil {
		write = func(f *os.File, data []byte) (int, error) { return f.Write(data) }
	}
	n, err := write(temp, data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = s.at("short-write", path)
	}
	if err != nil {
		out.Err = err
		return
	}
	if err = s.at("metadata", path); err != nil {
		out.Err = err
		return
	}
	st := o.info.Sys().(*syscall.Stat_t)
	if err = temp.Chown(int(st.Uid), int(st.Gid)); err == nil {
		err = temp.Chmod(o.info.Mode().Perm())
	}
	if err == nil {
		err = preserveNativeSecurity(temp, o.security)
	}
	if err == nil {
		err = nativeMetadata(temp)
	}
	if err == nil {
		security, e := nativeSecurity(temp)
		if e != nil || security != o.security {
			err = errors.Join(e, errors.New("staged security metadata differs"))
		}
	}
	if err != nil {
		out.Err = err
		return
	}
	if err = s.at("flush", path); err == nil {
		err = temp.Sync()
	}
	if err != nil {
		out.Err = err
		return
	}
	stagedInfo, statErr := temp.Stat()
	if statErr != nil {
		out.Err = statErr
		return
	}
	err = temp.Close()
	closed = true
	if err == nil {
		err = s.at("close", path)
	}
	if err != nil {
		out.Err = err
		return
	}
	if err = s.at("before-final-check", path); err != nil {
		out.Err = err
		return
	}
	for _, v := range observations {
		if err = s.validate(ctx, v, b); err != nil {
			out.Err = err
			return
		}
	}
	stageCheck, e := nativeOpen(o.parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if e != nil {
		out.Err = e
		return
	}
	stageInfo, e := stageCheck.Stat()
	if e == nil && (!os.SameFile(createdInfo, stageInfo) || !sameSecurity(stagedInfo, stageInfo)) {
		e = errors.New("staged entry identity or metadata changed")
	}
	if e == nil {
		e = verifyNativeSecurity(stageCheck, o.security)
	}
	if e == nil {
		stageBytes, readErr := io.ReadAll(io.LimitReader(stageCheck, int64(len(data))+1))
		if readErr != nil || !bytes.Equal(stageBytes, data) {
			e = errors.Join(readErr, errors.New("staged content changed"))
		}
	}
	_ = stageCheck.Close()
	if e != nil {
		out.Err = e
		return
	}
	if err = checkpoint(ctx, b, 1); err != nil {
		out.Err = err
		return
	}
	// The explicitly accepted noncooperating-writer gap starts here. renameat
	// publishes the final entry through the pinned parent, never its referent.
	if err = s.at("after-final-check", path); err != nil {
		out.Err = err
		return
	}

	if err = s.at("rename", path); err != nil {
		out.Err = err
		return
	}
	if err = checkpoint(ctx, b, 1); err != nil {
		out.Err = err
		return
	}
	rename := s.rename
	if rename == nil {
		rename = nativeRename
	}
	renameErr := rename(o.parent, name, filepath.Base(path))
	if renameErr != nil {
		current, currentErr := nativeEntryInfo(o.parent, filepath.Base(path))
		if currentErr != nil || !os.SameFile(stagedInfo, current) {
			stage, stageErr := nativeEntryInfo(o.parent, name)
			if currentErr != nil || stageErr != nil || !os.SameFile(createdInfo, stage) || !os.SameFile(o.info, current) {
				out.Uncertain = true
				o.uncertain = true
			}
			out.Err = renameErr
			return
		}
		// An actual failed native call may nevertheless have published. Evidence
		// of the staged inode at the target retains that fact without rollback.
	}
	published = true
	o.uncertain = true
	out.Published = true
	out.HardLinks = uint64(st.Nlink)
	out.Err = errors.Join(renameErr, s.at("published", path))
	// Observe publication before later flush/cancellation failures. Never rollback.
	currentParent, currentChain, parentErr := nativeParent(path)
	if currentParent != nil {
		_ = currentParent.Close()
	}
	if parentErr != nil || !sameChain(o.chain, currentChain) {
		out.Uncertain = true
		out.Err = errors.Join(out.Err, parentErr, errors.New("published logical parent changed"))
		return
	}
	f, e := nativeOpen(o.parent, filepath.Base(path), syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if e == nil {
		info, statErr := f.Stat()
		if statErr == nil {
			statErr = verifyNativeSecurity(f, o.security)
		}
		if statErr == nil && info.Mode().IsRegular() && os.SameFile(stagedInfo, info) && sameSecurity(stagedInfo, info) {
			raw, readErr := io.ReadAll(io.LimitReader(f, int64(candidate.Len())+1))
			if readErr == nil && bytes.Equal(raw, data) {
				o.source = candidate
				o.info = info
				out.Saved = candidate
				o.uncertain = false
			} else {
				e = errors.Join(readErr, errors.New("published content changed before observation"))
			}
		} else {
			e = errors.Join(statErr, errors.New("published entry identity or metadata changed"))
		}
		_ = f.Close()
	}
	if e != nil {
		out.Uncertain = true
		out.Err = errors.Join(out.Err, e)
	}
	if err = s.at("directory-flush", path); err == nil {
		err = o.parent.Sync()
	}
	out.Err = errors.Join(out.Err, err)
	if err = checkpoint(ctx, b, 1); err != nil {
		out.Err = errors.Join(out.Err, err)
	}
	return
}

func (s *nativeWriteSet) matches(path string, source parser.Source, absent bool) bool {
	o := s.entries[path]
	return o != nil && o.absent == absent && (absent || o.original == source)
}

// Never remove a detected replacement of our private stage name. The remaining
// no-CAS check/unlink interval is subject to the documented external-writer gap.
func cleanupNativeStage(parent *os.File, name string, owned fs.FileInfo) error {
	f, err := nativeOpen(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	actual, err := f.Stat()
	_ = f.Close()
	if err != nil {
		return err
	}
	if !os.SameFile(owned, actual) {
		return errors.New("staging entry substituted; foreign artifact retained")
	}
	return nativeRemove(parent, name)
}

type directoryIdentity struct {
	info fs.FileInfo
	acl  string
}

func directoryACL(f *os.File) (string, error) {
	var raw [4096]byte
	n, err := C.md_aclbytes(C.int(f.Fd()), unsafe.Pointer(&raw[0]), C.size_t(len(raw)))
	if n < 0 {
		return "", err
	}
	return string(raw[:int(n)]), nil
}

func reserveNativeParents(b *parser.OperationBudget, path string) error {
	n := int64(strings.Count(path, "/")) + 1
	if err := b.ChargeWork(n*4096 + int64(len(path))*2 + 1); err != nil {
		return err
	}
	return b.ChargeNodes(n*4 + 1)
}

func nativeEntryInfo(parent *os.File, name string) (fs.FileInfo, error) {
	f, err := nativeOpen(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	return info, errors.Join(err, f.Close())
}

func verifyNativeSecurity(f *os.File, expected string) error {
	if err := nativeMetadata(f); err != nil {
		return err
	}
	security, err := nativeSecurity(f)
	if err != nil {
		return err
	}
	if security != expected {
		return errors.New("extended security metadata changed")
	}
	return nil
}
