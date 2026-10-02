package preview

import (
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestEncodedPowerShellCommand(t *testing.T) {
	script := `[Console]::Out.Write('日本語')`
	command := encodedPowerShellCommand(script)
	if !strings.HasPrefix(command, powerShellCommandPrefix) {
		t.Fatalf("PowerShell command prefix=%q", command)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, powerShellCommandPrefix))
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	if got := string(utf16.Decode(units)); got != script {
		t.Fatalf("decoded PowerShell command=%q", got)
	}
	if got := psQuote(`C:/person's notes`); got != `'C:/person''s notes'` {
		t.Fatalf("PowerShell argument=%q", got)
	}
}

func TestWindowsServerHTTPPathsStayUnderRoot(t *testing.T) {
	backend := newFakeRemoteFS()
	h := &handler{target: remoteTarget{Host: "host", Root: "C:/root", Windows: true}, remote: backend}
	for _, endpoint := range []string{`/..%5csecret`, `/C:/secret`, `/_ykview/transfer/download?path=..%5csecret`} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if response.Code != http.StatusNotFound && response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe request %q returned %d", endpoint, response.Code)
		}
	}
}
