//go:build windows

package preview

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsLocalTargets(t *testing.T) {
	for _, input := range []string{`C:\work\notes.txt`, `C:/work/notes.txt`, `\\server\share\notes.txt`, `.\work`, `..\work`, `~\work`, `\work`} {
		target, err := parseTarget(input)
		if err != nil || !target.Local || target.Root != input {
			t.Fatalf("parseTarget(%q)=%#v, %v", input, target, err)
		}
	}

	root := t.TempDir()
	target, err := parseTarget(root)
	if err != nil || !target.Local {
		t.Fatalf("temporary directory target=%#v, %v", target, err)
	}
	resolved, err := resolveLocalTarget(target)
	if err != nil || resolved.Root != root {
		t.Fatalf("resolved target=%#v, %v", resolved, err)
	}
}

func TestWindowsLocalPreviewAndTransfer(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "nested", "note.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	local := newLocalRemoteFS()
	h := &handler{target: remoteTarget{Host: "local", Root: root, Local: true}, remote: newCachedRemoteFS(local, "local"), transfer: local, writeEnabled: true}
	if got := h.pathBase(file); got != "note.txt" {
		t.Fatalf("local file name=%q", got)
	}
	for _, endpoint := range []string{"/nested/note.txt?raw=1", "/_ykview/transfer/download?path=nested/note.txt"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if response.Code != http.StatusOK || response.Body.String() != "hello" {
			t.Fatalf("GET %s: status=%d body=%q", endpoint, response.Code, response.Body.String())
		}
	}
	for _, endpoint := range []string{`/..%5csecret.txt`, `/_ykview/transfer/download?path=C%3A%2Fsecret.txt`} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if response.Code != http.StatusNotFound && response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe GET %s: status=%d", endpoint, response.Code)
		}
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("paths", "nested/new.txt"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("files", "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("uploaded")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/_ykview/transfer/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", response.Code, response.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(root, "nested", "new.txt"))
	if err != nil || string(content) != "uploaded" {
		t.Fatalf("uploaded content=%q, %v", content, err)
	}
}

func TestWindowsSSHUsesIndependentConnections(t *testing.T) {
	remote := newSSHRemoteFS("remote-host")
	remote.enableConnectionSharing()
	if remote.controlDir != "" || remote.controlPath != "" {
		t.Fatalf("unexpected ControlMaster state: %#v", remote)
	}
	command, _, cancel := remote.newSSHCommand(context.Background(), false, "sh", "-c", "true")
	defer cancel()
	args := strings.Join(command.Args, " ")
	if !strings.Contains(args, "ssh ") || strings.Contains(args, "ControlPath=") || !strings.Contains(args, "remote-host") {
		t.Fatalf("ssh command=%q", args)
	}
}

func TestWindowsServerPowerShellOperations(t *testing.T) {
	for _, shell := range []string{"cmd.exe", "powershell.exe"} {
		t.Run(shell, func(t *testing.T) {
			remote := newSSHRemoteFS("test-host")
			remote.command = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				commandLine := args[len(args)-1]
				if !strings.HasPrefix(commandLine, powerShellCommandPrefix) {
					t.Errorf("unexpected remote command %q", commandLine)
				}
				if shell == "cmd.exe" {
					return exec.CommandContext(ctx, "cmd.exe", "/c", commandLine)
				}
				return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", commandLine)
			}
			if !remote.probeWindows(context.Background()) {
				t.Fatal("Windows server probe failed")
			}
			remote.windows = true
			home, err := remote.Home(context.Background())
			if err != nil || !isWindowsDriveAbsolute(home) {
				t.Fatalf("Windows home=%q, %v", home, err)
			}
			root := filepath.ToSlash(t.TempDir())
			name := "日本語's.bin"
			file := joinRemotePath(root, name)
			content := []byte{0, 1, 2, 3, 255, 10}
			if err := os.WriteFile(filepath.FromSlash(file), content, 0o600); err != nil {
				t.Fatal(err)
			}
			if kind, err := remote.Kind(context.Background(), file); err != nil || kind != "file" {
				t.Fatalf("file kind=%q, %v", kind, err)
			}
			if size, err := remote.Size(context.Background(), file); err != nil || size != int64(len(content)) {
				t.Fatalf("file size=%d, %v", size, err)
			}
			if got, err := remote.Read(context.Background(), file); err != nil || !bytes.Equal(got, content) {
				t.Fatalf("file contents=%v, %v", got, err)
			}
			stream, err := remote.OpenRange(context.Background(), file, 2, 3)
			if err != nil {
				t.Fatal(err)
			}
			ranged, readErr := io.ReadAll(stream)
			closeErr := stream.Close()
			if readErr != nil || closeErr != nil || !bytes.Equal(ranged, content[2:5]) {
				t.Fatalf("range=%v, read=%v, close=%v", ranged, readErr, closeErr)
			}
			listing, err := remote.ListBatch(context.Background(), root)
			if err != nil || listing.RootKind != "dir" || len(listing.Listings) == 0 {
				t.Fatalf("listing=%#v, %v", listing, err)
			}
			if err := remote.MkdirAll(context.Background(), joinRemotePath(root, "nested")); err != nil {
				t.Fatal(err)
			}
			for _, unsafePath := range []string{"relative-path", root + "/../outside"} {
				if err := remote.MkdirAll(context.Background(), unsafePath); err == nil {
					t.Fatalf("unsafe upload path %q was accepted", unsafePath)
				}
			}
			destination := joinRemotePath(root, "nested/upload.bin")
			if err := remote.WriteFile(context.Background(), destination, bytes.NewReader(content)); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.FromSlash(destination))
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("uploaded contents=%v, %v", got, err)
			}
			if err := remote.WriteFile(context.Background(), destination, bytes.NewReader([]byte("replacement"))); err != nil {
				t.Fatal(err)
			}
			got, err = os.ReadFile(filepath.FromSlash(destination))
			if err != nil || string(got) != "replacement" {
				t.Fatalf("replacement=%q, %v", got, err)
			}
			for _, pattern := range []string{".ykview-upload-*", ".ykview-backup-*"} {
				leftovers, err := filepath.Glob(filepath.Join(filepath.FromSlash(root), "nested", pattern))
				if err != nil || len(leftovers) != 0 {
					t.Fatalf("upload leftovers for %q: %v, %v", pattern, leftovers, err)
				}
			}
		})
	}
}
