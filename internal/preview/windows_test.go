//go:build windows

package preview

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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
