package preview

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerUsesCachedDirectoryListingAndEntryKind(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.kinds["/root"] = "dir"
	backend.lists["/root"] = []remoteEntry{{Name: "docs", Kind: "dir"}}
	backend.kinds["/root/docs"] = "dir"
	backend.lists["/root/docs"] = []remoteEntry{{Name: "readme.md", Kind: "file"}}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", listingCacheOptions{
		TTL:                    time.Minute,
		MaxEntries:             10,
		PrefetchMaxDirectories: 0,
	})
	h := &handler{
		target: remoteTarget{Host: "remote-host", Root: "/root"},
		remote: cached,
	}

	for _, requestPath := range []string{"/", "/"} {
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d, want 200", requestPath, response.Code)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/docs/", nil)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /docs/ status=%d, want 200", response.Code)
	}
	if got := backend.calls("/root"); got != 1 {
		t.Fatalf("root listing calls=%d, want 1", got)
	}
	if got := backend.kindCallCount("/root/docs"); got != 0 {
		t.Fatalf("docs kind calls=%d, want 0 from cached entry kind", got)
	}
	if got := backend.calls("/root/docs"); got != 1 {
		t.Fatalf("docs listing calls=%d, want 1", got)
	}
}
