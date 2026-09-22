package preview

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type fakeTransferFS struct {
	files map[string][]byte
}

func (f *fakeTransferFS) Open(_ context.Context, remotePath string) (io.ReadCloser, transferInfo, error) {
	content, ok := f.files[remotePath]
	if !ok {
		return nil, transferInfo{}, errors.New("file not found")
	}
	return io.NopCloser(bytes.NewReader(content)), transferInfo{Kind: "file", Size: int64(len(content))}, nil
}

func (f *fakeTransferFS) MkdirAll(context.Context, string) error { return nil }

func (f *fakeTransferFS) WriteFile(_ context.Context, remotePath string, src io.Reader) error {
	content, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	f.files[remotePath] = content
	return nil
}

func TestValidateTransferRelativePath(t *testing.T) {
	for _, input := range []string{"/absolute", "../parent", "docs/../secret", `docs\\file`, "docs//file", "docs/./file"} {
		if _, err := validateTransferRelativePath(input, false); err == nil {
			t.Fatalf("validateTransferRelativePath(%q) unexpectedly succeeded", input)
		}
	}
	for input, want := range map[string]string{"": "", "notes.txt": "notes.txt", "docs/readme.md": "docs/readme.md"} {
		got, err := validateTransferRelativePath(input, true)
		if err != nil || got != want {
			t.Fatalf("validateTransferRelativePath(%q)=%q, %v; want %q", input, got, err, want)
		}
	}
}

func TestSSHRemoteFSOpenStreamsCommandOutput(t *testing.T) {
	remote := newSSHRemoteFS("remote-host")
	remote.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf streamed")
	}
	stream, info, err := remote.Open(context.Background(), "/root/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(stream)
	closeErr := stream.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("stream read=%v close=%v", readErr, closeErr)
	}
	if string(body) != "streamed" || info.Size != -1 {
		t.Fatalf("body=%q info=%#v", body, info)
	}
}

func TestSSHRemoteFSWriteStreamsStdin(t *testing.T) {
	remote := newSSHRemoteFS("remote-host")
	remote.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null")
	}
	if err := remote.WriteFile(context.Background(), "/root/upload.txt", strings.NewReader("payload")); err != nil {
		t.Fatal(err)
	}
}

func TestHandlerDownloadsIndividualFile(t *testing.T) {
	remote := newFakeRemoteFS()
	remote.kinds["/root/notes.txt"] = "file"
	transfer := &fakeTransferFS{files: map[string][]byte{"/root/notes.txt": []byte("hello\n")}}
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: remote, transfer: transfer}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/_ykview/transfer/download?path=notes.txt", nil))

	if response.Code != http.StatusOK || response.Body.String() != "hello\n" {
		t.Fatalf("download response=%d %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, "notes.txt") {
		t.Fatalf("content disposition=%q", got)
	}
	if got := response.Header().Get("Content-Length"); got != "6" {
		t.Fatalf("content length=%q, want 6", got)
	}
}

func TestHandlerDownloadsDirectoryAsZip(t *testing.T) {
	remote := newFakeRemoteFS()
	remote.kinds["/root/docs"] = "dir"
	remote.kinds["/root/docs/readme.md"] = "file"
	remote.kinds["/root/docs/nested"] = "dir"
	remote.kinds["/root/docs/nested/data.txt"] = "file"
	remote.lists["/root/docs"] = []remoteEntry{
		{Name: "readme.md", Kind: "file"},
		{Name: "nested", Kind: "dir"},
	}
	remote.lists["/root/docs/nested"] = []remoteEntry{{Name: "data.txt", Kind: "file"}}
	transfer := &fakeTransferFS{files: map[string][]byte{
		"/root/docs/readme.md":       []byte("readme"),
		"/root/docs/nested/data.txt": []byte("data"),
	}}
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: remote, transfer: transfer}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/_ykview/transfer/download?path=docs", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("zip response=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	contents := make(map[string]string)
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			contents[file.Name] = ""
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		contents[file.Name] = string(body)
	}
	for name, want := range map[string]string{
		"docs/":                "",
		"docs/readme.md":       "readme",
		"docs/nested/":         "",
		"docs/nested/data.txt": "data",
	} {
		if got, ok := contents[name]; !ok || got != want {
			t.Fatalf("zip entry %q=%q, present=%v; want %q", name, got, ok, want)
		}
	}
}

func TestDirectoryTemplateIncludesTransferControlsWhenWriteEnabled(t *testing.T) {
	remote := newFakeRemoteFS()
	remote.kinds["/root"] = "dir"
	remote.lists["/root"] = []remoteEntry{{Name: "notes.txt", Kind: "file"}}
	h := &handler{
		target:       remoteTarget{Host: "remote-host", Root: "/root"},
		remote:       remote,
		writeEnabled: true,
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	body := response.Body.String()
	for _, want := range []string{"Download selected (.zip)", "file-picker", "directory-picker", "drop-zone", `data-path="notes.txt"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("directory response missing %q: %s", want, body)
		}
	}
}

func TestHandlerRejectsTransferPathTraversal(t *testing.T) {
	remote := newFakeRemoteFS()
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: remote, transfer: &fakeTransferFS{files: map[string][]byte{}}}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/_ykview/transfer/download?path=../secret", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", response.Code)
	}
}

func TestHandlerRejectsUploadWhenWriteDisabled(t *testing.T) {
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: newFakeRemoteFS(), transfer: &fakeTransferFS{files: map[string][]byte{}}}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/_ykview/transfer/upload", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", response.Code)
	}
}

func TestHandlerUploadsMultipartFileToLocalTarget(t *testing.T) {
	root := t.TempDir()
	remote := newLocalRemoteFS()
	h := &handler{
		target:       remoteTarget{Host: "local", Root: root, Local: true},
		remote:       remote,
		transfer:     remote,
		writeEnabled: true,
	}

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	if err := multipartWriter.WriteField("paths", "nested/hello.txt"); err != nil {
		t.Fatal(err)
	}
	part, err := multipartWriter.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("uploaded")); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/_ykview/transfer/upload", &body)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", response.Code, response.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(root, "nested", "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "uploaded" {
		t.Fatalf("uploaded content=%q", content)
	}
}

func TestHandlerResumableUploadCommitsAtomicallyAfterFinalChunk(t *testing.T) {
	root := t.TempDir()
	remote := newLocalRemoteFS()
	store := newUploadSessionStore()
	h := &handler{
		target:       remoteTarget{Host: "local", Root: root, Local: true},
		remote:       remote,
		transfer:     remote,
		writeEnabled: true,
		uploads:      store,
	}
	requestChunk := func(id string, offset int64, content string) (uploadChunkResponse, int) {
		query := "directory=&path=disk.img&total=11&offset=" + strconv.FormatInt(offset, 10)
		if id != "" {
			query += "&upload_id=" + id
		}
		request := httptest.NewRequest(http.MethodPatch, "/_ykview/transfer/upload-chunk?"+query, strings.NewReader(content))
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		var result uploadChunkResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode chunk response: %v; body=%s", err, response.Body.String())
		}
		return result, response.Code
	}

	first, status := requestChunk("", 0, "hello")
	if status != http.StatusOK || first.Complete || first.Offset != 5 || first.UploadID == "" {
		t.Fatalf("first chunk status=%d response=%#v", status, first)
	}
	if _, err := os.Stat(filepath.Join(root, "disk.img")); !os.IsNotExist(err) {
		t.Fatalf("destination appeared before final chunk, stat error=%v", err)
	}

	statusResponse := httptest.NewRecorder()
	h.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/_ykview/transfer/upload-chunk?upload_id="+first.UploadID, nil))
	var current uploadChunkResponse
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Offset != 5 || current.Complete {
		t.Fatalf("session status=%#v", current)
	}
	wrongOffset, status := requestChunk(first.UploadID, 0, "hello")
	if status != http.StatusConflict || wrongOffset.Offset != 5 {
		t.Fatalf("wrong offset status=%d response=%#v", status, wrongOffset)
	}

	second, status := requestChunk(first.UploadID, first.Offset, " world")
	if status != http.StatusOK || !second.Complete || second.Offset != 11 {
		t.Fatalf("final chunk status=%d response=%#v", status, second)
	}
	content, err := os.ReadFile(filepath.Join(root, "disk.img"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello world" {
		t.Fatalf("committed content=%q", content)
	}
	if len(store.sessions) != 0 {
		t.Fatalf("completed upload session was not removed: %d", len(store.sessions))
	}
}

func TestHandlerResumableUploadHonorsConfiguredLimit(t *testing.T) {
	root := t.TempDir()
	remote := newLocalRemoteFS()
	h := &handler{
		target:        remoteTarget{Host: "local", Root: root, Local: true},
		remote:        remote,
		transfer:      remote,
		writeEnabled:  true,
		maxUploadSize: 10,
		uploads:       newUploadSessionStore(),
	}
	request := httptest.NewRequest(http.MethodPatch, "/_ykview/transfer/upload-chunk?directory=&path=disk.img&total=11&offset=0", strings.NewReader("x"))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(h.uploads.sessions) != 0 {
		t.Fatalf("oversized upload created a session: %d", len(h.uploads.sessions))
	}
}
