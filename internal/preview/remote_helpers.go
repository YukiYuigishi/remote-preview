package preview

import (
	"bytes"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"strings"
)

// The helper artifacts are built with CGO disabled and compressed to keep the
// main binary smaller. They are generated from cmd/ykview-helper for
// the supported POSIX remote targets.
//
//go:generate ../../scripts/generate-remote-helpers.sh
//go:embed remote_helpers/*.gz
var embeddedRemoteHelperFiles embed.FS

func embeddedRemoteHelper(platform string) ([]byte, error) {
	name := "remote_helpers/" + strings.ReplaceAll(platform, "/", "-") + ".gz"
	compressed, err := embeddedRemoteHelperFiles.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("remote helper is not embedded for %s: %w", platform, err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("open embedded remote helper for %s: %w", platform, err)
	}
	defer reader.Close()
	binary, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read embedded remote helper for %s: %w", platform, err)
	}
	return binary, nil
}
