package preview

import (
	"context"
	"errors"
	"os"
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

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
