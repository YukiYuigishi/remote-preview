package preview

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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
	options := newCLIOptions("remote-preview")
	if err := options.flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if !*options.openPage {
		t.Fatal("expected browser opening to be enabled by default")
	}

	options = newCLIOptions("remote-preview")
	if err := options.flags.Parse([]string{"-open=false"}); err != nil {
		t.Fatal(err)
	}
	if *options.openPage {
		t.Fatal("expected -open=false to disable browser opening")
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

func TestResolveTargetHomeRelativePath(t *testing.T) {
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
		resolved, err := resolveTarget(context.Background(), target, &fakeRemoteFS{home: "/remote/home"})
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Root != want || resolved.Home {
			t.Fatalf("resolveTarget(%q)=%#v, want root %q", input, resolved, want)
		}
	}
}

func TestParseTargetRecognizesLocalPathForms(t *testing.T) {
	for _, input := range []string{".", "./subdir", "..", "../parent", "/tmp/remote-preview", "~", "~/workspace"} {
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

func TestParseTargetHostShorthand(t *testing.T) {
	got, err := parseTarget("remote-host")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "remote-host" || !got.Home || got.Root != "" {
		t.Fatalf("unexpected home target: %#v", got)
	}
}

func TestResolveTargetHome(t *testing.T) {
	target, err := parseTarget("remote-host")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveTarget(context.Background(), target, &fakeRemoteFS{home: "/remote/home"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Root != "/remote/home" || resolved.Home {
		t.Fatalf("unexpected resolved target: %#v", resolved)
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
