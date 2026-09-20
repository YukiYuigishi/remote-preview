package preview

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesKnownTextFileInBrowser(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/main.go"] = "file"
	backend.reads["/root/main.go"] = []byte("package main\nfunc main() {}\n")
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/main.go", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type=%q", got)
	}
	if !strings.Contains(response.Body.String(), "source-code") || !strings.Contains(response.Body.String(), "package main") {
		t.Fatalf("text viewer missing from response: %q", response.Body.String())
	}
	if strings.Contains(response.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("text file should not be an attachment")
	}
}

func TestHandlerSniffsUnknownUTF8TextFile(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/notes.data"] = "file"
	backend.reads["/root/notes.data"] = []byte("plain text without a known extension\n")
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/notes.data", nil))

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("response=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
}

func TestHandlerKeepsBinaryFileAsRaw(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/archive.bin"] = "file"
	backend.reads["/root/archive.bin"] = []byte{0x00, 0x01, 0x02, 0xff}
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/archive.bin", nil))

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("response=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	if string(response.Body.Bytes()) != string(backend.reads["/root/archive.bin"]) {
		t.Fatalf("binary response was rendered as text: %v", response.Body.Bytes())
	}
}

func TestHandlerEscapesTextSourceForHTMLAndJavaScript(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/script.js"] = "file"
	backend.reads["/root/script.js"] = []byte(`const value = "</script><script>alert('xss')</script>";`)
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/script.js", nil))
	body := response.Body.String()
	if strings.Contains(body, `</script><script>alert('xss')`) {
		t.Fatalf("source escaped into executable script: %q", body)
	}
}

func TestTextDetectionAndSyntaxLanguage(t *testing.T) {
	if !isTextFile("unknown.data", []byte("valid UTF-8 text")) {
		t.Fatal("expected unknown UTF-8 file to be text")
	}
	if isTextFile("unknown.data", []byte{0x00, 0x01, 0x02}) {
		t.Fatal("expected NUL-containing file to be binary")
	}
	if got := syntaxLanguage("main.go"); got != "go" {
		t.Fatalf("syntax language=%q, want go", got)
	}
	if got := syntaxLanguage("notes.txt"); got != "" {
		t.Fatalf("plain text syntax language=%q, want empty", got)
	}
}
