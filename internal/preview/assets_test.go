package preview

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServePreviewAsset(t *testing.T) {
	h := &handler{}
	request := httptest.NewRequest(http.MethodGet, previewAssetPrefix+"marked.js", nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("content type=%q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("cache control=%q", got)
	}
	if !strings.Contains(response.Body.String(), "marked") {
		t.Fatal("embedded marked asset was not served")
	}
}

func TestServePreviewAssetSupportsHeadAndRejectsUnknownAssets(t *testing.T) {
	h := &handler{}
	headResponse := httptest.NewRecorder()
	h.ServeHTTP(headResponse, httptest.NewRequest(http.MethodHead, previewAssetPrefix+"highlight.css", nil))
	if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 {
		t.Fatalf("HEAD response status=%d body=%d", headResponse.Code, headResponse.Body.Len())
	}
	if headResponse.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD response is missing Content-Length")
	}

	notFound := httptest.NewRecorder()
	h.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, previewAssetPrefix+"missing.js", nil))
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("unknown asset status=%d, want 404", notFound.Code)
	}
}

func TestPreviewTemplatesUseEmbeddedAssets(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "markdown", source: markdownTemplate.Tree.Root.String()},
		{name: "text", source: textTemplate.Tree.Root.String()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if strings.Contains(test.source, "cdn.jsdelivr.net") {
				t.Fatal("template still contains a runtime CDN URL")
			}
			if !strings.Contains(test.source, previewAssetPrefix) {
				t.Fatalf("template does not reference %s", previewAssetPrefix)
			}
		})
	}
}
