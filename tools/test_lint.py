#!/usr/bin/env python3
"""Exercise the real lint configuration in a disposable module, never the checkout."""

from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def write(root, name, content):
    path = root / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def run(binary, root, *args):
    result = subprocess.run(
        [binary, *args], cwd=root, text=True, stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT, check=False,
    )
    return result.returncode, result.stdout


def main():
    binary = str(Path(sys.argv[1]).resolve())
    checkout = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="mdchk-lint-test-") as directory:
        root = Path(directory)
        shutil.copyfile(checkout / ".golangci.yml", root / ".golangci.yml")
        go_directive = next(line for line in (checkout / "go.mod").read_text().splitlines()
                            if line.startswith("go "))
        write(root, "go.mod", f"module github.com/frithuz/mdchk\n\n{go_directive}\n")
        with (root / "go.mod").open("a", encoding="utf-8") as module:
            for path, version, directory in (
                ("github.com/goccy/go-yaml", "v1.19.2", "yaml"),
                ("github.com/modelcontextprotocol/go-sdk", "v1.8.0", "sdk"),
                ("golang.org/x/sys", "v0.49.0", "sys"),
                ("example.invalid/unapproved", "v1.0.0", "unapproved"),
            ):
                module.write(f"\nrequire {path} {version}\nreplace {path} => ./_dependencies/{directory}\n")
                write(root, f"_dependencies/{directory}/go.mod", f"module {path}\n\n{go_directive}\n")
                source = "// Package dependency supplies a local import probe.\npackage dependency\nfunc Helper() {}\n"
                write(root, f"_dependencies/{directory}/helper.go", source)
                if directory == "sdk":
                    write(root, "_dependencies/sdk/mcp/helper.go", source)
                if directory == "sys":
                    write(root, "_dependencies/sys/unix/helper.go", source)
        # All probes are valid Go; each checks a separately enabled analyzer.
        write(root, "internal/probe/probe.go", '''// Package probe exercises static analysis.
package probe
import (
    "crypto/md5"
    "errors"
    "fmt"
    "log"
    "os"
    "strings"
)
type State int
const (First State = iota; Second)
func Missing(s State) { switch s { case First: fmt.Print(s) } }
func Unchecked() { os.Chdir(".") }
func Formatting() { fmt.Printf("%d", "wrong") }
func Ineffective() int { x := 1; x = 2; return x }
func Replace(s string) string { return strings.Replace(s, "x", "y", 0) }
func unusedFunction() {}
func Compare(err error) bool { return err == errors.ErrUnsupported }
func Exit() { os.Exit(1) }
func Fatal() { log.New(os.Stderr, "", 0).Fatal("stop") }
func Digest(data []byte) [16]byte { return md5.Sum(data) } // #nosec G401
//nolint:errcheck
func Unexplained() { os.Chdir(".") }
''')
        write(root, "internal/conformance/helper.go", "// Package conformance supplies a test renderer.\npackage conformance\nfunc Helper() {}\n")
        write(root, "internal/helper/helper.go", "// Package helper supplies a dependency probe.\npackage helper\nfunc Helper() {}\n")
        write(root, "internal/parser/probe.go", '''// Package parser owns core syntax.
package parser
import (
 "github.com/frithuz/mdchk/internal/conformance"
 "github.com/frithuz/mdchk/internal/helper"
)
func Boundary() { conformance.Helper(); helper.Helper() }
''')
        write(root, "internal/probe/dependencies.go", '''package probe
import (
 yaml "github.com/goccy/go-yaml"
 sdk "github.com/modelcontextprotocol/go-sdk/mcp"
 unapproved "example.invalid/unapproved"
 "github.com/frithuz/mdchk/internal/conformance"
 unix "golang.org/x/sys/unix"
)
func Dependencies() { yaml.Helper(); sdk.Helper(); unapproved.Helper(); conformance.Helper(); unix.Helper() }
''')
        code, output = run(binary, root, "run", "--config", ".golangci.yml", "./...")
        if code != 1:
            raise AssertionError(f"expected finding exit 1, got {code}:\n{output}")
        for analyzer in ("govet", "staticcheck", "unused", "ineffassign", "errcheck",
                         "errorlint", "exhaustive", "nolintlint", "depguard",
                         "forbidigo", "gosec"):
            if f"({analyzer})" not in output:
                raise AssertionError(f"missing {analyzer} detection:\n{output}")
        for boundary in ("parser-standard-library", "test-only-renderer",
                         "configuration-adapter", "mcp-adapter", "approved-dependencies",
                         "native-resource-adapter"):
            if boundary not in output:
                raise AssertionError(f"missing {boundary} detection:\n{output}")
        if "G401" not in output or "Fatal" not in output:
            raise AssertionError(f"missing security/fatal logging detection:\n{output}")
        print("All 11 analyzers detect representative violations; #nosec cannot bypass gosec.")

        # External parser tests may import the test renderer. Process entry
        # points alone may terminate; an aliased os import must still be handled.
        shutil.rmtree(root / "internal/probe")
        write(root, "internal/parser/probe.go", "// Package parser supplies a dependency probe.\npackage parser\nfunc Parse() {}\n")
        write(root, "internal/config/probe.go", '''// Package config owns YAML decoding.
package config
import yaml "github.com/goccy/go-yaml"
func Decode() { yaml.Helper() }
''')
        write(root, "internal/integration/quarto_options.go", '''// Package integration owns bounded Quarto option adaptation.
package integration
import yaml "github.com/goccy/go-yaml"
func DecodeOptions() { yaml.Helper() }
''')
        write(root, "internal/mcp/probe.go", '''// Package mcp owns SDK adaptation.
package mcp
import sdk "github.com/modelcontextprotocol/go-sdk/mcp"
func Adapt() { sdk.Helper() }
''')
        write(root, "internal/resource/native_probe.go", '''// Package resource owns native read adaptation.
package resource
import unix "golang.org/x/sys/unix"
func Native() { unix.Helper() }
''')
        write(root, "internal/parser/probe_test.go", '''package parser_test
import (
 "testing"
 "github.com/frithuz/mdchk/internal/conformance"
)
func TestAdapter(t *testing.T) { conformance.Helper() }
''')
        for name in ("cmd/mdchk/main.go", "internal/conformance/cmd/specadapter/main.go"):
            write(root, name, 'package main\nimport process "os"\nfunc main() { process.Exit(0) }\n')
        code, output = run(binary, root, "run", "--config", ".golangci.yml", "./...")
        if code:
            raise AssertionError(f"legitimate tests/entry points rejected:\n{output}")
        print("External parser tests and both process entry points pass.")

        # The integration exception names exactly one bounded adapter. Neither
        # another integration file nor the core parser inherits YAML access.
        write(root, "internal/integration/other.go", '''package integration
import yaml "github.com/goccy/go-yaml"
func OtherOptions() { yaml.Helper() }
''')
        write(root, "internal/parser/probe.go", '''// Package parser owns core syntax.
package parser
import yaml "github.com/goccy/go-yaml"
func Parse() { yaml.Helper() }
''')
        code, output = run(binary, root, "run", "--config", ".golangci.yml", "./...")
        if code != 1:
            raise AssertionError(f"expected bounded YAML adapter violations, got {code}:\n{output}")
        # Depguard reports the first applicable denied rule for an import.
        # The earlier probe independently exercises parser-standard-library;
        # these two YAML imports must both remain configuration-adapter errors.
        for required in ("internal/integration/other.go", "internal/parser/probe.go",
                         "configuration-adapter"):
            if required not in output:
                raise AssertionError(f"missing narrow YAML boundary {required}:\n{output}")
        if "internal/integration/quarto_options.go:" in output:
            raise AssertionError(f"approved bounded adapter rejected:\n{output}")
        print("Only the bounded Quarto options adapter gains YAML access; other integration/parser files fail.")

        with (root / ".golangci.yml").open("a", encoding="utf-8") as config:
            config.write("\nunknown-configuration-key: true\n")
        code, output = run(binary, root, "config", "verify", "--config", ".golangci.yml")
        if code == 0:
            raise AssertionError(f"invalid configuration accepted:\n{output}")
        print("Configuration errors fail the gate.")


if __name__ == "__main__":
    main()
