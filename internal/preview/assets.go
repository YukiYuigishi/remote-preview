package preview

import (
	"embed"
	"net/http"
	"strconv"
	"strings"
)

// Browser preview assets are embedded into the local binary so the browser
// never needs an external CDN connection for the normal preview experience.
//
//go:embed assets/marked.js assets/mermaid.js assets/highlight.js assets/highlight.css
var previewAssetFiles embed.FS

// Bump this when the embedded asset set changes so browsers can keep using an
// immutable cached copy for the current binary.
const previewAssetPrefix = "/_remote-preview/assets/v1/"

var previewAssetContentTypes = map[string]string{
	"marked.js":     "text/javascript; charset=utf-8",
	"mermaid.js":    "text/javascript; charset=utf-8",
	"highlight.js":  "text/javascript; charset=utf-8",
	"highlight.css": "text/css; charset=utf-8",
}

func servePreviewAsset(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, previewAssetPrefix) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}

	name := strings.TrimPrefix(r.URL.Path, previewAssetPrefix)
	contentType, ok := previewAssetContentTypes[name]
	if !ok {
		http.NotFound(w, r)
		return true
	}
	data, err := previewAssetFiles.ReadFile("assets/" + name)
	if err != nil {
		http.NotFound(w, r)
		return true
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
	return true
}
