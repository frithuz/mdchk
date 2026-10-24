package resource

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func windowsNativeTemp(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func windowsNativeRoot(t *testing.T, path string) *os.File {
	t.Helper()
	root, err := nativeOpenAnchor(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

func windowsNativeWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeWindowsReadOnlyHandleAndMetadata(t *testing.T) {
	dir := windowsNativeTemp(t)
	windowsNativeWrite(t, filepath.Join(dir, "guide.md"), "immutable\r\n")
	root := windowsNativeRoot(t, dir)
	file, err := nativeOpenChild(root, "guide.md", false)
	if err != nil {
		t.Fatal(err)
	}
	text, readErr := io.ReadAll(file)
	if readErr != nil || string(text) != "immutable\r\n" {
		t.Fatal(string(text), readErr)
	}
	if n, err := file.Write([]byte("overwrite")); n != 0 || err == nil {
		t.Fatal("read-only handle accepted a write", n, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	metadata, err := nativeOpenMetadata(root, "guide.md", false)
	if err != nil {
		t.Fatal(err)
	}
	info, statErr := metadata.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() != int64(len("immutable\r\n")) {
		t.Fatal(info, statErr)
	}
	if n, err := metadata.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatal("metadata-only handle granted content access", n, err)
	}
	if err := metadata.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "guide.md"))
	if err != nil || string(after) != "immutable\r\n" {
		t.Fatal("native reads changed fixture", string(after), err)
	}
}

func TestNativeWindowsIndependentPinnedDirectoryHandles(t *testing.T) {
	dir := windowsNativeTemp(t)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	windowsNativeWrite(t, filepath.Join(dir, "sub", "leaf.md"), "original handle")
	root := windowsNativeRoot(t, dir)
	parent, err := nativeOpenChild(root, "sub", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "sub"), filepath.Join(dir, "moved")); err != nil {
		_ = parent.Close()
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		_ = parent.Close()
		t.Fatal(err)
	}
	windowsNativeWrite(t, filepath.Join(dir, "sub", "leaf.md"), "replacement path")
	file, err := nativeOpenChild(parent, "leaf.md", false)
	if err != nil {
		_ = parent.Close()
		t.Fatal(err)
	}
	text, readErr := io.ReadAll(file)
	closeErr := file.Close()
	parentCloseErr := parent.Close()
	if readErr != nil || closeErr != nil || parentCloseErr != nil || string(text) != "original handle" {
		t.Fatal("relative read reopened a replaced parent path", string(text), readErr, closeErr, parentCloseErr)
	}
	for range 2 {
		clone, err := nativeOpenChild(root, ".", true)
		if err != nil {
			t.Fatal(err)
		}
		names, readErr := clone.Readdirnames(16)
		closeErr := clone.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil || !slices.Contains(names, "sub") || !slices.Contains(names, "moved") {
			t.Fatal("directory clone shared/reused an enumeration cursor", names, readErr, closeErr)
		}
	}
}

func TestNativeWindowsRejectsEveryReparseComponent(t *testing.T) {
	dir := windowsNativeTemp(t)
	outside := windowsNativeTemp(t)
	windowsNativeWrite(t, filepath.Join(dir, "body.md"), "inside")
	windowsNativeWrite(t, filepath.Join(outside, "secret.md"), "must not be read")
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	windowsNativeWrite(t, filepath.Join(dir, "folder", "child.md"), "inside child")
	if err := os.Link(filepath.Join(dir, "body.md"), filepath.Join(dir, "same-inode.md")); err != nil {
		t.Fatal(err)
	}
	root := windowsNativeRoot(t, dir)
	for _, tc := range []struct {
		name, target string
		directory    bool
	}{
		{"contained", "body.md", false},
		{"escaping", filepath.Join(outside, "secret.md"), false},
		{"dangling", "absent.md", false},
		{"same-inode", "same-inode.md", false},
		{"contained-dir", "folder", true},
		{"escaping-dir", outside, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// This is mandatory native proof. A runner unable to create the link
			// fails explicitly; it must not silently count skipped races as safe.
			if err := os.Symlink(tc.target, filepath.Join(dir, tc.name)); err != nil {
				t.Fatalf("native Windows symbolic-link proof unavailable: %v", err)
			}
			for _, open := range []func(*os.File, string, bool) (*os.File, error){nativeOpenChild, nativeOpenMetadata} {
				file, err := open(root, tc.name, tc.directory)
				if file != nil {
					_ = file.Close()
					t.Fatal("a native reparse component was opened")
				}
				if !nativePolicyError(err) {
					t.Fatal("link refusal lost policy identity", err)
				}
			}
		})
	}
}

func TestNativeWindowsRejectsActualDirectoryJunctions(t *testing.T) {
	dir := windowsNativeTemp(t)
	outside := windowsNativeTemp(t)
	inside := filepath.Join(dir, "ordinary")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	windowsNativeWrite(t, filepath.Join(inside, "child.md"), "contained fixture")
	windowsNativeWrite(t, filepath.Join(outside, "child.md"), "outside fixture")
	root := windowsNativeRoot(t, dir)
	for _, open := range []func(*os.File, string, bool) (*os.File, error){nativeOpenChild, nativeOpenMetadata} {
		folder, err := open(root, "ordinary", true)
		if err != nil {
			t.Fatal("ordinary directory negative control failed", err)
		}
		if err := folder.Close(); err != nil {
			t.Fatal(err)
		}
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, target string }{{"contained-junction", inside}, {"escaping-junction", outside}} {
		t.Run(tc.name, func(t *testing.T) {
			junction := filepath.Join(dir, tc.name)
			// cmd.exe is used only to create this mandatory native fixture. Do
			// not let a specially configured temp base introduce shell syntax.
			if strings.ContainsAny(junction+tc.target, "\r\n\"%&|<>^!()") {
				t.Fatal("native junction proof unavailable: temporary paths contain cmd metacharacters")
			}
			command := exec.Command(filepath.Join(system, "cmd.exe"), "/d", "/c", "mklink", "/J", junction, tc.target) //nolint:gosec // Fixed system executable and validated generated fixture paths.
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("native Windows junction proof unavailable: %v\n%s", err, output)
			}
			name, err := windows.UTF16PtrFromString(junction)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES,
				windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
				windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
			if err != nil {
				t.Fatal("cannot inspect actual junction fixture", err)
			}
			var tag struct{ Attributes, ReparseTag uint32 }
			tagErr := windows.GetFileInformationByHandleEx(handle, windows.FileAttributeTagInfo,
				(*byte)(unsafe.Pointer(&tag)), uint32(unsafe.Sizeof(tag)))
			closeErr := windows.CloseHandle(handle)
			if tagErr != nil || closeErr != nil || tag.Attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 || tag.ReparseTag != windows.IO_REPARSE_TAG_MOUNT_POINT {
				t.Fatal("fixture is not an actual Windows mount-point junction", tag, tagErr, closeErr)
			}
			for _, open := range []func(*os.File, string, bool) (*os.File, error){nativeOpenChild, nativeOpenMetadata} {
				file, err := open(root, tc.name, true)
				if file != nil {
					_ = file.Close()
					t.Fatal("native directory junction was opened")
				}
				if !nativePolicyError(err) {
					t.Fatal("junction refusal lost policy identity", err)
				}
			}
			file, err := nativeOpenAnchor(junction)
			if file != nil {
				_ = file.Close()
				t.Fatal("junction anchor was opened")
			}
			if !nativePolicyError(err) {
				t.Fatal("junction anchor refusal lost policy identity", err)
			}
		})
	}
}

func TestNativeWindowsNamePolicyAndUnsupportedAnchors(t *testing.T) {
	for _, name := range []string{"CON", "con", "NUL", "COM1", "LPT9", "CONIN$", "CONOUT$", "name:stream", "name.", "name ", "a/b", `a\b`, "a?b", "a*b", "a\x00b", ".", "..", ""} {
		if nativeRepresentable(name) {
			t.Errorf("unsupported native name admitted: %q", name)
		}
	}
	for _, name := range []string{".GIT", "Guide.md", "a%20b.md", "a+b.md", "é.md", "e\u0301.md", "a\u2028b.md", "a\u2029b.md"} {
		if !nativeRepresentable(name) {
			t.Errorf("literal canonical Windows name changed or rejected: %q", name)
		}
	}
	for _, path := range []string{`\\server\share\folder`, `\\?\C:\folder`, `\\.\C:\folder`} {
		if err := nativeValidateAnchor(path); !nativePolicyError(err) {
			t.Fatal("raw unsupported anchor escaped lexical preflight", path, err)
		}
		file, err := nativeOpenAnchor(path)
		if file != nil {
			_ = file.Close()
			t.Fatal("unsupported anchor acquired a handle", path)
		}
		if !nativePolicyError(err) {
			t.Fatal("unsupported anchor did not retain explicit refusal", path, err)
		}
	}
	for _, path := range []string{`C:relative`, `C:`, `folder\name:stream`, `folder\NUL`, `folder.\child`, "folder\x01name"} {
		if err := nativeValidateAnchor(path); !nativePolicyError(err) {
			t.Fatal("implicit/stream anchor escaped lexical preflight", path, err)
		}
	}
	for _, path := range []string{`C:\folder`, `C:/folder`, `folder`, `..\folder`, "."} {
		if err := nativeValidateAnchor(path); err != nil {
			t.Fatal("ordinary local anchor cannot be resolved by caller", path, err)
		}
	}
}

func TestNativeWindowsFullHandleIdentity(t *testing.T) {
	dir := windowsNativeTemp(t)
	windowsNativeWrite(t, filepath.Join(dir, "first.md"), "same text")
	windowsNativeWrite(t, filepath.Join(dir, "second.md"), "same text")
	if err := os.Link(filepath.Join(dir, "first.md"), filepath.Join(dir, "hardlink.md")); err != nil {
		t.Fatal(err)
	}
	root := windowsNativeRoot(t, dir)
	identities := make(map[string]string)
	for _, name := range []string{"first.md", "second.md", "hardlink.md"} {
		file, err := nativeOpenMetadata(root, name, false)
		if err != nil {
			t.Fatal(err)
		}
		identity, identityErr := nativeIdentity(file)
		closeErr := file.Close()
		if identityErr != nil || closeErr != nil || identity == "" {
			t.Fatal(identity, identityErr, closeErr)
		}
		identities[name] = identity
	}
	if identities["first.md"] == identities["second.md"] || identities["first.md"] != identities["hardlink.md"] {
		t.Fatal("handle identity confused equal bytes or lost hard-link identity", identities)
	}
}

func TestNativeWindowsClosedParentAndInvalidComponents(t *testing.T) {
	dir := windowsNativeTemp(t)
	windowsNativeWrite(t, filepath.Join(dir, "data.md"), "data")
	root, err := nativeOpenAnchor(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../data.md", "sub/data.md", `sub\data.md`, "C:data.md", "..", ".", ""} {
		file, err := nativeOpenChild(root, name, false)
		if file != nil {
			_ = file.Close()
			t.Fatal("noncomponent native input acquired a handle", name)
		}
		if !errors.Is(err, ErrContract) {
			t.Fatal(name, err)
		}
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := nativeOpenChild(root, "data.md", false)
	if file != nil {
		_ = file.Close()
		t.Fatal("closed parent granted a new child handle")
	}
	if err == nil {
		t.Fatal("closed parent accepted")
	}
}

func windowsNativeSetCaseMode(t *testing.T, path string, sensitive bool) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// Test fixture setup only: no production read handle receives write rights.
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.FILE_WRITE_ATTRIBUTES|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	var flags uint32
	if sensitive {
		flags = windows.FILE_CS_FLAG_CASE_SENSITIVE_DIR
	}
	setErr := windows.SetFileInformationByHandle(handle, windows.FileCaseSensitiveInfo,
		(*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	closeErr := windows.CloseHandle(handle)
	if setErr != nil || closeErr != nil {
		t.Fatalf("native Windows directory-case proof unavailable: set=%v close=%v", setErr, closeErr)
	}
}

func TestNativeWindowsGitMetadataHonorsDirectoryCase(t *testing.T) {
	for _, sensitive := range []bool{false, true} {
		name := "insensitive"
		if sensitive {
			name = "sensitive"
		}
		t.Run(name, func(t *testing.T) {
			dir := windowsNativeTemp(t)
			windowsNativeSetCaseMode(t, dir, sensitive)
			if err := os.Mkdir(filepath.Join(dir, ".GIT"), 0700); err != nil {
				t.Fatal(err)
			}
			root := windowsNativeRoot(t, dir)
			upper, err := nativeOpenMetadata(root, ".GIT", true)
			if err != nil {
				t.Fatal(err)
			}
			upperID, identityErr := nativeIdentity(upper)
			closeErr := upper.Close()
			if identityErr != nil || closeErr != nil {
				t.Fatal(identityErr, closeErr)
			}
			hostInfo, hostErr := os.Lstat(filepath.Join(dir, ".git"))
			t.Logf("directory sensitive=%v, host os.Lstat(.git) error=%v", sensitive, hostErr)
			marker, markerErr := nativeOpenGitMetadata(root)
			if sensitive {
				if marker != nil {
					_ = marker.Close()
					t.Fatal("distinct case-sensitive .GIT was treated as metadata")
				}
				if !errors.Is(markerErr, os.ErrNotExist) {
					t.Fatal("case-sensitive exact metadata absence lost", markerErr)
				}
				if !errors.Is(hostErr, os.ErrNotExist) {
					t.Fatal("native case-sensitive fixture did not distinguish .git/.GIT", hostInfo, hostErr)
				}
				return
			}
			if hostErr != nil || hostInfo == nil || markerErr != nil || marker == nil {
				t.Fatal("native metadata case alias not honored", hostErr, markerErr)
			}
			markerID, identityErr := nativeIdentity(marker)
			closeErr = marker.Close()
			if identityErr != nil || closeErr != nil || markerID != upperID {
				t.Fatal("metadata alias did not identify the actual entry", markerID, upperID, identityErr, closeErr)
			}
		})
	}
}

func TestNativeWindowsTrustedAnchorCaseAliases(t *testing.T) {
	parent := windowsNativeTemp(t)
	windowsNativeSetCaseMode(t, parent, false)
	path := filepath.Join(parent, "MixedAnchorName")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "mixedanchorname")
	alias = strings.ToLower(alias[:1]) + alias[1:]
	if _, err := os.Stat(alias); err != nil {
		t.Fatal("trusted native anchor alias fixture unavailable", err)
	}
	first := windowsNativeRoot(t, path)
	second := windowsNativeRoot(t, alias)
	firstID, firstErr := nativeIdentity(first)
	secondID, secondErr := nativeIdentity(second)
	if firstErr != nil || secondErr != nil || firstID != secondID {
		t.Fatal("trusted local anchor alias changed pinned identity", firstID, secondID, firstErr, secondErr)
	}
	// This allowance is private to trusted anchor acquisition, not a change to
	// logical key validation or the exact-spelling resource enumeration contract.
}

func TestNativeWindowsNamedGitAliasBeforeLogicalAbsence(t *testing.T) {
	for _, sensitive := range []bool{false, true} {
		name := "insensitive"
		if sensitive {
			name = "sensitive"
		}
		t.Run(name, func(t *testing.T) {
			dir := windowsNativeTemp(t)
			windowsNativeSetCaseMode(t, dir, sensitive)
			if err := os.Mkdir(filepath.Join(dir, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			root := windowsNativeRoot(t, dir)
			fixed, err := nativeOpenGitMetadata(root)
			if err != nil {
				t.Fatal(err)
			}
			fixedID, identityErr := nativeIdentity(fixed)
			closeErr := fixed.Close()
			if identityErr != nil || closeErr != nil {
				t.Fatal(identityErr, closeErr)
			}
			for _, alias := range []string{".GIT", ".GiT"} {
				file, err := nativeOpenGitAliasMetadata(root, alias)
				if sensitive {
					if file != nil {
						_ = file.Close()
						t.Fatal("sensitive namespace inferred an absent spelling from metadata", alias)
					}
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatal(alias, err)
					}
					continue
				}
				if err != nil || file == nil {
					t.Fatal("native Git alias was mistaken for logical absence", alias, err)
				}
				aliasID, identityErr := nativeIdentity(file)
				closeErr := file.Close()
				if identityErr != nil || closeErr != nil || aliasID != fixedID {
					t.Fatal("alias did not prove the fixed metadata identity", alias, aliasID, fixedID, identityErr, closeErr)
				}
			}
			for _, ordinary := range []string{"guide.md", ".github", ".git/child", ".git."} {
				file, err := nativeOpenGitAliasMetadata(root, ordinary)
				if file != nil {
					_ = file.Close()
					t.Fatal("Git-only alias probe accepted arbitrary resource", ordinary)
				}
				if !errors.Is(err, ErrContract) {
					t.Fatal(ordinary, err)
				}
			}
		})
	}
}

func TestNativeWindowsBareDriveVolumePin(t *testing.T) {
	volume := filepath.VolumeName(windowsNativeTemp(t))
	first, err := windowsOpenVolume(volume)
	if err != nil {
		t.Fatal("bare trusted drive volume could not be pinned", err)
	}
	firstInfo, statErr := first.Stat()
	firstID, identityErr := nativeIdentity(first)
	closeErr := first.Close()
	if statErr != nil || identityErr != nil || closeErr != nil || !firstInfo.IsDir() || firstID == "" {
		t.Fatal(firstInfo, firstID, statErr, identityErr, closeErr)
	}
	second, err := windowsOpenVolume(strings.ToLower(volume))
	if err != nil {
		t.Fatal("lowercase drive volume could not be pinned", err)
	}
	secondID, identityErr := nativeIdentity(second)
	closeErr = second.Close()
	if identityErr != nil || closeErr != nil || secondID != firstID {
		t.Fatal("drive spelling changed pinned native volume", secondID, firstID, identityErr, closeErr)
	}
	for _, name := range []string{volume + `\folder`, `\\server\share`, `\\?\C:`, `\\.\PhysicalDrive0`, ""} {
		file, err := windowsOpenVolume(name)
		if file != nil {
			_ = file.Close()
			t.Fatal("bare-volume primitive accepted path components or a device namespace", name)
		}
		if !errors.Is(err, ErrContract) {
			t.Fatal(name, err)
		}
	}
	if file, err := windowsOpenNative(0, `\??\C:\folder`, true, false); file != nil || !errors.Is(err, ErrContract) {
		if file != nil {
			_ = file.Close()
		}
		t.Fatal("component primitive admitted an absolute native path", err)
	}
}
