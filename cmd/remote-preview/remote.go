package main

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

type remoteEntry struct {
	Name string
	Kind string
}

type RemoteFS interface {
	Kind(ctx context.Context, remotePath string) (string, error)
	List(ctx context.Context, remotePath string) ([]remoteEntry, error)
	Read(ctx context.Context, remotePath string) ([]byte, error)
	Home(ctx context.Context) (string, error)
}

type sshRemoteFS struct {
	host           string
	commandTimeout time.Duration
	connectTimeout time.Duration
	command        sshCommandFactory
}

type sshCommandFactory func(context.Context, string, ...string) *exec.Cmd

func newSSHRemoteFS(host string) *sshRemoteFS {
	return &sshRemoteFS{
		host:           host,
		commandTimeout: 30 * time.Second,
		connectTimeout: 30 * time.Second,
		command:        exec.CommandContext,
	}
}

func (s *sshRemoteFS) Home(ctx context.Context) (string, error) {
	script := `printf '%s' "$HOME"`
	out, err := s.run(ctx, "sh", "-c", script, "sh")
	if err != nil {
		return "", err
	}
	home := strings.TrimSpace(string(out))
	if home == "" || !strings.HasPrefix(home, "/") {
		return "", fmt.Errorf("remote home is not an absolute path: %q", home)
	}
	return path.Clean(home), nil
}

func (s *sshRemoteFS) Kind(ctx context.Context, remotePath string) (string, error) {
	script := `p=$1; if [ -d "$p" ]; then printf dir; elif [ -f "$p" ]; then printf file; else printf missing; fi`
	out, err := s.run(ctx, "sh", "-c", script, "sh", remotePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *sshRemoteFS) Read(ctx context.Context, remotePath string) ([]byte, error) {
	script := `p=$1; exec cat -- "$p"`
	return s.run(ctx, "sh", "-c", script, "sh", remotePath)
}

func (s *sshRemoteFS) List(ctx context.Context, remotePath string) ([]remoteEntry, error) {
	// Output is intentionally line-oriented for the MVP. Filenames containing
	// newlines are not supported by the directory browser yet.
	script := `
p=$1
for f in "$p"/* "$p"/.[!.]* "$p"/..?*; do
  if [ ! -e "$f" ] && [ ! -L "$f" ]; then
    continue
  fi
  n=${f##*/}
  if [ -d "$f" ]; then t=dir
  elif [ -f "$f" ]; then t=file
  elif [ -L "$f" ]; then t=link
  else t=other
  fi
  printf '%s\t%s\n' "$t" "$n"
done
`
	out, err := s.run(ctx, "sh", "-c", script, "sh", remotePath)
	if err != nil {
		return nil, err
	}

	var entries []remoteEntry
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		kind, name, ok := strings.Cut(line, "\t")
		if !ok || name == "" {
			continue
		}
		entries = append(entries, remoteEntry{Name: name, Kind: kind})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind == "dir" && entries[j].Kind != "dir" {
			return true
		}
		if entries[i].Kind != "dir" && entries[j].Kind == "dir" {
			return false
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func (s *sshRemoteFS) run(ctx context.Context, args ...string) ([]byte, error) {
	commandTimeout := s.commandTimeout
	if commandTimeout <= 0 {
		commandTimeout = 30 * time.Second
	}
	connectTimeout := s.connectTimeout
	if connectTimeout <= 0 {
		connectTimeout = commandTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	sshArgs := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=" + sshTimeoutSeconds(connectTimeout),
		s.host,
	}
	sshArgs = append(sshArgs, shellJoin(args...))

	command := s.command
	if command == nil {
		command = exec.CommandContext
	}
	cmd := command(runCtx, "ssh", sshArgs...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if runCtx.Err() != nil {
			return nil, fmt.Errorf("ssh %s: %w", s.host, runCtx.Err())
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ssh %s: %s", s.host, msg)
	}
	return out, nil
}

func sshTimeoutSeconds(timeout time.Duration) string {
	seconds := int(timeout / time.Second)
	if timeout%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

func shellJoin(args ...string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
