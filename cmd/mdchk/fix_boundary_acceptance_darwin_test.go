//go:build darwin && arm64 && cgo

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompiledApplyStaleNativeBoundary(t *testing.T) {
	for _, mode := range []string{"same-size-restored-mtime", "identical-inode-replacement"} {
		t.Run(mode, func(t *testing.T) {
			mutation := `if path=="b.md" { name:=names[path]; info,e:=os.Stat(name); if e!=nil { panic(e) }; if e=os.WriteFile(name,[]byte("new!"),info.Mode().Perm()); e!=nil { panic(e) }; if e=os.Chtimes(name,info.ModTime(),info.ModTime()); e!=nil { panic(e) } }; `
			if mode == "identical-inode-replacement" {
				mutation = `if path=="b.md" { name:=names[path]; info,e:=os.Stat(name); if e!=nil { panic(e) }; temporary:=name+".external"; if e=os.WriteFile(temporary,[]byte("b.md"),info.Mode().Perm()); e!=nil { panic(e) }; if e=os.Chtimes(temporary,info.ModTime(),info.ModTime()); e!=nil { panic(e) }; if e=os.Rename(temporary,name); e!=nil { panic(e) } }; `
			}
			binary := buildApplyBoundaryFixture(t, mutation+`return validated.PublishMapped(ctx,native,path,names)`)
			for _, format := range []string{"json", "text"} {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
				before := map[string]os.FileInfo{}
				for _, p := range []string{"a.md", "b.md", "c.md"} {
					putConfigurationFile(t, root, p, p)
					info, err := os.Stat(filepath.Join(root, p))
					if err != nil {
						t.Fatal(err)
					}
					before[p] = info
				}
				inventory := configurationBytes(t, root)
				got := runAppliedFix(t, binary, root, "fix", "a.md", "b.md", "c.md", "--format="+format)
				if got.Status != 13 {
					t.Fatal(got)
				}
				for _, p := range []string{"a.md", "b.md", "c.md"} {
					want := p + "\n"
					status := "written"
					if p == "b.md" {
						want = p
						status = "refused"
						if mode == "same-size-restored-mtime" {
							want = "new!"
						}
					}
					assertSavedApplyFile(t, root, p, want, status, got, format)
					after, err := os.Stat(filepath.Join(root, p))
					if err != nil {
						t.Fatal(err)
					}
					if before[p].Mode() != after.Mode() {
						t.Fatal("changed mode", p)
					}
					if p == "b.md" && (!before[p].ModTime().Equal(after.ModTime()) || os.SameFile(before[p], after) != (mode == "same-size-restored-mtime")) {
						t.Fatal("stale mutation did not establish intended identity", mode)
					}
					inventory[filepath.Join(root, p)] = sha256.Sum256([]byte(want))
				}
				if !reflect.DeepEqual(inventory, configurationBytes(t, root)) {
					t.Fatal("unexpected boundary artifacts")
				}
			}
		})
	}
}

func assertSavedApplyFile(t *testing.T, root, path, want, status string, got configurationRun, format string) {
	t.Helper()
	//nolint:gosec // Read only the synthetic test fixture.
	data, e := os.ReadFile(filepath.Join(root, path))
	if e != nil || string(data) != want {
		t.Fatal(path, string(data), want, e, got)
	}
	if format != "json" {
		return
	}
	found := false
	for _, f := range got.Result.Files {
		if f.Path != path {
			continue
		}
		found = true
		if f.WriteStatus != status || f.ResultSnapshot == nil || f.ResultSnapshot.View != "saved" || f.ResultSnapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
			t.Fatal(f)
		}
		for _, d := range got.Result.Diagnostics {
			if d.Path == path && d.Snapshot != *f.ResultSnapshot {
				t.Fatal("candidate diagnostic passed as saved", d)
			}
		}
	}
	if !found {
		t.Fatal("missing target outcome", path, got)
	}
}

func TestCompiledApplyOriginCancellation(t *testing.T) {
	for _, mode := range []string{"before-first", "after-first", "after-first-with-io"} {
		t.Run(mode, func(t *testing.T) {
			body := `cancel:=ctx.Value(struct{issue149 bool}{true}).(context.CancelFunc); cancel(); return validated.PublishMapped(context.Background(),native,path,names)`
			if mode != "before-first" {
				body = `pub:=validated.PublishMapped(ctx,native,path,names); if path=="a.md" { ctx.Value(struct{issue149 bool}{true}).(context.CancelFunc)() }; return pub`
				if mode == "after-first-with-io" {
					body = strings.Replace(body, "return pub", `pub.Err=errors.Join(pub.Err,errors.New("post-publication I/O failure")); return pub`, 1)
				}
			}
			binary := buildApplyBoundaryOriginFixture(t, body, true)
			for _, format := range []string{"json", "text"} {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
				before := map[string]os.FileInfo{}
				for _, p := range []string{"a.md", "b.md", "c.md"} {
					putConfigurationFile(t, root, p, p)
					info, e := os.Stat(filepath.Join(root, p))
					if e != nil {
						t.Fatal(e)
					}
					before[p] = info
				}
				inventory := configurationBytes(t, root)
				got := runAppliedFix(t, binary, root, "fix", "a.md", "b.md", "c.md", "--format="+format)
				wantStatus := 15
				if mode == "after-first-with-io" {
					wantStatus = 13
				}
				if got.Status != wantStatus {
					t.Fatal(got)
				}
				for _, p := range []string{"a.md", "b.md", "c.md"} {
					want := p
					written := p == "a.md" && mode != "before-first"
					if written {
						want += "\n"
					}
					//nolint:gosec // Read only this test's synthetic target.
					data, e := os.ReadFile(filepath.Join(root, p))
					if e != nil || string(data) != want {
						t.Fatal(p, string(data), e)
					}
					after, e := os.Stat(filepath.Join(root, p))
					if e != nil {
						t.Fatal(e)
					}
					if os.SameFile(before[p], after) == written || before[p].Mode() != after.Mode() || !written && !before[p].ModTime().Equal(after.ModTime()) {
						t.Fatal("cancellation altered wrong target", p)
					}
					inventory[filepath.Join(root, p)] = sha256.Sum256(data)
					if format == "json" {
						found := false
						for _, f := range got.Result.Files {
							if f.Path != p {
								continue
							}
							found = true
							if written {
								if f.WriteStatus != "written" || f.ResultSnapshot == nil || f.ResultSnapshot.View != "saved" || f.ResultSnapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
									t.Fatal(f)
								}
							} else {
								if f.WriteStatus == "written" || f.ResultSnapshot != nil {
									t.Fatal("unconfirmed cancellation snapshot", f)
								}
								covered := false
								for _, c := range got.Result.Coverage {
									if c.Path == p && c.Reason == "saved-view-unavailable" {
										covered = true
									}
								}
								if !covered {
									t.Fatal("missing unavailable saved coverage", p, got)
								}
							}
						}
						if !found {
							t.Fatal("missing cancellation outcome", p)
						}
						if len(got.Result.Interruptions) == 0 || got.Result.Complete {
							t.Fatal(got)
						}
					}
				}
				if !reflect.DeepEqual(inventory, configurationBytes(t, root)) {
					t.Fatal("cancellation left artifacts")
				}
			}
		})
	}
}

func TestCompiledApplyChangedNativePermissions(t *testing.T) {
	binary := buildApplyBoundaryFixture(t, `if path=="b.md" { if e:=os.Chmod(names[path],0444);e!=nil {panic(e)} }; return validated.PublishMapped(ctx,native,path,names)`)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	for _, p := range []string{"a.md", "b.md", "c.md"} {
		putConfigurationFile(t, root, p, p)
	}
	target := filepath.Join(root, "b.md")
	before, e := os.Stat(target)
	if e != nil {
		t.Fatal(e)
	}
	inventory := configurationBytes(t, root)
	got := runAppliedFix(t, binary, root, "fix", "a.md", "b.md", "c.md", "--format=json")
	if got.Status != 13 {
		t.Fatal(got)
	}
	for _, p := range []string{"a.md", "b.md", "c.md"} {
		want := p + "\n"
		status := "written"
		if p == "b.md" {
			want = p
			status = "refused"
		}
		assertSavedApplyFile(t, root, p, want, status, got, "json")
		inventory[filepath.Join(root, p)] = sha256.Sum256([]byte(want))
	}
	after, e := os.Stat(target)
	if e != nil || !os.SameFile(before, after) || after.Mode().Perm() != 0444 || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("native refusal replaced target or forced permissions", e)
	}
	if !reflect.DeepEqual(inventory, configurationBytes(t, root)) {
		t.Fatal("permission refusal left artifacts")
	}
}

func TestCompiledApplySymlinkSubstitutionRefused(t *testing.T) {
	// The replacement is made after acquisition, so selection cannot merely skip
	// the link. The real native publisher must reject the changed authority.
	binary := buildApplyBoundaryFixture(t, `if path=="b.md" { if e:=os.Remove(names[path]);e!=nil {panic(e)}; if e:=os.Symlink(names[path]+".external",names[path]);e!=nil {panic(e)} }; return validated.PublishMapped(ctx,native,path,names)`)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	for _, p := range []string{"a.md", "b.md", "c.md"} {
		putConfigurationFile(t, root, p, p)
	}
	putConfigurationFile(t, root, "b.md.external", "external")
	external := filepath.Join(root, "b.md.external")
	before, e := os.Stat(external)
	if e != nil {
		t.Fatal(e)
	}
	inventory := configurationBytes(t, root)
	got := runAppliedFix(t, binary, root, "fix", "a.md", "b.md", "c.md", "--format=json")
	if got.Status != 13 {
		t.Fatal(got)
	}
	for _, p := range []string{"a.md", "c.md"} {
		assertSavedApplyFile(t, root, p, p+"\n", "written", got, "json")
		inventory[filepath.Join(root, p)] = sha256.Sum256([]byte(p + "\n"))
	}
	delete(inventory, filepath.Join(root, "b.md"))
	if !reflect.DeepEqual(inventory, configurationBytes(t, root)) {
		t.Fatal("native substitution followed external link or left artifacts")
	}
	after, e := os.Stat(external)
	if e != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("native substitution changed external metadata", e)
	}
	link, e := os.Readlink(filepath.Join(root, "b.md"))
	if e != nil || link != external {
		t.Fatal("native refusal replaced substituted entry", link, e)
	}
	for _, f := range got.Result.Files {
		if f.Path == "b.md" && (f.WriteStatus == "written" || f.ResultSnapshot != nil) {
			t.Fatal("substituted target reported as saved candidate", f)
		}
	}
	covered := false
	for _, c := range got.Result.Coverage {
		if c.Path == "b.md" && c.Reason == "saved-view-unavailable" {
			covered = true
		}
	}
	if !covered {
		t.Fatal("missing saved-view uncertainty", got)
	}
}
