package preview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type targetResolutionErrorRemote struct {
	RemoteFS
	err error
}

func (r targetResolutionErrorRemote) Kind(context.Context, string) (string, error) {
	return "", r.err
}

func TestResolvePreviewTargetDirectoryAndLocalFile(t *testing.T) {
	directory := t.TempDir()
	resolvedDir, initialPath, err := resolvePreviewTarget(context.Background(), remoteTarget{Host: "local", Root: directory, Local: true}, newLocalRemoteFS())
	if err != nil {
		t.Fatal(err)
	}
	if resolvedDir.Root != directory || initialPath != "/" {
		t.Fatalf("directory target=%#v initialPath=%q", resolvedDir, initialPath)
	}

	name := "index #日本語.html"
	file := filepath.Join(directory, name)
	if err := os.WriteFile(file, []byte("<h1>fixture</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	localTarget, err := resolveLocalTarget(remoteTarget{Root: file, Local: true})
	if err != nil {
		t.Fatal(err)
	}
	resolvedFile, initialPath, err := resolvePreviewTarget(context.Background(), localTarget, newLocalRemoteFS())
	if err != nil {
		t.Fatal(err)
	}
	if resolvedFile.Root != directory || initialPath != "/"+url.PathEscape(name) {
		t.Fatalf("file target=%#v initialPath=%q, want parent=%q escaped=%q", resolvedFile, initialPath, directory, "/"+url.PathEscape(name))
	}
}

func TestResolvePreviewTargetRemoteHomeRelativeFile(t *testing.T) {
	target, err := parseTarget("remote-host:~/docs/index #日本語.html")
	if err != nil {
		t.Fatal(err)
	}
	target.setResolvedHome("/home/test")
	backend := newFakeRemoteFS()
	backend.kinds[target.Root] = "file"

	resolved, initialPath, err := resolvePreviewTarget(context.Background(), target, backend)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Host != "remote-host" || resolved.Root != "/home/test/docs" || initialPath != "/index%20%23%E6%97%A5%E6%9C%AC%E8%AA%9E.html" {
		t.Fatalf("resolved target=%#v initialPath=%q", resolved, initialPath)
	}
}

func TestResolvePreviewTargetReportsMissingAndKindErrors(t *testing.T) {
	missing := newFakeRemoteFS()
	if _, _, err := resolvePreviewTarget(context.Background(), remoteTarget{Host: "remote-host", Root: "/missing"}, missing); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing target error=%v", err)
	}

	wantErr := errors.New("connection unavailable")
	backend := targetResolutionErrorRemote{err: wantErr}
	if _, _, err := resolvePreviewTarget(context.Background(), remoteTarget{Host: "remote-host", Root: "/docs"}, backend); !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "resolve target remote-host:/docs") {
		t.Fatalf("Kind error=%v", err)
	}
}

func TestWriteStartupInfoUsesPlainURLOutput(t *testing.T) {
	var output bytes.Buffer
	writeStartupInfo(&output, remoteTarget{Host: "remote-host", Root: "/remote/path"}, "http://127.0.0.1:7391/")

	got := output.String()
	if !strings.Contains(got, "browsing remote-host:/remote/path\n") {
		t.Fatalf("startup target output=%q", got)
	}
	if !strings.Contains(got, "open: http://127.0.0.1:7391/\n") {
		t.Fatalf("startup URL output=%q", got)
	}
	if strings.Contains(got, "level=") || strings.Contains(got, "msg=") {
		t.Fatalf("startup output contains structured log fields: %q", got)
	}
}

func TestCLIOptionsOpenByDefault(t *testing.T) {
	options := newCLIOptions("ykview")
	if err := options.flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if !*options.openPage {
		t.Fatal("expected browser opening to be enabled by default")
	}

	options = newCLIOptions("ykview")
	if err := options.flags.Parse([]string{"-open=false"}); err != nil {
		t.Fatal(err)
	}
	if *options.openPage {
		t.Fatal("expected -open=false to disable browser opening")
	}
}

func TestCLIOptionsWriteIsOptIn(t *testing.T) {
	options := newCLIOptions("ykview")
	if err := options.flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if *options.write {
		t.Fatal("expected uploads to be disabled by default")
	}

	options = newCLIOptions("ykview")
	if err := options.flags.Parse([]string{"-write"}); err != nil {
		t.Fatal(err)
	}
	if !*options.write {
		t.Fatal("expected -write to enable uploads")
	}
	if *options.maxUploadSize != 0 {
		t.Fatalf("default max upload size=%d, want unlimited", *options.maxUploadSize)
	}

	options = newCLIOptions("ykview")
	if err := options.flags.Parse([]string{"-max-upload-size", "1073741824"}); err != nil {
		t.Fatal(err)
	}
	if *options.maxUploadSize != 1073741824 {
		t.Fatalf("max upload size=%d", *options.maxUploadSize)
	}
}

func TestParseTarget(t *testing.T) {
	got, err := parseTarget("remote-host:/remote/path")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "remote-host" || got.Root != "/remote/path" {
		t.Fatalf("unexpected target: %#v", got)
	}
}

func TestParseTargetAcceptsRemoteHomeRelativePath(t *testing.T) {
	for _, input := range []string{"remote-host:~", "remote-host:~/docs", "remote-host:docs", "remote-host:../docs"} {
		got, err := parseTarget(input)
		if err != nil {
			t.Fatalf("parseTarget(%q): %v", input, err)
		}
		if got.Host != "remote-host" || !got.Home {
			t.Fatalf("parseTarget(%q)=%#v, want home-relative target", input, got)
		}
	}
}

func TestSetResolvedHomeHomeRelativePath(t *testing.T) {
	for input, want := range map[string]string{
		"remote-host:~":            "/remote/home",
		"remote-host:~/docs":       "/remote/home/docs",
		"remote-host:docs/project": "/remote/home/docs/project",
		"remote-host:../docs":      "/remote/docs",
	} {
		target, err := parseTarget(input)
		if err != nil {
			t.Fatal(err)
		}
		target.setResolvedHome("/remote/home")
		if target.Root != want || target.Home {
			t.Fatalf("setResolvedHome(%q)=%#v, want root %q", input, target, want)
		}
	}
}

func TestParseTargetRecognizesLocalPathForms(t *testing.T) {
	for _, input := range []string{".", "./subdir", "..", "../parent", "/tmp/ykview", "~", "~/workspace"} {
		got, err := parseTarget(input)
		if err != nil {
			t.Fatalf("parseTarget(%q): %v", input, err)
		}
		if !got.Local || got.Root != input {
			t.Fatalf("parseTarget(%q)=%#v, want local target", input, got)
		}
	}
}

func TestResolveLocalTargetMakesAbsolutePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	got, err := resolveLocalTarget(remoteTarget{Root: root, Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Local || got.Host != "local" || got.Root != root {
		t.Fatalf("resolved local target=%#v", got)
	}

	remote, err := parseTarget("remote-host")
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := resolveLocalTarget(remote); err != nil {
		t.Fatal(err)
	} else if resolved != remote {
		t.Fatalf("non-local target changed: before=%#v after=%#v", remote, resolved)
	}
}

func TestWindowsServerTargetPaths(t *testing.T) {
	for input, want := range map[string]string{
		"host:C:/data":               "C:/data",
		`host:C:\data`:               "C:/data",
		"host:C:/data/..":            "C:/",
		`host:\\server\share\folder`: "//server/share/folder",
		"host://server/share/folder": "//server/share/folder",
	} {
		target, err := parseTarget(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := target.prepareWindows(); err != nil || target.Root != want || target.Home || !target.Windows {
			t.Fatalf("prepareWindows(%q)=%#v, %v", input, target, err)
		}
	}
	target, err := parseTarget("host:~/notes")
	if err != nil {
		t.Fatal(err)
	}
	if err := target.prepareWindows(); err != nil {
		t.Fatal(err)
	}
	target.setResolvedHome("C:/Users/person")
	if target.Root != "C:/Users/person/notes" {
		t.Fatalf("home-relative Windows path=%q", target.Root)
	}
	for _, input := range []string{"host:/data", "host:C:relative"} {
		target, err := parseTarget(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := target.prepareWindows(); err == nil {
			t.Fatalf("prepareWindows(%q) unexpectedly succeeded", input)
		}
	}
	if got := dirRemotePath("C:/note.txt"); got != "C:/" {
		t.Fatalf("Windows drive parent=%q", got)
	}
	if got := joinRemotePath("C:/", "note.txt"); got != "C:/note.txt" {
		t.Fatalf("Windows drive child=%q", got)
	}
	if got := dirRemotePath("//server/share/note.txt"); got != "//server/share" {
		t.Fatalf("Windows UNC parent=%q", got)
	}
	if got := joinRemotePath("//server/share", "note.txt"); got != "//server/share/note.txt" {
		t.Fatalf("Windows UNC child=%q", got)
	}
}

func TestWindowsServerRelativePathValidation(t *testing.T) {
	for _, input := range []string{`..\secret`, `C:/secret`, "CON", "aux.txt", "note. ", "folder /note", "a\x00b"} {
		if isWindowsSafeRelativePath(input) {
			t.Fatalf("unsafe Windows path %q accepted", input)
		}
	}
	for _, input := range []string{"file.txt", "nested/note #日本語.txt"} {
		if !isWindowsSafeRelativePath(input) {
			t.Fatalf("safe Windows path %q rejected", input)
		}
	}
}

func TestParseTargetHostShorthand(t *testing.T) {
	got, err := parseTarget("remote-host")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "remote-host" || !got.Home || got.Root != "" {
		t.Fatalf("unexpected home target: %#v", got)
	}
}

func TestSetResolvedHome(t *testing.T) {
	target, err := parseTarget("remote-host")
	if err != nil {
		t.Fatal(err)
	}
	target.setResolvedHome("/remote/home")
	if target.Root != "/remote/home" || target.Home {
		t.Fatalf("unexpected resolved target: %#v", target)
	}
}

func TestParseDelimitedPreviewHandlesQuotedFieldsAndUnevenRecords(t *testing.T) {
	got, err := parseDelimitedPreview([]byte("name,note\nAlice,\"contains, separator\nand newline\"\nBob\n"), ',')
	if err != nil {
		t.Fatal(err)
	}
	if got.Empty || len(got.Headers) != 2 || len(got.Rows) != 2 {
		t.Fatalf("preview shape=%#v", got)
	}
	if got.Rows[0][1] != "contains, separator\nand newline" {
		t.Fatalf("quoted multiline field=%q", got.Rows[0][1])
	}
	if got.Rows[1][0] != "Bob" || got.Rows[1][1] != "" {
		t.Fatalf("uneven record=%#v", got.Rows[1])
	}
}

func TestParseDelimitedPreviewEmptyAndHeaderOnly(t *testing.T) {
	got, err := parseDelimitedPreview(nil, ',')
	if err != nil || !got.Empty || len(got.Headers) != 0 {
		t.Fatalf("empty preview=%#v, err=%v", got, err)
	}

	got, err = parseDelimitedPreview([]byte("name,value\n"), ',')
	if err != nil || got.Empty || len(got.Headers) != 2 || len(got.Rows) != 0 {
		t.Fatalf("header-only preview=%#v, err=%v", got, err)
	}
}

func TestParseDelimitedPreviewAppliesDisplayLimits(t *testing.T) {
	var source strings.Builder
	source.WriteString("value\n")
	for i := 0; i <= maxDelimitedPreviewRows; i++ {
		source.WriteString("row\n")
	}
	got, err := parseDelimitedPreview([]byte(source.String()), ',')
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != maxDelimitedPreviewRows || !got.RowsOmitted {
		t.Fatalf("row cap: rows=%d omitted=%v", len(got.Rows), got.RowsOmitted)
	}

	wide := strings.Repeat("column,", maxDelimitedPreviewColumns) + "extra\n"
	got, err = parseDelimitedPreview([]byte(wide), ',')
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Headers) != maxDelimitedPreviewColumns || !got.ColumnsOmitted {
		t.Fatalf("column cap: columns=%d omitted=%v", len(got.Headers), got.ColumnsOmitted)
	}

	longField := strings.Repeat("x", maxDelimitedPreviewFieldBytes+1)
	got, err = parseDelimitedPreview([]byte("value\n"+longField+"\n"), ',')
	if err != nil {
		t.Fatal(err)
	}
	if !got.FieldsTruncated || len(got.Rows[0][0]) > maxDelimitedPreviewFieldBytes+3 {
		t.Fatalf("field cap: truncated=%v bytes=%d", got.FieldsTruncated, len(got.Rows[0][0]))
	}
}

func TestParseDelimitedPreviewRejectsMalformedCSV(t *testing.T) {
	if _, err := parseDelimitedPreview([]byte("name,note\n\"unfinished"), ','); err == nil {
		t.Fatal("expected malformed quoted field to fail parsing")
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("a'b")
	want := `'a'"'"'b'`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCleanRelativeURLPathCannotEscapeRoot(t *testing.T) {
	for _, in := range []string{"/../etc/passwd", "/docs/../../secret", "../../../x"} {
		got := cleanRelativeURLPath(in)
		if got == "../etc/passwd" || got == "../secret" || got == "../x" {
			t.Fatalf("path escaped root: %q -> %q", in, got)
		}
		if len(got) >= 2 && got[:2] == ".." {
			t.Fatalf("path escaped root: %q -> %q", in, got)
		}
	}
}

func TestFileKinds(t *testing.T) {
	if !isMarkdown("README.md") || !isHTML("index.HTML") || !isImage("x.webp") || !isPlainText("main.go") {
		t.Fatal("file kind detection failed")
	}
}

func TestParentURL(t *testing.T) {
	cases := map[string]string{
		"/":          "",
		"/docs/":     "/",
		"/docs/api/": "/docs/",
		"/docs/file": "/docs/",
	}
	for in, want := range cases {
		if got := parentURL(in); got != want {
			t.Fatalf("parentURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPathURLJoinEscapesSegment(t *testing.T) {
	got := pathURLJoin("/", "? # 日本語 %")
	want := "/%3F%20%23%20%E6%97%A5%E6%9C%AC%E8%AA%9E%20%25"
	if got != want {
		t.Fatalf("pathURLJoin()=%q want %q", got, want)
	}
}

func TestPreviewURLForUsesLoopbackForUnspecifiedAddress(t *testing.T) {
	got := previewURLFor(&net.TCPAddr{IP: net.IPv4zero, Port: 7391})
	want := "http://127.0.0.1:7391/"
	if got != want {
		t.Fatalf("previewURLFor()=%q want %q", got, want)
	}
}

func TestListenTCPWithPortFallbackUsesNextPortWhenRequestedPortIsOccupied(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	_, portText, err := net.SplitHostPort(occupied.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	if port == 65535 {
		t.Skip("cannot test a port after 65535")
	}

	listener, err := listenTCPWithPortFallback(fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	_, actualPortText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	actualPort, err := strconv.Atoi(actualPortText)
	if err != nil {
		t.Fatal(err)
	}
	if actualPort <= port {
		t.Fatalf("fallback port=%d, want a port after occupied port %d", actualPort, port)
	}
}

func TestListenTCPWithPortFallbackKeepsFreePort(t *testing.T) {
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	reservedPort := reserved.Addr().(*net.TCPAddr).Port
	reserved.Close()

	listener, err := listenTCPWithPortFallback(fmt.Sprintf("127.0.0.1:%d", reservedPort))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if got := listener.Addr().(*net.TCPAddr).Port; got != reservedPort {
		t.Fatalf("port=%d, want requested free port %d", got, reservedPort)
	}
}

func TestListenTCPWithPortFallbackKeepsEphemeralPortSemantics(t *testing.T) {
	listener, err := listenTCPWithPortFallback("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if got := listener.Addr().(*net.TCPAddr).Port; got == 0 {
		t.Fatal("ephemeral listener did not receive a port")
	}
}
