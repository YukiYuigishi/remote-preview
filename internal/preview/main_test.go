package preview

import (
	"bytes"
	"context"
	"net"
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

func TestParseTargetRejectsRelativePath(t *testing.T) {
	if _, err := parseTarget("remote-host:docs"); err == nil {
		t.Fatal("expected error")
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
