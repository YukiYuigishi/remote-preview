package preview

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
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
		} else if isCSV(entry.Name) {
			icon = "📊"
			kindLabel = "CSV table"
		} else if isTSV(entry.Name) {
			icon = "📊"
			kindLabel = "TSV table"
		} else if isJSONLines(entry.Name) {
			kindLabel = "JSON lines"
		} else if isAudio(entry.Name) {
			icon = "🔊"
			kindLabel = "audio"
		} else if isVideo(entry.Name) {
			icon = "🎞️"
			kindLabel = "video"
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
	if r.Method == http.MethodHead {
		if isMedia(remotePath) && r.URL.Query().Get("raw") != "1" {
			h.serveMedia(w, r, rel, remotePath)
			return
		}
		if r.URL.Query().Get("raw") == "1" || isHTML(remotePath) || isSVG(remotePath) || (!isMarkdown(remotePath) && !isCSV(remotePath) && !isTSV(remotePath) && !isPlainText(remotePath)) {
			h.serveRawFile(w, r, remotePath)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		return
	}
	if isMedia(remotePath) && r.URL.Query().Get("raw") != "1" {
		h.serveMedia(w, r, rel, remotePath)
		return
	}
	if r.URL.Query().Get("raw") == "1" || isHTML(remotePath) || isSVG(remotePath) {
		h.serveRawFile(w, r, remotePath)
		return
	}

	if !isPlainText(remotePath) && !isCSV(remotePath) && !isTSV(remotePath) && !isJSONLines(remotePath) {
		prefix, err := h.readFilePrefix(r.Context(), remotePath, 1024)
		if err != nil {
			h.fileReadError(w, err)
			return
		}
		if !isLikelyText(prefix) {
			h.serveRawFile(w, r, remotePath)
			return
		}
	}

	data, err := h.remote.Read(r.Context(), remotePath)
	if err != nil {
		h.fileReadError(w, err)
		return
	}

	if isMarkdown(remotePath) {
		h.serveMarkdown(w, r, rel, remotePath, data)
		return
	}

	if isCSV(remotePath) {
		h.serveDelimited(w, r, remotePath, data, ',')
		return
	}
	if isTSV(remotePath) {
		h.serveDelimited(w, r, remotePath, data, '\t')
		return
	}

	if isTextFile(remotePath, data) {
		h.serveText(w, r, rel, remotePath, data)
		return
	}

	h.serveRawFile(w, r, remotePath)
}

func (h *handler) fileReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	http.Error(w, err.Error(), http.StatusBadGateway)
}

func (h *handler) serveMedia(w http.ResponseWriter, r *http.Request, rel, remotePath string) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		return
	}
	mediaKind := "audio"
	if isVideo(remotePath) {
		mediaKind = "video"
	}
	data := struct {
		Name        string
		Host        string
		RemotePath  string
		Breadcrumb  template.HTML
		Kind        string
		ContentType string
		RawURL      string
		DownloadURL string
	}{
		Name:        path.Base(remotePath),
		Host:        h.target.Host,
		RemotePath:  remotePath,
		Breadcrumb:  breadcrumbHTML(r.URL.EscapedPath(), false),
		Kind:        mediaKind,
		ContentType: explicitMediaType(remotePath),
		RawURL:      withQueryValue(r.URL, "raw", "1"),
		DownloadURL: transferURLPrefix + "download?path=" + url.QueryEscape(rel),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := mediaTemplate.Execute(w, data); err != nil {
		slog.Error("render media preview", "error", err)
	}
}

func (h *handler) readFilePrefix(ctx context.Context, remotePath string, limit int64) ([]byte, error) {
	if source := h.rangeSource(); source != nil {
		size, err := source.Size(ctx, remotePath)
		if err != nil {
			return nil, err
		}
		if size > limit {
			size = limit
		}
		reader, err := source.OpenRange(ctx, remotePath, 0, size)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(io.LimitReader(reader, limit))
	}
	data, err := h.remote.Read(ctx, remotePath)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		data = data[:limit]
	}
	return data, nil
}

func (h *handler) rangeSource() rangeStreamFS {
	if source, ok := h.remote.(rangeStreamFS); ok && supportsRangeStream(h.remote) {
		return source
	}
	if source, ok := h.transfer.(rangeStreamFS); ok && supportsRangeStream(h.transfer) {
		return source
	}
	return nil
}

func supportsRangeStream(filesystem any) bool {
	if cached, ok := filesystem.(*cachedRemoteFS); ok {
		return supportsRangeStream(cached.backend)
	}
	_, ok := filesystem.(rangeStreamFS)
	return ok
}

func (h *handler) serveRawFile(w http.ResponseWriter, r *http.Request, remotePath string) {
	contentType := rawContentType(remotePath)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "bytes")

	source := h.rangeSource()
	var size int64
	var buffered []byte
	if source != nil {
		var err error
		size, err = source.Size(r.Context(), remotePath)
		if err != nil {
			h.fileReadError(w, err)
			return
		}
	} else if r.Method == http.MethodHead {
		// Legacy RemoteFS fakes do not necessarily expose size metadata. Keep
		// HEAD free of reads and opens when the backend cannot answer its size.
		return
	} else {
		var err error
		buffered, err = h.remote.Read(r.Context(), remotePath)
		if err != nil {
			h.fileReadError(w, err)
			return
		}
		size = int64(len(buffered))
	}

	var start, end int64
	partial := false
	rangeValues := r.Header.Values("Range")
	if len(rangeValues) > 0 && strings.TrimSpace(strings.Join(rangeValues, ",")) != "" {
		var err error
		if len(rangeValues) != 1 {
			err = fmt.Errorf("multiple Range headers are not supported")
		} else {
			start, end, err = parseSingleByteRange(rangeValues[0], size)
		}
		if err != nil {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		partial = true
	} else if size > 0 {
		end = size - 1
	}
	length := int64(0)
	if size > 0 {
		length = end - start + 1
	}
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	}
	if r.Method == http.MethodHead || length == 0 {
		if partial {
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		return
	}

	var reader io.ReadCloser
	if source != nil {
		var err error
		reader, err = source.OpenRange(r.Context(), remotePath, start, length)
		if err != nil {
			h.fileReadError(w, err)
			return
		}
	} else {
		reader = io.NopCloser(bytes.NewReader(buffered[start : start+length]))
	}
	defer reader.Close()
	if partial {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if _, err := copyWithContext(r.Context(), w, io.LimitReader(reader, length)); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		slog.Debug("raw file stream ended with error", "remote_path", remotePath, "error", err)
	}
}

func parseSingleByteRange(header string, size int64) (int64, int64, error) {
	if size < 0 {
		return 0, 0, fmt.Errorf("invalid file size")
	}
	unit, spec, ok := strings.Cut(strings.TrimSpace(header), "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(unit), "bytes") || strings.Contains(spec, ",") {
		return 0, 0, fmt.Errorf("unsupported byte range %q", header)
	}
	spec = strings.TrimSpace(spec)
	left, right, ok := strings.Cut(spec, "-")
	if !ok || strings.Contains(right, "-") || (left == "" && right == "") || size == 0 {
		return 0, 0, fmt.Errorf("unsatisfiable byte range %q", header)
	}
	if left == "" {
		suffix, err := parseUnsignedRangeNumber(right)
		if err != nil || suffix == 0 {
			return 0, 0, fmt.Errorf("unsatisfiable byte range %q", header)
		}
		start := int64(0)
		if suffix < size {
			start = size - suffix
		}
		return start, size - 1, nil
	}
	start, err := parseUnsignedRangeNumber(left)
	if err != nil || start >= size {
		return 0, 0, fmt.Errorf("unsatisfiable byte range %q", header)
	}
	end := size - 1
	if right != "" {
		requestedEnd, parseErr := parseUnsignedRangeNumber(right)
		if parseErr != nil || requestedEnd < start {
			return 0, 0, fmt.Errorf("unsatisfiable byte range %q", header)
		}
		if requestedEnd < end {
			end = requestedEnd
		}
	}
	return start, end, nil
}

func parseUnsignedRangeNumber(value string) (int64, error) {
	if value == "" {
		return 0, fmt.Errorf("empty range number")
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("invalid range number %q", value)
		}
	}
	return strconv.ParseInt(value, 10, 64)
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
	h.serveTextWithNotice(w, r, rel, remotePath, source, "")
}

func (h *handler) serveTextWithNotice(w http.ResponseWriter, r *http.Request, rel, remotePath string, source []byte, notice string) {
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
		Notice       string
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
		Notice:       notice,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := textTemplate.Execute(w, data); err != nil {
		slog.Error("render text", "error", err)
	}
}

const (
	maxDelimitedPreviewRows       = 200
	maxDelimitedPreviewColumns    = 40
	maxDelimitedPreviewFieldBytes = 4096
)

type delimitedPreview struct {
	Name         string
	Host         string
	RemotePath   string
	Breadcrumb   template.HTML
	RawURL       string
	SourceURL    string
	TableURL     string
	Source       string
	Headers      []string
	Rows         [][]string
	SourceView   bool
	Empty        bool
	LimitMessage string
}

type parsedDelimitedPreview struct {
	Headers         []string
	Rows            [][]string
	Empty           bool
	RowsOmitted     bool
	ColumnsOmitted  bool
	FieldsTruncated bool
}

func (h *handler) serveDelimited(w http.ResponseWriter, r *http.Request, remotePath string, source []byte, separator rune) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		return
	}

	viewSource := r.URL.Query().Get("view") == "source"
	preview := parsedDelimitedPreview{Empty: true}
	if !viewSource {
		var err error
		preview, err = parseDelimitedPreview(source, separator)
		if err != nil {
			format := "CSV"
			if separator == '\t' {
				format = "TSV"
			}
			h.serveTextWithNotice(w, r, "", remotePath, source, format+" could not be parsed. Showing source.")
			return
		}
	}

	data := delimitedPreview{
		Name:       path.Base(remotePath),
		Host:       h.target.Host,
		RemotePath: remotePath,
		Breadcrumb: breadcrumbHTML(r.URL.EscapedPath(), false),
		RawURL:     withRawQuery(r.URL),
		SourceURL:  withQueryValue(r.URL, "view", "source"),
		TableURL:   withQueryValue(r.URL, "view", "table"),
		Source:     string(source),
		Headers:    preview.Headers,
		Rows:       preview.Rows,
		SourceView: viewSource,
		Empty:      preview.Empty,
	}
	if preview.RowsOmitted || preview.ColumnsOmitted || preview.FieldsTruncated {
		data.LimitMessage = fmt.Sprintf("Preview limits: up to %d records, %d columns, and %d bytes per field. Some content is omitted.", maxDelimitedPreviewRows, maxDelimitedPreviewColumns, maxDelimitedPreviewFieldBytes)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := delimitedTemplate.Execute(w, data); err != nil {
		slog.Error("render delimited preview", "error", err)
	}
}

func parseDelimitedPreview(source []byte, separator rune) (parsedDelimitedPreview, error) {
	reader := csv.NewReader(bytes.NewReader(source))
	reader.Comma = separator
	reader.FieldsPerRecord = -1

	result := parsedDelimitedPreview{}
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			result.Empty = true
			return result, nil
		}
		return result, err
	}
	result.Empty = false
	header = limitDelimitedRecord(header, &result)

	var records [][]string
	for len(records) < maxDelimitedPreviewRows {
		record, readErr := reader.Read()
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return parsedDelimitedPreview{}, readErr
		}
		records = append(records, limitDelimitedRecord(record, &result))
	}
	if len(records) == maxDelimitedPreviewRows {
		_, readErr := reader.Read()
		if readErr == nil {
			result.RowsOmitted = true
		} else if readErr != io.EOF {
			return parsedDelimitedPreview{}, readErr
		}
	}

	columnCount := len(header)
	for _, record := range records {
		if len(record) > columnCount {
			columnCount = len(record)
		}
	}
	if columnCount > maxDelimitedPreviewColumns {
		columnCount = maxDelimitedPreviewColumns
		result.ColumnsOmitted = true
	}
	result.Headers = make([]string, columnCount)
	for i := range result.Headers {
		if i < len(header) {
			result.Headers[i] = header[i]
		} else {
			result.Headers[i] = fmt.Sprintf("Column %d", i+1)
		}
	}
	result.Rows = make([][]string, len(records))
	for rowIndex, record := range records {
		row := make([]string, columnCount)
		copy(row, record)
		result.Rows[rowIndex] = row
	}
	return result, nil
}

func limitDelimitedRecord(record []string, preview *parsedDelimitedPreview) []string {
	if len(record) > maxDelimitedPreviewColumns {
		preview.ColumnsOmitted = true
		record = record[:maxDelimitedPreviewColumns]
	}
	for i, field := range record {
		if len(field) > maxDelimitedPreviewFieldBytes {
			end := maxDelimitedPreviewFieldBytes
			if utf8.ValidString(field) {
				for end > 0 && !utf8.RuneStart(field[end]) {
					end--
				}
			}
			record[i] = strings.ToValidUTF8(field[:end], "�") + "…"
			preview.FieldsTruncated = true
		}
	}
	return record
}

func isCSV(p string) bool {
	return strings.EqualFold(path.Ext(p), ".csv")
}

func isTSV(p string) bool {
	return strings.EqualFold(path.Ext(p), ".tsv")
}

func isJSONLines(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	return ext == ".jsonl" || ext == ".ndjson"
}

func withQueryValue(u *url.URL, key, value string) string {
	q := u.Query()
	if value == "" {
		q.Del(key)
	} else {
		q.Set(key, value)
	}
	copy := *u
	copy.RawQuery = q.Encode()
	return copy.RequestURI()
}

var explicitMediaTypes = map[string]string{
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".m4a":  "audio/mp4",
	".flac": "audio/flac",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".ogv":  "video/ogg",
}

func explicitMediaType(p string) string {
	return explicitMediaTypes[strings.ToLower(path.Ext(p))]
}

func rawContentType(p string) string {
	if mediaType := explicitMediaType(p); mediaType != "" {
		return mediaType
	}
	if isSVG(p) {
		return "image/svg+xml"
	}
	if contentType := mime.TypeByExtension(strings.ToLower(path.Ext(p))); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
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

func isAudio(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".mp3", ".wav", ".ogg", ".m4a", ".flac":
		return true
	default:
		return false
	}
}

func isVideo(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".mp4", ".webm", ".mov", ".ogv":
		return true
	default:
		return false
	}
}

func isMedia(p string) bool {
	return isAudio(p) || isVideo(p)
}

func isSVG(p string) bool {
	return strings.EqualFold(path.Ext(p), ".svg")
}

func isPlainText(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".txt", ".text", ".log", ".json", ".jsonl", ".ndjson", ".yaml", ".yml", ".toml", ".xml", ".csv", ".tsv", ".go", ".rs", ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".java", ".kt", ".kts", ".swift", ".rb", ".php", ".pl", ".lua", ".r", ".scala", ".ex", ".exs", ".erl", ".hrl", ".hs", ".fs", ".fsx", ".vb", ".groovy", ".gradle", ".css", ".scss", ".less", ".sh", ".bash", ".zsh", ".fish", ".bat", ".cmd", ".ps1", ".sql", ".conf", ".ini", ".properties", ".env", ".lock", ".patch", ".diff", ".tex", ".rst", ".adoc", ".graphql", ".gql", ".proto", ".tf", ".hcl", ".vim":
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
	case ".json", ".jsonl", ".ndjson":
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
