package preview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseBatchListingsPreservesSpecialNames(t *testing.T) {
	output := []byte("K\x00dir\x00D\x00\x00E\x00dir\x00tab\tname\nfile\x00E\x00file\x00plain\x00X\x00D\x00child\x00E\x00file\x00nested\x00X\x00")
	result, err := parseBatchListings("/root", output)
	if err != nil {
		t.Fatal(err)
	}
	if result.RootKind != "dir" || len(result.Listings) != 2 {
		t.Fatalf("unexpected batch result: %#v", result)
	}
	if result.Listings[0].Path != "/root" || result.Listings[0].Entries[0].Name != "tab\tname\nfile" {
		t.Fatalf("unexpected root listing: %#v", result.Listings[0])
	}
	if result.Listings[1].Path != "/root/child" || result.Listings[1].Entries[0].Name != "nested" {
		t.Fatalf("unexpected child listing: %#v", result.Listings[1])
	}
}

func TestListBatchWorksWithPOSIXShell(t *testing.T) {
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

	shells := []struct {
		name string
		args []string
	}{
		{name: "/bin/sh"},
	}
	if busybox, err := exec.LookPath("busybox"); err == nil {
		shells = append(shells, struct {
			name string
			args []string
		}{name: busybox, args: []string{"sh"}})
	}

	for _, shell := range shells {
		shell := shell
		t.Run(shell.name, func(t *testing.T) {
			remote := newSSHRemoteFS("test")
			remote.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
				args := append([]string(nil), shell.args...)
				args = append(args, "-c", batchListingScript, "sh", root, "16")
				return exec.CommandContext(ctx, shell.name, args...)
			}
			result, err := remote.ListBatch(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if result.RootKind != "dir" || len(result.Listings) != 2 {
				t.Fatalf("unexpected batch result: %#v", result)
			}
			if result.Listings[0].Path != root || result.Listings[1].Path != filepath.Join(root, "child") {
				t.Fatalf("unexpected paths: %#v", result.Listings)
			}
			if len(result.Listings[1].Entries) != 1 || result.Listings[1].Entries[0].Name != "nested" {
				t.Fatalf("unexpected child entries: %#v", result.Listings[1].Entries)
			}
		})
	}
}
