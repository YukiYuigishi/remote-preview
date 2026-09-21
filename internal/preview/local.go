package preview

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type localRemoteFS struct{}

func newLocalRemoteFS() *localRemoteFS {
	return &localRemoteFS{}
}

func (l *localRemoteFS) Home(ctx context.Context) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	return os.UserHomeDir()
}

func (l *localRemoteFS) Kind(ctx context.Context, localPath string) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}

	info, err := os.Stat(localPath)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "dir", nil
	}
	if info.Mode().IsRegular() {
		return "file", nil
	}
	return "missing", nil
}

func (l *localRemoteFS) List(ctx context.Context, localPath string) ([]remoteEntry, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	directory, err := os.ReadDir(localPath)
	if err != nil {
		return nil, err
	}
	entries := make([]remoteEntry, 0, len(directory))
	for _, entry := range directory {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		} else if entry.Type()&os.ModeSymlink != 0 {
			kind = "link"
		} else if entry.Type() != 0 && !entry.Type().IsRegular() {
			kind = "other"
		}
		entries = append(entries, remoteEntry{Name: entry.Name(), Kind: kind})
	}
	sortRemoteEntries(entries)
	return entries, nil
}

func (l *localRemoteFS) Read(ctx context.Context, localPath string) ([]byte, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return os.ReadFile(localPath)
}

func (l *localRemoteFS) Open(ctx context.Context, localPath string) (io.ReadCloser, transferInfo, error) {
	if err := contextError(ctx); err != nil {
		return nil, transferInfo{}, err
	}
	file, err := os.Open(localPath)
	if err != nil {
		return nil, transferInfo{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, transferInfo{}, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, transferInfo{}, errors.New("only regular files can be transferred")
	}
	return file, transferInfo{Kind: "file", Size: info.Size()}, nil
}

func (l *localRemoteFS) MkdirAll(ctx context.Context, localPath string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	return os.MkdirAll(localPath, 0o755)
}

func (l *localRemoteFS) WriteFile(ctx context.Context, localPath string, src io.Reader) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if info, err := os.Lstat(localPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to overwrite a symbolic link")
		}
		if !info.Mode().IsRegular() {
			return errors.New("upload destination is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	parent := filepath.Dir(localPath)
	temporary, err := os.CreateTemp(parent, ".ykview-upload-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	defer cleanup()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := copyWithContext(ctx, temporary, src); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, localPath); err != nil {
		return err
	}
	return nil
}

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
