package main

import (
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
