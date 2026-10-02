//go:build server_windows && !server_linux && !server_darwin

package preview

import (
	"fmt"
	"io/fs"
)

const compiledServerOS = "windows"

func readEmbeddedRemoteHelper(name string) ([]byte, error) {
	return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
}
