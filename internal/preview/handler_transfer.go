package preview

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
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
	if r.ContentLength > maxUploadBodySize {
		writeTransferJSON(w, http.StatusRequestEntityTooLarge, nil, fmt.Errorf("upload exceeds %d bytes", maxUploadBodySize))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBodySize)
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
