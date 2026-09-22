package preview

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const transferURLPrefix = "/_ykview/transfer/"

func (h *handler) transferBackend() (transferFS, bool) {
	if h.transfer != nil {
		return h.transfer, true
	}
	backend, ok := h.remote.(transferFS)
	return backend, ok
}

func (h *handler) serveTransfer(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, transferURLPrefix) {
	case "download":
		h.serveDownload(w, r)
	case "download-zip":
		h.serveZIPDownload(w, r)
	case "upload":
		h.serveUpload(w, r)
	case "upload-chunk":
		h.serveUploadChunk(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *handler) transferPath(relative string) string {
	if h.target.Local {
		return filepath.Join(h.target.Root, filepath.FromSlash(relative))
	}
	return path.Join(h.target.Root, relative)
}

func (h *handler) serveDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, ok := r.URL.Query()["path"]
	if !ok || len(raw) != 1 {
		http.Error(w, "download path is required", http.StatusBadRequest)
		return
	}
	relative, err := validateTransferRelativePath(raw[0], true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	remotePath := h.transferPath(relative)
	kind, err := h.remote.Kind(r.Context(), remotePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	switch kind {
	case "file":
		h.serveDownloadFile(w, r, relative, remotePath)
	case "dir":
		h.serveArchive(w, r, []string{relative}, archiveName(relative))
	default:
		http.NotFound(w, r)
	}
}

func (h *handler) serveZIPDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	values := r.URL.Query()["path"]
	if len(values) == 0 {
		http.Error(w, "at least one download path is required", http.StatusBadRequest)
		return
	}
	paths := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		relative, err := validateTransferRelativePath(value, true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, exists := seen[relative]; exists {
			continue
		}
		seen[relative] = struct{}{}
		paths = append(paths, relative)
	}
	h.serveArchive(w, r, paths, "download.zip")
}

func (h *handler) serveDownloadFile(w http.ResponseWriter, r *http.Request, relative, remotePath string) {
	backend, ok := h.transferBackend()
	if !ok {
		http.Error(w, "transfer backend is unavailable", http.StatusNotImplemented)
		return
	}
	filename := path.Base(relative)
	if filename == "." || filename == "/" || filename == "" {
		filename = "download"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", transferContentDisposition(filename))
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	stream, info, err := backend.Open(r.Context(), remotePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()
	if info.Size >= 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size))
	}
	if _, err := copyWithContext(r.Context(), w, stream); err != nil {
		slog.Debug("file download failed", "path", remotePath, "error", err)
	}
}

type archiveEntry struct {
	relative  string
	remote    string
	directory bool
}

func archiveName(relative string) string {
	if relative == "" {
		return "download.zip"
	}
	name := path.Base(relative)
	if name == "." || name == "/" || name == "" {
		return "download.zip"
	}
	return name + ".zip"
}

func (h *handler) serveArchive(w http.ResponseWriter, r *http.Request, selections []string, filename string) {
	entries, err := h.collectArchiveEntries(r.Context(), selections)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", transferContentDisposition(filename))
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	archive := zip.NewWriter(w)
	for _, entry := range entries {
		name := entry.relative
		if entry.directory && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if entry.directory {
			header.Method = zip.Store
		}
		writer, createErr := archive.CreateHeader(header)
		if createErr != nil {
			_ = archive.Close()
			slog.Debug("zip entry creation failed", "path", entry.remote, "error", createErr)
			return
		}
		if entry.directory {
			continue
		}
		backend, ok := h.transferBackend()
		if !ok {
			_ = archive.Close()
			slog.Debug("zip download backend unavailable")
			return
		}
		stream, _, openErr := backend.Open(r.Context(), entry.remote)
		if openErr != nil {
			_ = archive.Close()
			slog.Debug("zip file open failed", "path", entry.remote, "error", openErr)
			return
		}
		_, copyErr := copyWithContext(r.Context(), writer, stream)
		closeErr := stream.Close()
		if copyErr != nil || closeErr != nil {
			_ = archive.Close()
			if copyErr != nil {
				slog.Debug("zip file copy failed", "path", entry.remote, "error", copyErr)
			} else {
				slog.Debug("zip file close failed", "path", entry.remote, "error", closeErr)
			}
			return
		}
	}
	if err := archive.Close(); err != nil {
		slog.Debug("zip close failed", "error", err)
	}
}

func (h *handler) collectArchiveEntries(ctx context.Context, selections []string) ([]archiveEntry, error) {
	entries := make([]archiveEntry, 0)
	seenEntries := make(map[string]struct{})
	visitedDirectories := make(map[string]struct{})
	for _, selection := range selections {
		if err := h.collectArchiveNode(ctx, selection, 0, &entries, seenEntries, visitedDirectories); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (h *handler) collectArchiveNode(ctx context.Context, relative string, depth int, entries *[]archiveEntry, seenEntries, visitedDirectories map[string]struct{}) error {
	if depth > maxArchiveDepth {
		return fmt.Errorf("archive path is too deep")
	}
	remotePath := h.transferPath(relative)
	kind, err := h.remote.Kind(ctx, remotePath)
	if err != nil {
		return err
	}
	switch kind {
	case "file":
		if _, exists := seenEntries[relative]; !exists {
			seenEntries[relative] = struct{}{}
			*entries = append(*entries, archiveEntry{relative: relative, remote: remotePath})
		}
		return nil
	case "dir":
		if _, visited := visitedDirectories[remotePath]; visited {
			return nil
		}
		visitedDirectories[remotePath] = struct{}{}
		if relative != "" {
			if _, exists := seenEntries[relative]; !exists {
				seenEntries[relative] = struct{}{}
				*entries = append(*entries, archiveEntry{relative: relative, remote: remotePath, directory: true})
			}
		}
		children, err := h.remote.List(ctx, remotePath)
		if err != nil {
			return err
		}
		for _, child := range children {
			childRelative, err := joinTransferRelativePath(relative, child.Name)
			if err != nil {
				return err
			}
			if len(*entries) >= maxArchiveEntries {
				return fmt.Errorf("archive contains too many entries")
			}
			if err := h.collectArchiveNode(ctx, childRelative, depth+1, entries, seenEntries, visitedDirectories); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("cannot download unsupported path %q", relative)
	}
}

func (h *handler) serveUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.writeEnabled {
		http.Error(w, "uploads are disabled; restart with -write", http.StatusForbidden)
		return
	}
	backend, ok := h.transferBackend()
	if !ok {
		http.Error(w, "transfer backend is unavailable", http.StatusNotImplemented)
		return
	}
	directory, err := validateTransferRelativePath(r.URL.Query().Get("directory"), true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	directoryPath := h.transferPath(directory)
	kind, err := h.remote.Kind(r.Context(), directoryPath)
	if err != nil {
		writeTransferJSON(w, http.StatusBadGateway, nil, err)
		return
	}
	if kind != "dir" {
		writeTransferJSON(w, http.StatusConflict, nil, fmt.Errorf("upload destination is not a directory"))
		return
	}
	if h.maxUploadSize > 0 && r.ContentLength > h.maxUploadSize {
		writeTransferJSON(w, http.StatusRequestEntityTooLarge, nil, fmt.Errorf("upload exceeds %d bytes", h.maxUploadSize))
		return
	}
	if h.maxUploadSize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadSize)
	}
	if err := r.ParseMultipartForm(maxUploadMemorySize); err != nil {
		status := http.StatusBadRequest
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) || strings.Contains(err.Error(), "request body too large") {
			status = http.StatusRequestEntityTooLarge
		}
		writeTransferJSON(w, status, nil, err)
		return
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File["files"]) == 0 {
		writeTransferJSON(w, http.StatusBadRequest, nil, fmt.Errorf("no files were uploaded"))
		return
	}
	paths := r.MultipartForm.Value["paths"]
	uploaded := make([]string, 0, len(r.MultipartForm.File["files"]))
	for index, header := range r.MultipartForm.File["files"] {
		relative := header.Filename
		if index < len(paths) && paths[index] != "" {
			relative = paths[index]
		}
		fileRelative, err := validateTransferRelativePath(relative, false)
		if err != nil {
			writeTransferJSON(w, http.StatusBadRequest, uploaded, err)
			return
		}
		targetRelative, err := joinTransferRelativePath(directory, fileRelative)
		if err != nil {
			writeTransferJSON(w, http.StatusBadRequest, uploaded, err)
			return
		}
		if h.target.Local {
			if err := validateLocalUploadPath(h.target.Root, targetRelative); err != nil {
				writeTransferJSON(w, http.StatusBadRequest, uploaded, err)
				return
			}
		}
		file, err := header.Open()
		if err != nil {
			writeTransferJSON(w, http.StatusBadRequest, uploaded, err)
			return
		}
		targetPath := h.transferPath(targetRelative)
		if err := backend.MkdirAll(r.Context(), h.transferPath(path.Dir(targetRelative))); err == nil {
			err = backend.WriteFile(r.Context(), targetPath, file)
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			writeTransferJSON(w, http.StatusBadGateway, uploaded, err)
			return
		}
		uploaded = append(uploaded, targetRelative)
		if invalidator, ok := h.remote.(interface{ Invalidate(string) }); ok {
			invalidator.Invalidate(directoryPath)
			invalidator.Invalidate(h.transferPath(path.Dir(targetRelative)))
		}
	}
	writeTransferJSON(w, http.StatusOK, uploaded, nil)
}

func (h *handler) uploadStore() *uploadSessionStore {
	h.uploadsMu.Lock()
	defer h.uploadsMu.Unlock()
	if h.uploads == nil {
		h.uploads = newUploadSessionStore()
	}
	return h.uploads
}

func (h *handler) serveUploadChunk(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		h.reportUploadSession(w, r)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.writeEnabled {
		http.Error(w, "uploads are disabled; restart with -write", http.StatusForbidden)
		return
	}
	backend, ok := h.transferBackend()
	if !ok {
		http.Error(w, "transfer backend is unavailable", http.StatusNotImplemented)
		return
	}
	query := r.URL.Query()
	directory, err := validateTransferRelativePath(query.Get("directory"), true)
	if err != nil {
		writeUploadChunkJSON(w, http.StatusBadRequest, "", 0, false, nil, err)
		return
	}
	relative, err := validateTransferRelativePath(query.Get("path"), false)
	if err != nil {
		writeUploadChunkJSON(w, http.StatusBadRequest, "", 0, false, nil, err)
		return
	}
	total, err := parseUploadInt64(query.Get("total"), "total")
	if err != nil || total < 0 {
		if err == nil {
			err = fmt.Errorf("total must be zero or greater")
		}
		writeUploadChunkJSON(w, http.StatusBadRequest, "", 0, false, nil, err)
		return
	}
	offset, err := parseUploadInt64(query.Get("offset"), "offset")
	if err != nil || offset < 0 {
		if err == nil {
			err = fmt.Errorf("offset must be zero or greater")
		}
		writeUploadChunkJSON(w, http.StatusBadRequest, "", 0, false, nil, err)
		return
	}
	if offset > total {
		writeUploadChunkJSON(w, http.StatusBadRequest, "", offset, false, nil, fmt.Errorf("offset exceeds total"))
		return
	}
	if h.maxUploadSize > 0 && total > h.maxUploadSize {
		writeUploadChunkJSON(w, http.StatusRequestEntityTooLarge, "", offset, false, nil, fmt.Errorf("upload exceeds %d bytes", h.maxUploadSize))
		return
	}
	if r.ContentLength > uploadChunkSize {
		writeUploadChunkJSON(w, http.StatusRequestEntityTooLarge, "", offset, false, nil, fmt.Errorf("upload chunk exceeds %d bytes", uploadChunkSize))
		return
	}
	directoryPath := h.transferPath(directory)
	if kind, kindErr := h.remote.Kind(r.Context(), directoryPath); kindErr != nil {
		writeUploadChunkJSON(w, http.StatusBadGateway, "", offset, false, nil, kindErr)
		return
	} else if kind != "dir" {
		writeUploadChunkJSON(w, http.StatusConflict, "", offset, false, nil, fmt.Errorf("upload destination is not a directory"))
		return
	}
	targetRelative, err := joinTransferRelativePath(directory, relative)
	if err != nil {
		writeUploadChunkJSON(w, http.StatusBadRequest, "", offset, false, nil, err)
		return
	}
	if h.target.Local {
		if err := validateLocalUploadPath(h.target.Root, targetRelative); err != nil {
			writeUploadChunkJSON(w, http.StatusBadRequest, "", offset, false, nil, err)
			return
		}
	}
	store := h.uploadStore()
	id := query.Get("upload_id")
	if id == "" && offset != 0 {
		writeUploadChunkJSON(w, http.StatusConflict, "", offset, false, nil, fmt.Errorf("a new upload must start at offset zero"))
		return
	}
	if id == "" {
		session, createErr := store.create(directory, relative, total)
		if createErr != nil {
			writeUploadChunkJSON(w, http.StatusInternalServerError, "", offset, false, nil, createErr)
			return
		}
		id = session.id
	}

	store.mu.Lock()
	session, exists := store.sessions[id]
	if !exists {
		store.mu.Unlock()
		writeUploadChunkJSON(w, http.StatusNotFound, id, offset, false, nil, fmt.Errorf("upload session not found"))
		return
	}
	if session.directory != directory || session.relative != relative || session.total != total {
		store.mu.Unlock()
		writeUploadChunkJSON(w, http.StatusConflict, id, session.offset, false, nil, fmt.Errorf("upload session metadata does not match"))
		return
	}
	if session.committing {
		store.mu.Unlock()
		writeUploadChunkJSON(w, http.StatusConflict, id, session.offset, false, nil, fmt.Errorf("upload session is being committed"))
		return
	}
	if offset != session.offset {
		expected := session.offset
		store.mu.Unlock()
		writeUploadChunkJSON(w, http.StatusConflict, id, expected, false, nil, fmt.Errorf("unexpected upload offset; expected %d", expected))
		return
	}
	remaining := session.total - session.offset
	if r.ContentLength > remaining {
		store.mu.Unlock()
		writeUploadChunkJSON(w, http.StatusRequestEntityTooLarge, id, session.offset, false, nil, fmt.Errorf("chunk exceeds remaining upload size"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, uploadChunkSize)
	written, writeErr := copyWithContext(r.Context(), session.file, r.Body)
	session.offset += written
	session.lastActivity = time.Now()
	if writeErr != nil {
		current := session.offset
		store.mu.Unlock()
		status := http.StatusBadRequest
		var maxBytesError *http.MaxBytesError
		if errors.As(writeErr, &maxBytesError) {
			status = http.StatusRequestEntityTooLarge
		}
		writeUploadChunkJSON(w, status, id, current, false, nil, writeErr)
		return
	}
	if written == remaining {
		var extra [1]byte
		extraRead, extraErr := r.Body.Read(extra[:])
		if extraRead > 0 {
			_ = session.file.Truncate(session.offset - written)
			_, _ = session.file.Seek(session.offset-written, io.SeekStart)
			expected := session.offset - written
			session.offset = expected
			store.mu.Unlock()
			writeUploadChunkJSON(w, http.StatusRequestEntityTooLarge, id, expected, false, nil, fmt.Errorf("chunk exceeds remaining upload size"))
			return
		}
		if extraErr != nil && extraErr != io.EOF {
			store.mu.Unlock()
			writeUploadChunkJSON(w, http.StatusBadRequest, id, session.offset, false, nil, extraErr)
			return
		}
	}
	if session.offset < session.total {
		current := session.offset
		store.mu.Unlock()
		writeUploadChunkJSON(w, http.StatusOK, id, current, false, nil, nil)
		return
	}
	if err := session.file.Sync(); err != nil {
		store.mu.Unlock()
		writeUploadChunkJSON(w, http.StatusInternalServerError, id, session.offset, false, nil, err)
		return
	}
	if _, err := session.file.Seek(0, io.SeekStart); err != nil {
		store.mu.Unlock()
		writeUploadChunkJSON(w, http.StatusInternalServerError, id, session.offset, false, nil, err)
		return
	}
	session.committing = true
	store.mu.Unlock()

	targetPath := h.transferPath(targetRelative)
	commitErr := backend.MkdirAll(r.Context(), h.transferPath(path.Dir(targetRelative)))
	if commitErr == nil {
		commitErr = backend.WriteFile(r.Context(), targetPath, session.file)
	}
	if commitErr != nil {
		store.mu.Lock()
		session.committing = false
		store.mu.Unlock()
		writeUploadChunkJSON(w, http.StatusBadGateway, id, session.offset, false, nil, commitErr)
		return
	}
	store.mu.Lock()
	store.removeLocked(id)
	store.mu.Unlock()
	if invalidator, ok := h.remote.(interface{ Invalidate(string) }); ok {
		invalidator.Invalidate(directoryPath)
		invalidator.Invalidate(h.transferPath(path.Dir(targetRelative)))
	}
	writeUploadChunkJSON(w, http.StatusOK, id, total, true, []string{targetRelative}, nil)
}

func (h *handler) reportUploadSession(w http.ResponseWriter, r *http.Request) {
	if !h.writeEnabled {
		http.Error(w, "uploads are disabled; restart with -write", http.StatusForbidden)
		return
	}
	id := r.URL.Query().Get("upload_id")
	if id == "" {
		http.Error(w, "upload_id is required", http.StatusBadRequest)
		return
	}
	store := h.uploadStore()
	store.mu.Lock()
	session, ok := store.sessions[id]
	if ok {
		response := uploadChunkResponse{UploadID: id, Offset: session.offset, Complete: session.offset == session.total}
		store.mu.Unlock()
		writeUploadChunkResponse(w, r.Method, http.StatusOK, response)
		return
	}
	store.mu.Unlock()
	http.NotFound(w, r)
}

func parseUploadInt64(raw, name string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return value, nil
}

type uploadChunkResponse struct {
	UploadID string   `json:"upload_id"`
	Offset   int64    `json:"offset"`
	Complete bool     `json:"complete"`
	Uploaded []string `json:"uploaded,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func writeUploadChunkJSON(w http.ResponseWriter, status int, id string, offset int64, complete bool, uploaded []string, err error) {
	response := uploadChunkResponse{UploadID: id, Offset: offset, Complete: complete, Uploaded: uploaded}
	if err != nil {
		response.Error = err.Error()
	}
	writeUploadChunkResponse(w, http.MethodGet, status, response)
}

func writeUploadChunkResponse(w http.ResponseWriter, method string, status int, response uploadChunkResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(response)
	}
}

func validateLocalUploadPath(root, relative string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing upload through a symbolic-link target root")
	}
	current := root
	for _, segment := range strings.Split(relative, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing upload through symbolic link %q", segment)
		}
		if !info.IsDir() && current != filepath.Join(root, filepath.FromSlash(relative)) {
			return fmt.Errorf("upload path component %q is not a directory", segment)
		}
	}
	return nil
}

func writeTransferJSON(w http.ResponseWriter, status int, uploaded []string, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	response := struct {
		Uploaded []string `json:"uploaded,omitempty"`
		Error    string   `json:"error,omitempty"`
	}{Uploaded: uploaded}
	if err != nil {
		response.Error = err.Error()
	}
	_ = json.NewEncoder(w).Encode(response)
}
