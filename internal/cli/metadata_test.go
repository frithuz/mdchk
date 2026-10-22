package cli

import (
	"bytes"
	"testing"
)

func TestMetadataDeliveryAndEmptyLauncherVersion(t *testing.T) {
	for _, command := range [][]string{nil, {"lint"}, {"fix"}, {"inspect"}, {"config"}, {"config", "validate"}} {
		for _, flag := range []string{"--help", "--version"} {
			args := append(append([]string{}, command...), flag)
			for _, writer := range []interface{ Write([]byte) (int, error) }{failingWriter{}, validationShortWriter{}} {
				if got := Run(args, writer, writer, "v1"); got != 13 {
					t.Fatalf("%v delivery=%d", args, got)
				}
			}
		}
		var out, stderr bytes.Buffer
		args := append(append([]string{}, command...), "--version")
		if got := Run(args, &out, &stderr, ""); got != 0 || out.String() != "mdchk dev\n" || stderr.Len() != 0 {
			t.Fatalf("blank launcher version %v: %d %q %q", args, got, &out, &stderr)
		}
	}
}
