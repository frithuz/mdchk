package resource

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// nativeValidateAnchor is deliberately lexical. It runs before canonicalization
// can probe a remote/device namespace or expand a drive-relative current folder.
func nativeValidateAnchor(path string) error {
	if !utf8.ValidString(path) || strings.IndexByte(path, 0) >= 0 {
		return ErrContract
	}
	volume := filepath.VolumeName(path)
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") ||
		volume != "" && (len(volume) != 2 || !asciiAlpha(volume[0]) || volume[1] != ':') {
		return fmt.Errorf("%w: UNC, device and extended Windows anchors are unsupported", errNativePolicy)
	}
	rest := path[len(volume):]
	if volume != "" && (rest == "" || rest[0] != '\\' && rest[0] != '/') {
		return fmt.Errorf("%w: drive-relative Windows anchors are unsupported", errNativePolicy)
	}
	if strings.ContainsAny(rest, `<>:"|?*`) {
		return fmt.Errorf("%w: unsupported Windows anchor spelling", errNativePolicy)
	}
	for _, component := range strings.FieldsFunc(rest, func(r rune) bool { return r == '\\' || r == '/' }) {
		// Trusted relative anchor dot steps are resolved by the caller. This
		// allowance is never used for an untrusted resource key component.
		if component != "." && component != ".." && !nativeRepresentable(component) {
			return fmt.Errorf("%w: unsupported Windows anchor component", errNativePolicy)
		}
	}
	return nil
}

// nativeOpenAnchor admits only an operator-selected canonical local drive path.
// The bare trusted drive volume is pinned separately from subsequent components,
// which are always opened relative to an owned, non-reparse directory handle.
func nativeOpenAnchor(path string) (*os.File, error) {
	if err := nativeValidateAnchor(path); err != nil {
		return nil, err
	}
	volume := filepath.VolumeName(path)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
		return nil, ErrContract
	}
	if len(volume) != 2 || !asciiAlpha(volume[0]) || volume[1] != ':' {
		return nil, fmt.Errorf("%w: UNC, device and extended Windows anchors are unsupported", errNativePolicy)
	}
	volumeName, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return nil, err
	}
	// Refuse a mapped network drive before opening its namespace. The caller
	// must still validate the pinned anchor identity across acquisition; this
	// probe is not an atomic snapshot of Windows device-map configuration.
	const driveRemote = 4 // DRIVE_REMOTE from GetDriveTypeW.
	if windows.GetDriveType(volumeName) == driveRemote {
		return nil, fmt.Errorf("%w: mapped network Windows anchors are unsupported", errNativePolicy)
	}
	root, err := windowsOpenVolume(volume)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path[len(volume):], `\`), `\`)
	if len(parts) == 1 && parts[0] == "" {
		clone, cloneErr := windowsOpenChild(root, ".", true, true)
		return windowsReplaceParent(root, clone, cloneErr)
	}
	for i, part := range parts {
		if !nativeRepresentable(part) {
			return windowsReplaceParent(root, nil, fmt.Errorf("%w: unsupported Windows anchor component", errNativePolicy))
		}
		next, openErr := windowsOpenChildCase(root, part, true, i == len(parts)-1, true)
		root, err = windowsReplaceParent(root, next, openErr)
		if err != nil {
			return nil, err
		}
	}
	return root, nil
}

// windowsOpenVolume opens only a validated bare local drive root, analogous to
// opening "/" on Unix. A DOS drive mapping is an object-manager link rather
// than an untrusted filesystem path component. No caller path segments reach
// this Win32 open; each remaining component uses strict relative NtCreateFile.
func windowsOpenVolume(volume string) (*os.File, error) {
	if len(volume) != 2 || !asciiAlpha(volume[0]) || volume[1] != ':' {
		return nil, ErrContract
	}
	name, err := windows.UTF16PtrFromString(`\\?\` + volume + `\`)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.FILE_TRAVERSE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file, err := windowsAdmitHandle(handle, volume+`\`, true)
	if err != nil {
		return nil, err
	}
	if _, err := nativeIdentity(file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// nativeIdentity uses the full handle-derived Windows identifier. In particular,
// ReFS identifiers are 128 bits; os.SameFile's legacy 64-bit identifier cannot
// substitute for this check. There is no pathname-based identity fallback.
func nativeIdentity(file *os.File) (string, error) {
	if file == nil {
		return "", ErrContract
	}
	connection, err := file.SyscallConn()
	if err != nil {
		return "", err
	}
	var info struct {
		Volume uint64
		ID     [16]byte
	}
	var queryErr error
	err = connection.Control(func(fd uintptr) {
		queryErr = windows.GetFileInformationByHandleEx(windows.Handle(fd), windows.FileIdInfo,
			(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	})
	if err != nil {
		return "", err
	}
	if queryErr != nil {
		if errors.Is(queryErr, windows.ERROR_INVALID_PARAMETER) || errors.Is(queryErr, windows.ERROR_NOT_SUPPORTED) || errors.Is(queryErr, windows.ERROR_INVALID_FUNCTION) {
			return "", fmt.Errorf("%w: full Windows file identity unavailable: %v", errNativePolicy, queryErr)
		}
		return "", queryErr
	}
	if info.ID == [16]byte{} {
		return "", fmt.Errorf("%w: full Windows file identity unavailable", errNativePolicy)
	}
	return fmt.Sprintf("%016x:%x", info.Volume, info.ID), nil
}

func windowsReplaceParent(parent, next *os.File, openErr error) (*os.File, error) {
	closeErr := parent.Close()
	if openErr == nil && closeErr == nil {
		return next, nil
	}
	if next != nil {
		closeErr = errors.Join(closeErr, next.Close())
	}
	return nil, errors.Join(openErr, closeErr)
}

func nativeOpenChild(parent *os.File, name string, directory bool) (*os.File, error) {
	return windowsOpenChild(parent, name, directory, true)
}

// nativeOpenMetadata retains a no-follow type/identity handle without requesting
// content access. Directory enumeration still uses nativeOpenChild(..., true).
func nativeOpenMetadata(parent *os.File, name string, directory bool) (*os.File, error) {
	return windowsOpenChild(parent, name, directory, false)
}

// nativeOpenGitMetadata is the sole native-case alias probe. Its name is fixed
// by policy, it requests metadata only and it never follows a reparse point.
// Case-sensitive directories retain .GIT as distinct from an absent .git entry.
func nativeOpenGitMetadata(parent *os.File) (*os.File, error) {
	return nativeOpenGitAliasMetadata(parent, ".git")
}

// nativeOpenGitAliasMetadata is restricted to a case spelling of the Git marker.
// It lets the reader identify prohibited native metadata aliases before claiming
// exact logical absence; it is not an arbitrary case-insensitive resource open.
func nativeOpenGitAliasMetadata(parent *os.File, name string) (*os.File, error) {
	if !strings.EqualFold(name, ".git") {
		return nil, ErrContract
	}
	return windowsOpenChildCase(parent, name, false, false, true)
}

func windowsDirectoryCaseFlags(handle windows.Handle) (uint32, error) {
	var flags uint32
	err := windows.GetFileInformationByHandleEx(handle, windows.FileCaseSensitiveInfo,
		(*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return 0, fmt.Errorf("%w: native directory case policy unavailable: %v", errNativePolicy, err)
	}
	if err != nil {
		return 0, err
	}
	if flags&^uint32(windows.FILE_CS_FLAG_CASE_SENSITIVE_DIR) != 0 {
		return 0, fmt.Errorf("%w: unknown native directory case policy", errNativePolicy)
	}
	return flags, nil
}

func windowsOpenChild(parent *os.File, name string, directory, data bool) (*os.File, error) {
	return windowsOpenChildCase(parent, name, directory, data, false)
}

// Native-case matching is reserved for the trusted anchor and fixed Git marker.
// Untrusted resource component opens never opt into case-insensitive lookup.
func windowsOpenChildCase(parent *os.File, name string, directory, data, hostCase bool) (*os.File, error) {
	if parent == nil || !(directory && name == ".") && !nativeRepresentable(name) {
		return nil, ErrContract
	}
	connection, err := parent.SyscallConn()
	if err != nil {
		return nil, err
	}
	var file *os.File
	var openErr error
	// Control pins the parent descriptor through the relative native open. Fd by
	// itself could be closed and recycled concurrently before NtCreateFile.
	err = connection.Control(func(fd uintptr) {
		handle := windows.Handle(fd)
		if !hostCase {
			file, openErr = windowsOpenNative(handle, name, directory, data)
			return
		}
		before, caseErr := windowsDirectoryCaseFlags(handle)
		if caseErr != nil {
			openErr = caseErr
			return
		}
		file, openErr = windowsOpenNativeCase(handle, name, directory, data, before&windows.FILE_CS_FLAG_CASE_SENSITIVE_DIR == 0)
		if openErr != nil && !errors.Is(openErr, os.ErrNotExist) && !nativePolicyError(openErr) {
			return // An encountered I/O failure remains an I/O failure.
		}
		after, caseErr := windowsDirectoryCaseFlags(handle)
		if caseErr == nil && after != before {
			caseErr = errFilesystemUnstable
		}
		if caseErr != nil {
			if file != nil {
				caseErr = errors.Join(caseErr, file.Close())
				file = nil
			}
			// Provisional absence must not hide failed/unstable case-policy
			// validation. No content was read through this handle.
			openErr = caseErr
		}
	})
	if err != nil {
		if file != nil {
			err = errors.Join(err, file.Close())
		}
		return nil, err
	}
	return file, openErr
}

func windowsOpenNative(parent windows.Handle, name string, directory, data bool) (*os.File, error) {
	return windowsOpenNativeCase(parent, name, directory, data, false)
}

func windowsOpenNativeCase(parent windows.Handle, name string, directory, data, insensitive bool) (*os.File, error) {
	if parent == 0 {
		return nil, ErrContract // Never use this component primitive for absolute paths.
	}
	objectName := name
	if parent != 0 && directory && name == "." {
		// An empty relative NT object name reopens the pinned directory itself,
		// with an independent file-position context. It is never a user key.
		objectName = ""
	}
	unicodeName, err := windows.NewNTUnicodeString(objectName)
	if err != nil {
		return nil, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		RootDirectory: parent,
		ObjectName:    unicodeName,
		Attributes:    windows.OBJ_DONT_REPARSE,
	}
	if insensitive {
		// Trusted anchor components and fixed Git marker probes may match
		// native case aliases when the parent directory permits them.
		// Ordinary resource components never request case folding.
		attributes.Attributes |= windows.OBJ_CASE_INSENSITIVE
	}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	access := uint32(windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE)
	if data {
		access |= windows.FILE_READ_DATA // Also FILE_LIST_DIRECTORY for directories.
	}
	options := uint32(windows.FILE_OPEN_REPARSE_POINT | windows.FILE_SYNCHRONOUS_IO_NONALERT)
	if directory {
		access |= windows.FILE_TRAVERSE
		options |= windows.FILE_DIRECTORY_FILE
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, access, &attributes, &status, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN, options, 0, 0)
	if err != nil {
		return nil, windowsNativeError(err)
	}
	return windowsAdmitHandle(handle, name, directory)
}

func windowsAdmitHandle(handle windows.Handle, name string, directory bool) (*os.File, error) {
	// Inspect the actual object for both the trusted volume pin and relative
	// component opens. A reparse/device handle is never exposed for data access;
	// no unsupported guard causes a weaker component-resolution fallback.
	fileType, err := windows.GetFileType(handle)
	if err != nil {
		return nil, errors.Join(err, windows.CloseHandle(handle))
	}
	if fileType != windows.FILE_TYPE_DISK {
		return nil, windowsClosePolicy(handle)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, errors.Join(err, windows.CloseHandle(handle))
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DEVICE) != 0 || directory && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return nil, windowsClosePolicy(handle)
	}
	return os.NewFile(uintptr(handle), name), nil
}

func windowsClosePolicy(handle windows.Handle) error {
	if err := windows.CloseHandle(handle); err != nil {
		// A genuine close error must remain I/O rather than be hidden by the
		// policy refusal which caused cleanup.
		return fmt.Errorf("closing refused native Windows object: %w", err)
	}
	return fmt.Errorf("%w: Windows reparse or special object", errNativePolicy)
}

func windowsNativeError(err error) error {
	var status windows.NTStatus
	if !errors.As(err, &status) {
		return err
	}
	switch status {
	case windows.STATUS_REPARSE_POINT_ENCOUNTERED, windows.STATUS_STOPPED_ON_SYMLINK,
		windows.STATUS_IO_REPARSE_TAG_NOT_HANDLED, windows.STATUS_REPARSE_POINT_NOT_RESOLVED,
		windows.STATUS_INVALID_PARAMETER, windows.STATUS_NOT_SUPPORTED:
		return fmt.Errorf("%w: native no-follow open: %v", errNativePolicy, status)
	default:
		// Keep ordinary absence/permission/sharing identities usable through
		// errors.Is, without exposing a weaker path-based retry or fallback.
		return fmt.Errorf("%v: %w", status, status.Errno())
	}
}

func nativePolicyError(err error) bool { return errors.Is(err, errNativePolicy) }

// This is an active Windows adapter policy, not a transformation of the shared
// logical namespace. filepath.IsLocal's device-name check is lexical and does
// not access files; it accounts for the running Windows device-name semantics.
func nativeRepresentable(name string) bool {
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) ||
		strings.ContainsAny(name, `<>:"/\|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || !filepath.IsLocal(name) {
		return false
	}
	for _, scalar := range name {
		if unicode.IsControl(scalar) {
			return false
		}
	}
	return true
}
