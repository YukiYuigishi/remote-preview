package preview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type handler struct {
	target        remoteTarget
	remote        RemoteFS
	transfer      transferFS
	writeEnabled  bool
	maxUploadSize int64
	uploads       *uploadSessionStore
	uploadsMu     sync.Mutex
	verbose       bool
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	defer func() {
		slog.Debug("http request done", "method", r.Method, "uri", r.URL.RequestURI(), "duration", time.Since(started))
	}()

	if servePreviewAsset(w, r) {
		return
	}
	if strings.HasPrefix(r.URL.Path, transferURLPrefix) {
		h.serveTransfer(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.verbose {
		slog.Info("http request", "method", r.Method, "uri", r.URL.RequestURI())
	}

	rel := cleanRelativeURLPath(r.URL.Path)
	remotePath := path.Join(h.target.Root, rel)
	slog.Debug("http request start", "method", r.Method, "uri", r.URL.RequestURI(), "remote_path", remotePath)

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
	slog.Debug("directory listing ready", "remote_path", remotePath, "entries", len(entries))

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		return
	}

	type viewEntry struct {
		Name        string
		Href        string
		Icon        string
		Kind        string
		SelectPath  string
		DownloadURL string
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
			kindLabel = "text"
		}
		selectPath, pathErr := joinTransferRelativePath(rel, entry.Name)
		if pathErr != nil {
			slog.Debug("skip entry with invalid transfer path", "name", entry.Name, "error", pathErr)
			continue
		}
		downloadURL := transferURLPrefix + "download?path=" + url.QueryEscape(selectPath)
		items = append(items, viewEntry{
			Name:        entry.Name,
			Href:        href,
			Icon:        icon,
			Kind:        kindLabel,
			SelectPath:  selectPath,
			DownloadURL: downloadURL,
		})
	}

	data := struct {
		Title          string
		Host           string
		RemotePath     string
		Breadcrumb     template.HTML
		Parent         string
		Entries        []viewEntry
		CurrentPath    string
		UploadURL      string
		ChunkUploadURL string
		WriteEnabled   bool
	}{
		Title:          directoryTitle(rel),
		Host:           h.target.Host,
		RemotePath:     remotePath,
		Breadcrumb:     breadcrumbHTML(r.URL.EscapedPath(), true),
		Parent:         parentURL(r.URL.EscapedPath()),
		Entries:        items,
		CurrentPath:    rel,
		UploadURL:      transferURLPrefix + "upload?directory=" + url.QueryEscape(rel),
		ChunkUploadURL: transferURLPrefix + "upload-chunk?directory=" + url.QueryEscape(rel),
		WriteEnabled:   h.writeEnabled,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := directoryTemplate.Execute(w, data); err != nil {
		slog.Error("render directory", "error", err)
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

	if isHTML(remotePath) {
		serveRaw(w, r, remotePath, data)
		return
	}

	if isTextFile(remotePath, data) {
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
		slog.Error("render markdown", "error", err)
	}
}

func (h *handler) serveText(w http.ResponseWriter, r *http.Request, rel, remotePath string, source []byte) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		return
	}

	jsonSource, _ := json.Marshal(string(source))
	jsonLanguage, _ := json.Marshal(syntaxLanguage(remotePath))
	data := struct {
		Name         string
		Host         string
		RemotePath   string
		Breadcrumb   template.HTML
		RawURL       string
		Source       string
		SourceJSON   template.JS
		Language     string
		LanguageJSON template.JS
	}{
		Name:         path.Base(remotePath),
		Host:         h.target.Host,
		RemotePath:   remotePath,
		Breadcrumb:   breadcrumbHTML(r.URL.EscapedPath(), false),
		RawURL:       withRawQuery(r.URL),
		Source:       string(source),
		SourceJSON:   template.JS(jsonSource),
		Language:     syntaxLanguage(remotePath),
		LanguageJSON: template.JS(jsonLanguage),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := textTemplate.Execute(w, data); err != nil {
		slog.Error("render text", "error", err)
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
	case ".txt", ".text", ".log", ".json", ".yaml", ".yml", ".toml", ".xml", ".csv", ".tsv", ".go", ".rs", ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".java", ".kt", ".kts", ".swift", ".rb", ".php", ".pl", ".lua", ".r", ".scala", ".ex", ".exs", ".erl", ".hrl", ".hs", ".fs", ".fsx", ".vb", ".groovy", ".gradle", ".css", ".scss", ".less", ".sh", ".bash", ".zsh", ".fish", ".bat", ".cmd", ".ps1", ".sql", ".conf", ".ini", ".properties", ".env", ".lock", ".patch", ".diff", ".tex", ".rst", ".adoc", ".graphql", ".gql", ".proto", ".tf", ".hcl", ".vim":
		return true
	default:
		name := strings.ToLower(path.Base(p))
		return name == "makefile" || name == "dockerfile" || name == "license" || name == "readme" || name == ".gitignore" || name == ".gitattributes" || name == ".gitconfig" || name == ".editorconfig" || name == ".npmrc" || name == ".nvmrc" || name == ".prettierrc"
	}
}

func isTextFile(p string, data []byte) bool {
	return isPlainText(p) || isLikelyText(data)
}

func isLikelyText(data []byte) bool {
	if len(data) == 0 || bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return len(data) == 0
	}
	controlBytes := 0
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != '\f' {
			controlBytes++
		}
	}
	return controlBytes*20 <= len(data)
}

func syntaxLanguage(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "ini"
	case ".xml":
		return "xml"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".swift":
		return "swift"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".lua":
		return "lua"
	case ".css":
		return "css"
	case ".scss":
		return "scss"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	case ".sql":
		return "sql"
	case ".conf", ".ini", ".properties":
		return "ini"
	case ".diff", ".patch":
		return "diff"
	case ".graphql", ".gql":
		return "graphql"
	case ".proto":
		return "protobuf"
	case ".tf", ".hcl":
		return "hcl"
	default:
		name := strings.ToLower(path.Base(p))
		if name == "dockerfile" {
			return "dockerfile"
		}
		if name == "makefile" {
			return "makefile"
		}
		return ""
	}
}
