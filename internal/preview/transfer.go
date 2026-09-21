package preview

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
)

const (
	maxUploadBodySize   int64 = 512 << 20
	maxUploadMemorySize       = 32 << 20
	maxArchiveEntries         = 10_000
	maxArchiveDepth           = 256
)

type transferInfo struct {
	Kind string
	Size int64
}

// transferFS contains the streaming and write operations needed by the
// transfer endpoints. RemoteFS intentionally remains read-only so existing
// preview fakes and callers do not acquire write capabilities accidentally.
type transferFS interface {
	Open(ctx context.Context, remotePath string) (io.ReadCloser, transferInfo, error)
	MkdirAll(ctx context.Context, remotePath string) error
	WriteFile(ctx context.Context, remotePath string, src io.Reader) error
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var copied int64
	for {
		select {
		case <-ctx.Done():
			return copied, ctx.Err()
		default:
		}

		read, readErr := src.Read(buffer)
		if read > 0 {
			written, writeErr := dst.Write(buffer[:read])
			copied += int64(written)
			if writeErr != nil {
				return copied, writeErr
			}
			if written != read {
				return copied, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return copied, nil
			}
			return copied, readErr
		}
	}
}

// validateTransferRelativePath accepts paths relative to the selected target
// root. It rejects ambiguous separators and traversal instead of relying on
// path.Clean to silently reinterpret user input.
func validateTransferRelativePath(raw string, allowEmpty bool) (string, error) {
	if raw == "" {
		if allowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("path must not be empty")
	}
	if strings.ContainsRune(raw, '\x00') || strings.Contains(raw, "\\") || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("invalid relative path %q", raw)
	}
	parts := strings.Split(raw, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid relative path %q", raw)
		}
	}
	return path.Join(parts...), nil
}

func joinTransferRelativePath(directory, name string) (string, error) {
	cleanDirectory, err := validateTransferRelativePath(directory, true)
	if err != nil {
		return "", err
	}
	cleanName, err := validateTransferRelativePath(name, false)
	if err != nil {
		return "", err
	}
	if cleanDirectory == "" {
		return cleanName, nil
	}
	return validateTransferRelativePath(path.Join(cleanDirectory, cleanName), false)
}

func transferContentDisposition(filename string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": filename})
}
