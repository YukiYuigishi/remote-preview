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

func TestHandlerServesCSVAsTableAndSource(t *testing.T) {
	content := "name,note\nAlice,\"contains, separator\nand newline\"\n"
	backend := newFakeRemoteFS()
	backend.kinds["/root/data.csv"] = "file"
	backend.reads["/root/data.csv"] = []byte(content)
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/data.csv", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("response=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	for _, want := range []string{"<th scope=\"col\">name</th>", "<th scope=\"col\">note</th>", "Alice", "contains, separator", "and newline", "view=source"} {
		if !strings.Contains(body, want) {
			t.Fatalf("table response missing %q: %q", want, body)
		}
	}

	sourceResponse := httptest.NewRecorder()
	h.ServeHTTP(sourceResponse, httptest.NewRequest(http.MethodGet, "/data.csv?view=source", nil))
	if sourceResponse.Code != http.StatusOK || !strings.Contains(sourceResponse.Body.String(), "<pre>name,note\nAlice,&#34;contains, separator") || !strings.Contains(sourceResponse.Body.String(), "and newline&#34;\n</pre>") {
		t.Fatalf("source response=%d body=%q", sourceResponse.Code, sourceResponse.Body.String())
	}
}

func TestHandlerServesTSVAndJSONLines(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/data.tsv"] = "file"
	backend.reads["/root/data.tsv"] = []byte("name\tnote\nAlice\t\"contains\ttab\"\n")
	for _, name := range []string{"events.jsonl", "events.ndjson"} {
		backend.kinds["/root/"+name] = "file"
		backend.reads["/root/"+name] = []byte("{\"ok\":true}\n")
	}
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	tsvResponse := httptest.NewRecorder()
	h.ServeHTTP(tsvResponse, httptest.NewRequest(http.MethodGet, "/data.tsv", nil))
	if tsvResponse.Code != http.StatusOK || !strings.Contains(tsvResponse.Body.String(), "contains\ttab") {
		t.Fatalf("TSV response=%d body=%q", tsvResponse.Code, tsvResponse.Body.String())
	}

	for _, name := range []string{"events.jsonl", "events.ndjson"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+name, nil))
		body := response.Body.String()
		if response.Code != http.StatusOK || !strings.Contains(body, `class="language-json"`) || !strings.Contains(body, `&#34;ok&#34;:true`) || !strings.Contains(body, `const language = "json";`) {
			t.Fatalf("%s response=%d body=%q", name, response.Code, body)
		}
	}
}

func TestHandlerFallsBackToSourceForMalformedCSV(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/bad.csv"] = "file"
	backend.reads["/root/bad.csv"] = []byte("name,note\n\"unfinished")
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/bad.csv", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "CSV could not be parsed. Showing source.") || !strings.Contains(body, `id="source-code"`) || !strings.Contains(body, "unfinished") {
		t.Fatalf("fallback response=%d body=%q", response.Code, body)
	}
}

func TestHandlerExplainsDelimitedPreviewLimitsAndKeepsSourceLink(t *testing.T) {
	content := "value\n" + strings.Repeat("record\n", maxDelimitedPreviewRows+1)
	backend := newFakeRemoteFS()
	backend.kinds["/root/large.csv"] = "file"
	backend.reads["/root/large.csv"] = []byte(content)
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/large.csv", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "Preview limits:") || !strings.Contains(body, "View full source") || !strings.Contains(body, "view=source") {
		t.Fatalf("limited response=%d body=%q", response.Code, body)
	}
	if got := strings.Count(body, "<td>record</td>"); got != maxDelimitedPreviewRows {
		t.Fatalf("rendered records=%d, want %d", got, maxDelimitedPreviewRows)
	}
}

func TestDirectoryListingClassifiesStructuredData(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root"] = "dir"
	backend.lists["/root"] = []remoteEntry{
		{Name: "data.csv", Kind: "file"},
		{Name: "data.tsv", Kind: "file"},
		{Name: "events.jsonl", Kind: "file"},
		{Name: "events.ndjson", Kind: "file"},
	}
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	for _, want := range []string{"CSV table", "TSV table", "JSON lines"} {
		if !strings.Contains(body, want) {
			t.Fatalf("directory listing missing %q: %q", want, body)
		}
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
	for _, name := range []string{"events.jsonl", "events.ndjson"} {
		if !isPlainText(name) || syntaxLanguage(name) != "json" {
			t.Fatalf("JSON Lines classification for %q: plain=%v language=%q", name, isPlainText(name), syntaxLanguage(name))
		}
	}
}
