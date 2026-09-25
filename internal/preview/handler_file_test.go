package preview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type fakeRangeRemoteFS struct {
	*fakeRemoteFS
	content   []byte
	sizeCalls int
	openCalls int
	ranges    [][2]int64
}

func (f *fakeRangeRemoteFS) Size(context.Context, string) (int64, error) {
	f.sizeCalls++
	return int64(len(f.content)), nil
}

func (f *fakeRangeRemoteFS) OpenRange(_ context.Context, _ string, offset, length int64) (io.ReadCloser, error) {
	f.openCalls++
	f.ranges = append(f.ranges, [2]int64{offset, length})
	end := int64(len(f.content))
	if length >= 0 && offset+length < end {
		end = offset + length
	}
	if offset > int64(len(f.content)) || end < offset {
		return nil, errors.New("invalid range")
	}
	return io.NopCloser(bytes.NewReader(f.content[offset:end])), nil
}

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

func TestHandlerServesMediaViewerBeforeReadingFile(t *testing.T) {
	for _, test := range []struct {
		name        string
		contentType string
		viewer      string
	}{
		{name: "track.MP3", contentType: "audio/mpeg", viewer: `<audio controls preload="metadata">`},
		{name: "clip.MOV", contentType: "video/quicktime", viewer: `<video controls preload="metadata">`},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := newFakeRemoteFS()
			remotePath := "/root/" + test.name
			backend.kinds[remotePath] = "file"
			backend.readErr[remotePath] = errors.New("media content should not be read for the viewer")
			h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+test.name, nil))
			body := response.Body.String()
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("response=%d content-type=%q body=%q", response.Code, response.Header().Get("Content-Type"), body)
			}
			for _, want := range []string{test.viewer, test.contentType, "raw=1", "Download", "cannot play this"} {
				if !strings.Contains(body, want) {
					t.Fatalf("media viewer missing %q: %q", want, body)
				}
			}
		})
	}
}

func TestHandlerHEADDoesNotReadPreviewFile(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/notes.txt"] = "file"
	backend.readErr["/root/notes.txt"] = errors.New("HEAD must not read file contents")
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/notes.txt", nil))
	if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("HEAD response=%d content-type=%q body=%q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestHandlerReportsBackendDeadlineWhileRequestIsActive(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/notes.txt"] = "file"
	backend.readErr["/root/notes.txt"] = context.DeadlineExceeded
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/notes.txt", nil))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("backend deadline response=%d body=%q, want 502", response.Code, response.Body.String())
	}
}

func TestHandlerStreamsRawRangesAndKeepsHEADOpenFree(t *testing.T) {
	base := newFakeRemoteFS()
	base.kinds["/root/track.mp3"] = "file"
	backend := &fakeRangeRemoteFS{fakeRemoteFS: base, content: []byte("0123456789")}
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	full := httptest.NewRecorder()
	h.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/track.mp3?raw=1", nil))
	if full.Code != http.StatusOK || full.Body.String() != "0123456789" || full.Header().Get("Content-Type") != "audio/mpeg" || full.Header().Get("Accept-Ranges") != "bytes" || full.Header().Get("Content-Length") != "10" {
		t.Fatalf("full response=%d headers=%v body=%q", full.Code, full.Header(), full.Body.String())
	}

	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/track.mp3?raw=1", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "10" {
		t.Fatalf("HEAD response=%d headers=%v body=%q", head.Code, head.Header(), head.Body.String())
	}
	if backend.openCalls != 1 {
		t.Fatalf("open calls after HEAD=%d, want only the full GET", backend.openCalls)
	}

	for _, test := range []struct {
		header     string
		wantBody   string
		wantRange  string
		wantOffset int64
		wantLength int64
	}{
		{header: "bytes=2-5", wantBody: "2345", wantRange: "bytes 2-5/10", wantOffset: 2, wantLength: 4},
		{header: "bytes=7-", wantBody: "789", wantRange: "bytes 7-9/10", wantOffset: 7, wantLength: 3},
		{header: "bytes=-4", wantBody: "6789", wantRange: "bytes 6-9/10", wantOffset: 6, wantLength: 4},
	} {
		request := httptest.NewRequest(http.MethodGet, "/track.mp3?raw=1", nil)
		request.Header.Set("Range", test.header)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusPartialContent || response.Body.String() != test.wantBody || response.Header().Get("Content-Range") != test.wantRange || response.Header().Get("Content-Length") != strconv.FormatInt(test.wantLength, 10) {
			t.Fatalf("Range %q response=%d headers=%v body=%q", test.header, response.Code, response.Header(), response.Body.String())
		}
		got := backend.ranges[len(backend.ranges)-1]
		if got != ([2]int64{test.wantOffset, test.wantLength}) {
			t.Fatalf("Range %q opened offset/length=%v", test.header, got)
		}
	}

	unsatisfiable := httptest.NewRequest(http.MethodGet, "/track.mp3?raw=1", nil)
	unsatisfiable.Header.Set("Range", "bytes=10-")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, unsatisfiable)
	if response.Code != http.StatusRequestedRangeNotSatisfiable || response.Header().Get("Content-Range") != "bytes */10" || response.Body.Len() != 0 {
		t.Fatalf("416 response=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	if backend.openCalls != 4 {
		t.Fatalf("range open calls=%d, want full GET plus three valid ranges", backend.openCalls)
	}
}

func TestLocalHandlerStreamsRawByteRange(t *testing.T) {
	root := t.TempDir()
	content := []byte("0123456789")
	if err := os.WriteFile(filepath.Join(root, "track.mp3"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	local := newCachedRemoteFS(newLocalRemoteFS(), "local")
	h := &handler{target: remoteTarget{Host: "local", Root: root}, remote: local, transfer: local}

	for _, test := range []struct {
		method     string
		rangeValue string
		status     int
		body       string
		contentLen string
		contentRng string
	}{
		{method: http.MethodGet, status: http.StatusOK, body: string(content), contentLen: "10"},
		{method: http.MethodHead, status: http.StatusOK, contentLen: "10"},
		{method: http.MethodGet, rangeValue: "bytes=4-6", status: http.StatusPartialContent, body: "456", contentLen: "3", contentRng: "bytes 4-6/10"},
	} {
		request := httptest.NewRequest(test.method, "/track.mp3?raw=1", nil)
		if test.rangeValue != "" {
			request.Header.Set("Range", test.rangeValue)
		}
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != test.status || response.Body.String() != test.body || response.Header().Get("Content-Length") != test.contentLen || response.Header().Get("Content-Range") != test.contentRng {
			t.Fatalf("%s Range %q response=%d headers=%v body=%q", test.method, test.rangeValue, response.Code, response.Header(), response.Body.String())
		}
	}
}

func TestHandlerKeepsLegacyFakeRemoteRangeFallback(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root/archive.bin"] = "file"
	backend.reads["/root/archive.bin"] = []byte("abcdef")
	cached := newCachedRemoteFS(backend, "remote-host")
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: cached}
	request := httptest.NewRequest(http.MethodGet, "/archive.bin?raw=1", nil)
	request.Header.Set("Range", "bytes=2-4")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "cde" || response.Header().Get("Content-Range") != "bytes 2-4/6" {
		t.Fatalf("legacy fake response=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
}

func TestMediaClassificationAndExplicitContentTypes(t *testing.T) {
	for _, name := range []string{"a.mp3", "a.wav", "a.ogg", "a.m4a", "a.flac"} {
		if !isAudio(name) || isVideo(name) || !strings.HasPrefix(explicitMediaType(name), "audio/") {
			t.Errorf("audio classification/type failed for %q: audio=%v video=%v type=%q", name, isAudio(name), isVideo(name), explicitMediaType(name))
		}
	}
	for _, name := range []string{"a.mp4", "a.webm", "a.mov", "a.ogv"} {
		if !isVideo(name) || isAudio(name) || !strings.HasPrefix(explicitMediaType(name), "video/") {
			t.Errorf("video classification/type failed for %q: audio=%v video=%v type=%q", name, isAudio(name), isVideo(name), explicitMediaType(name))
		}
	}
}

func TestParseSingleByteRange(t *testing.T) {
	for _, test := range []struct {
		header    string
		size      int64
		start     int64
		end       int64
		wantError bool
	}{
		{header: "bytes=3-7", size: 10, start: 3, end: 7},
		{header: "bytes=7-", size: 10, start: 7, end: 9},
		{header: "bytes=-4", size: 10, start: 6, end: 9},
		{header: "bytes=3-99", size: 10, start: 3, end: 9},
		{header: "bytes=10-", size: 10, wantError: true},
		{header: "bytes=-0", size: 10, wantError: true},
		{header: "bytes=0-1,4-5", size: 10, wantError: true},
		{header: "items=0-1", size: 10, wantError: true},
		{header: "bytes=0-", size: 0, wantError: true},
	} {
		start, end, err := parseSingleByteRange(test.header, test.size)
		if test.wantError {
			if err == nil {
				t.Errorf("parseSingleByteRange(%q, %d) unexpectedly succeeded", test.header, test.size)
			}
			continue
		}
		if err != nil || start != test.start || end != test.end {
			t.Errorf("parseSingleByteRange(%q, %d)=(%d,%d,%v), want (%d,%d,nil)", test.header, test.size, start, end, err, test.start, test.end)
		}
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
		{Name: "track.flac", Kind: "file"},
		{Name: "clip.webm", Kind: "file"},
	}
	h := &handler{target: remoteTarget{Host: "remote-host", Root: "/root"}, remote: backend}

	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	for _, want := range []string{"CSV table", "TSV table", "JSON lines", "audio", "video"} {
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
