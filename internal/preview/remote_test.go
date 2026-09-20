package preview

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSSHRemoteFSAddsConnectionTimeout(t *testing.T) {
	var commandName string
	var commandArgs []string
	remote := newSSHRemoteFS("remote-host")
	remote.connectTimeout = 7 * time.Second
	remote.command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		commandName = name
		commandArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "sh", "-c", "printf ok")
	}

	out, err := remote.run(context.Background(), "sh", "-c", "true")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "ok" {
		t.Fatalf("output=%q, want ok", out)
	}
	if commandName != "ssh" {
		t.Fatalf("command=%q, want ssh", commandName)
	}
	if !containsArg(commandArgs, "ConnectTimeout=7") {
		t.Fatalf("ssh args do not contain connection timeout: %v", commandArgs)
	}
}

func TestSSHRemoteFSCancelStopsCommand(t *testing.T) {
	remote := newSSHRemoteFS("remote-host")
	remote.commandTimeout = 30 * time.Second
	remote.command = blockingCommand

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := remote.run(ctx, "sh", "-c", "sleep 30")
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("command did not stop after context cancellation")
	}
}

func TestSSHRemoteFSCommandTimeout(t *testing.T) {
	remote := newSSHRemoteFS("remote-host")
	remote.commandTimeout = 30 * time.Millisecond
	remote.command = blockingCommand

	_, err := remote.run(context.Background(), "sh", "-c", "sleep 30")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want deadline exceeded", err)
	}
}

func blockingCommand(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", "while :; do sleep 1; done")
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestSSHRemoteFSHelperProcess(t *testing.T) {
	if os.Getenv("REMOTE_PREVIEW_HELPER") != "1" {
		return
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestSSHRemoteFSCommandFactoryCanBeReplaced(t *testing.T) {
	remote := newSSHRemoteFS("remote-host")
	remote.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestSSHRemoteFSHelperProcess", "--")
		cmd.Env = append(os.Environ(), "REMOTE_PREVIEW_HELPER=1")
		return cmd
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := remote.run(ctx, "true")
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("error=%v, want context deadline exceeded", err)
	}
}

func TestRemoteHelperPlatform(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		arch   string
		want   string
		wantOK bool
	}{
		{name: "linux amd64", goos: "Linux", arch: "x86_64", want: "linux/amd64", wantOK: true},
		{name: "linux arm64", goos: "Linux", arch: "aarch64", want: "linux/arm64", wantOK: true},
		{name: "darwin amd64", goos: "Darwin", arch: "x86_64", want: "darwin/amd64", wantOK: true},
		{name: "darwin arm64", goos: "Darwin", arch: "arm64", want: "darwin/arm64", wantOK: true},
		{name: "unsupported", goos: "FreeBSD", arch: "amd64", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := remoteHelperPlatform(tt.goos, tt.arch)
			if tt.wantOK {
				if err != nil || got != tt.want {
					t.Fatalf("platform=%q error=%v, want %q", got, err, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("platform=%q, want unsupported error", got)
			}
		})
	}
}

func TestSSHRemoteFSHelperFailureFallsBackToShellBatch(t *testing.T) {
	remote := newSSHRemoteFS("remote-host")
	remote.helperAttempted = true
	remote.helperPath = "/tmp/remote-preview-helper-test"
	remote.helperDone = make(chan struct{})
	close(remote.helperDone)
	remote.command = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		remoteCommand := args[len(args)-1]
		if strings.Contains(remoteCommand, "list-batch") {
			return exec.CommandContext(ctx, "sh", "-c", "printf invalid")
		}
		return exec.CommandContext(ctx, "sh", "-c", "printf 'K\\0dir\\0D\\0\\0X\\0'")
	}

	result, err := remote.ListBatch(context.Background(), "/root")
	if err != nil {
		t.Fatal(err)
	}
	if result.RootKind != "dir" || len(result.Listings) != 1 || result.Listings[0].Path != "/root" {
		t.Fatalf("unexpected fallback result: %#v", result)
	}
	if !remote.helperDisabled {
		t.Fatal("helper should be disabled after execution failure")
	}
}

func TestSSHRemoteFSUsesHelperBatch(t *testing.T) {
	remote := newSSHRemoteFS("remote-host")
	remote.helperAttempted = true
	remote.helperPath = "/tmp/remote-preview-helper-test"
	remote.helperDone = make(chan struct{})
	close(remote.helperDone)
	remote.command = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		remoteCommand := args[len(args)-1]
		if !strings.Contains(remoteCommand, "list-batch") {
			t.Fatalf("unexpected fallback command: %q", remoteCommand)
		}
		return exec.CommandContext(ctx, "sh", "-c", "printf 'K\\0dir\\0D\\0\\0E\\0file\\0note\\0X\\0'")
	}

	result, err := remote.ListBatch(context.Background(), "/root")
	if err != nil {
		t.Fatal(err)
	}
	if result.RootKind != "dir" || len(result.Listings) != 1 || len(result.Listings[0].Entries) != 1 || result.Listings[0].Entries[0].Name != "note" {
		t.Fatalf("unexpected helper result: %#v", result)
	}
}

func TestSSHRemoteFSHelperAssetsAreEmbedded(t *testing.T) {
	for _, platform := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"} {
		binary, err := embeddedRemoteHelper(platform)
		if err != nil {
			t.Fatalf("platform %s: %v", platform, err)
		}
		if len(binary) == 0 || !bytes.HasPrefix(binary, []byte("\x7fELF")) && !bytes.HasPrefix(binary, []byte("\xcf\xfa")) && !bytes.HasPrefix(binary, []byte("\xca\xfe")) {
			t.Fatalf("platform %s: unexpected helper binary header", platform)
		}
	}
}
