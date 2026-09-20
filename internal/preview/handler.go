package preview

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"
)

type handler struct {
	target  remoteTarget
	remote  RemoteFS
	verbose bool
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.verbose {
		log.Printf("%s %s", r.Method, r.URL.RequestURI())
	}

	rel := cleanRelativeURLPath(r.URL.Path)
	remotePath := path.Join(h.target.Root, rel)

	kind, err := h.remote.Kind(r.Context(), remotePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	switch kind {
	case "dir":
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.EscapedPath()+"/", http.StatusMovedPermanently)
			return
		}
		h.serveDirectory(w, r, rel, remotePath)
	case "file":
		h.serveFile(w, r, rel, remotePath)
	default:
		http.NotFound(w, r)
	}
}

func (h *handler) serveDirectory(w http.ResponseWriter, r *http.Request, rel, remotePath string) {
	entries, err := h.remote.List(r.Context(), remotePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		return
	}

	type viewEntry struct {
		Name string
		Href string
		Icon string
		Kind string
	}
	items := make([]viewEntry, 0, len(entries))
	for _, entry := range entries {
		href := pathURLJoin(r.URL.EscapedPath(), entry.Name)
		icon := "📄"
		kindLabel := "file"
		if entry.Kind == "dir" {
			href += "/"
			icon = "📁"
			kindLabel = "directory"
		} else if isMarkdown(entry.Name) {
			icon = "Ⓜ️"
			kindLabel = "markdown"
		} else if isImage(entry.Name) {
			icon = "🖼️"
			kindLabel = "image"
		} else if isHTML(entry.Name) {
			icon = "🌐"
			kindLabel = "html"
		} else if isPlainText(entry.Name) {
			href += "?view=1"
			kindLabel = "text"
		}
		items = append(items, viewEntry{Name: entry.Name, Href: href, Icon: icon, Kind: kindLabel})
	}

	data := struct {
		Title      string
		Host       string
		RemotePath string
		Breadcrumb template.HTML
		Parent     string
		Entries    []viewEntry
	}{
		Title:      directoryTitle(rel),
		Host:       h.target.Host,
		RemotePath: remotePath,
		Breadcrumb: breadcrumbHTML(r.URL.EscapedPath(), true),
		Parent:     parentURL(r.URL.EscapedPath()),
		Entries:    items,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := directoryTemplate.Execute(w, data); err != nil {
		log.Printf("render directory: %v", err)
	}
}

func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, rel, remotePath string) {
	data, err := h.remote.Read(r.Context(), remotePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	if r.URL.Query().Get("raw") == "1" {
		serveRaw(w, r, remotePath, data)
		return
	}

	if isMarkdown(remotePath) {
		h.serveMarkdown(w, r, rel, remotePath, data)
		return
	}

	if isPlainText(remotePath) && !isHTML(remotePath) && r.URL.Query().Get("view") == "1" {
		h.serveText(w, r, rel, remotePath, data)
		return
	}

	serveRaw(w, r, remotePath, data)
}

func (h *handler) serveMarkdown(w http.ResponseWriter, r *http.Request, rel, remotePath string, source []byte) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		return
	}

	jsonSource, _ := json.Marshal(string(source))
	data := struct {
		Name       string
		Host       string
		RemotePath string
		Breadcrumb template.HTML
		RawURL     string
		SourceJSON template.JS
	}{
		Name:       path.Base(remotePath),
		Host:       h.target.Host,
		RemotePath: remotePath,
		Breadcrumb: breadcrumbHTML(r.URL.EscapedPath(), false),
		RawURL:     withRawQuery(r.URL),
		SourceJSON: template.JS(jsonSource),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := markdownTemplate.Execute(w, data); err != nil {
		log.Printf("render markdown: %v", err)
	}
}

func (h *handler) serveText(w http.ResponseWriter, r *http.Request, rel, remotePath string, source []byte) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		return
	}

	data := struct {
		Name       string
		Host       string
		RemotePath string
		Breadcrumb template.HTML
		RawURL     string
		Source     string
	}{
		Name:       path.Base(remotePath),
		Host:       h.target.Host,
		RemotePath: remotePath,
		Breadcrumb: breadcrumbHTML(r.URL.EscapedPath(), false),
		RawURL:     withRawQuery(r.URL),
		Source:     string(source),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := textTemplate.Execute(w, data); err != nil {
		log.Printf("render text: %v", err)
	}
}

func serveRaw(w http.ResponseWriter, r *http.Request, remotePath string, data []byte) {
	if ctype := mime.TypeByExtension(strings.ToLower(path.Ext(remotePath))); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func isMarkdown(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	default:
		return false
	}
}

func isHTML(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".html", ".htm":
		return true
	default:
		return false
	}
}

func isImage(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".avif":
		return true
	default:
		return false
	}
}

func isPlainText(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".txt", ".log", ".json", ".yaml", ".yml", ".toml", ".xml", ".csv", ".go", ".rs", ".c", ".h", ".cpp", ".hpp", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".css", ".scss", ".sh", ".bash", ".zsh", ".fish", ".sql", ".conf", ".ini", ".env":
		return true
	default:
		name := strings.ToLower(path.Base(p))
		return name == "makefile" || name == "dockerfile" || name == "license" || name == "readme"
	}
}
