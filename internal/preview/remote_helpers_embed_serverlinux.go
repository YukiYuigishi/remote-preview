//go:build server_linux && !server_darwin && !server_windows

package preview

import "embed"

const compiledServerOS = "linux"

//go:embed remote_helpers/linux-*.gz
var embeddedRemoteHelperFiles embed.FS

func readEmbeddedRemoteHelper(name string) ([]byte, error) {
	return embeddedRemoteHelperFiles.ReadFile(name)
}
