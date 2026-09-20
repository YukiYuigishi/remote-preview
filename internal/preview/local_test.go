package preview

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRemoteFSKindListAndRead(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("hello local preview\n")
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	local := newLocalRemoteFS()
	if kind, err := local.Kind(context.Background(), root); err != nil || kind != "dir" {
		t.Fatalf("root kind=%q, err=%v", kind, err)
	}
	if kind, err := local.Kind(context.Background(), filepath.Join(root, "notes.txt")); err != nil || kind != "file" {
		t.Fatalf("file kind=%q, err=%v", kind, err)
	}
	if kind, err := local.Kind(context.Background(), filepath.Join(root, "missing")); err != nil || kind != "missing" {
		t.Fatalf("missing kind=%q, err=%v", kind, err)
	}

	entries, err := local.List(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0] != (remoteEntry{Name: "docs", Kind: "dir"}) || entries[1] != (remoteEntry{Name: "notes.txt", Kind: "file"}) {
		t.Fatalf("entries=%#v", entries)
	}

	got, err := local.Read(context.Background(), filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("content=%q, want %q", got, content)
	}
}

func TestLocalRemoteFSHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	local := newLocalRemoteFS()

	if _, err := local.Kind(ctx, "."); err != context.Canceled {
		t.Fatalf("Kind error=%v, want context canceled", err)
	}
	if _, err := local.List(ctx, "."); err != context.Canceled {
		t.Fatalf("List error=%v, want context canceled", err)
	}
	if _, err := local.Read(ctx, "missing"); err != context.Canceled {
		t.Fatalf("Read error=%v, want context canceled", err)
	}
}
