package remotehelper

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunListBatchPreservesSpecialNames(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tab\tname", "line\nname"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "child", "nested"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := Run([]string{"list-batch", ProtocolVersion, root, "16"}, &output); err != nil {
		t.Fatal(err)
	}
	encoded := output.String()
	for _, want := range []string{"K\x00dir\x00", "D\x00\x00", "E\x00file\x00tab\tname\x00", "E\x00file\x00line\nname\x00", "D\x00child\x00", "E\x00file\x00nested\x00", "X\x00"} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("output does not contain %q: %q", want, encoded)
		}
	}
}

func TestRunListBatchLimitsChildDirectories(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 4; i++ {
		if err := os.Mkdir(filepath.Join(root, string(rune('a'+i))), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	var output bytes.Buffer
	if err := Run([]string{"list-batch", ProtocolVersion, root, "2"}, &output); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(output.String(), "D\x00"); got != 3 {
		t.Fatalf("directory records=%d, want root plus 2 children: %q", got, output.String())
	}
}

func TestRunListBatchNonDirectoryRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := Run([]string{"list-batch", ProtocolVersion, file, "16"}, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "K\x00file\x00"; got != want {
		t.Fatalf("output=%q, want %q", got, want)
	}
}
