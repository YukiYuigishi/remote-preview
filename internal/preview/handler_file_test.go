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

func TestHandlerServesHTMLFileDirectly(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/index.html"] = "file"
	backend.reads["/root/index.html"] = []byte("<!doctype html><title>remote page</title><script>window.loaded = true</script>")
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/index.html", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type=%q, want text/html; charset=utf-8", got)
	}
	if got := response.Body.String(); got != string(backend.reads["/root/index.html"]) {
		t.Fatalf("HTML response=%q, want direct document", got)
	}
	if strings.Contains(response.Body.String(), "source-code") {
		t.Fatal("HTML file was rendered by the text viewer")
	}

	rawResponse := httptest.NewRecorder()
	h.ServeHTTP(rawResponse, httptest.NewRequest(http.MethodGet, "/index.html?raw=1", nil))
	if rawResponse.Code != http.StatusOK || rawResponse.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("raw HTML response=%d content-type=%q", rawResponse.Code, rawResponse.Header().Get("Content-Type"))
	}
	if got := rawResponse.Body.String(); got != string(backend.reads["/root/index.html"]) {
		t.Fatalf("raw HTML response=%q, want direct document", got)
	}
}

func TestHandlerServesSVGFileDirectly(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><text>diagram</text></svg>`)
	backend := newFakeRemoteFS()
	backend.kinds["/root/diagram.svg"] = "file"
	backend.kinds["/root/diagram.SVG"] = "file"
	backend.reads["/root/diagram.svg"] = svg
	backend.reads["/root/diagram.SVG"] = svg
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	for _, test := range []struct {
		name string
		url  string
	}{
		{name: "normal lowercase extension", url: "/diagram.svg"},
		{name: "normal uppercase extension", url: "/diagram.SVG"},
		{name: "raw uppercase extension", url: "/diagram.SVG?raw=1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.url, nil))

			if response.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", response.Code)
			}
			if got := response.Header().Get("Content-Type"); got != "image/svg+xml" {
				t.Fatalf("content type=%q, want image/svg+xml", got)
			}
			if got := response.Body.Bytes(); string(got) != string(svg) {
				t.Fatalf("SVG response=%q, want exact source bytes", got)
			}
			if strings.Contains(response.Body.String(), "source-code") {
				t.Fatal("SVG file was rendered by the text viewer")
			}
		})
	}

	if !isImage("diagram.SVG") {
		t.Fatal("SVG should retain image classification in directory listings")
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
