package preview

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRemoteFSSizeAndOpenRange(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "track.mp3")
	content := []byte("0123456789")
	if err := os.WriteFile(mediaPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	local := newLocalRemoteFS()
	size, err := local.Size(context.Background(), mediaPath)
	if err != nil || size != int64(len(content)) {
		t.Fatalf("Size=%d error=%v, want %d", size, err, len(content))
	}
	reader, err := local.OpenRange(context.Background(), mediaPath, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(io.LimitReader(reader, 3))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(body) != "456" {
		t.Fatalf("range body=%q read=%v close=%v", body, readErr, closeErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := local.OpenRange(ctx, mediaPath, 0, 1); err != context.Canceled {
		t.Fatalf("canceled OpenRange error=%v, want context canceled", err)
	}
}

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
