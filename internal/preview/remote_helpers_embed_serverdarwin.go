//go:build server_darwin && !server_linux && !server_windows

package preview

import "embed"

const compiledServerOS = "darwin"

//go:embed remote_helpers/darwin-*.gz
var embeddedRemoteHelperFiles embed.FS

func readEmbeddedRemoteHelper(name string) ([]byte, error) {
	return embeddedRemoteHelperFiles.ReadFile(name)
}
