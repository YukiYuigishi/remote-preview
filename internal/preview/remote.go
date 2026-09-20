package preview

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
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

type remoteListing struct {
	Path    string
	Entries []remoteEntry
}

type batchListingResult struct {
	RootKind string
	Listings []remoteListing
}

type RemoteFS interface {
	Kind(ctx context.Context, remotePath string) (string, error)
	List(ctx context.Context, remotePath string) ([]remoteEntry, error)
	Read(ctx context.Context, remotePath string) ([]byte, error)
	Home(ctx context.Context) (string, error)
}

type batchRemoteFS interface {
	ListBatch(ctx context.Context, remotePath string) (batchListingResult, error)
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
	out, err := s.runNamed(ctx, "home", "", "sh", "-c", script, "sh")
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
	out, err := s.runNamed(ctx, "kind", remotePath, "sh", "-c", script, "sh", remotePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *sshRemoteFS) Read(ctx context.Context, remotePath string) ([]byte, error) {
	script := `p=$1; exec cat -- "$p"`
	return s.runNamed(ctx, "read", remotePath, "sh", "-c", script, "sh", remotePath)
}

func (s *sshRemoteFS) List(ctx context.Context, remotePath string) ([]remoteEntry, error) {
	// This is the single-directory fallback for batch listing failures. Its
	// line-oriented output keeps the fallback simple; the normal SSH path uses
	// ListBatch's NUL-delimited protocol.
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
	out, err := s.runNamed(ctx, "list", remotePath, "sh", "-c", script, "sh", remotePath)
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

	sortRemoteEntries(entries)
	return entries, nil
}

const batchMaxDirectories = 16

const batchListingScript = `
root=$1
max_children=$2

list_dir() {
  rel=$1
  dir=$root
  if [ -n "$rel" ]; then
    dir="$root/$rel"
  fi
  printf 'D\0%s\0' "$rel"
  for f in "$dir"/* "$dir"/.[!.]* "$dir"/..?*; do
    if [ ! -e "$f" ] && [ ! -L "$f" ]; then
      continue
    fi
    n=${f##*/}
    if [ -d "$f" ]; then t=dir
    elif [ -f "$f" ]; then t=file
    elif [ -L "$f" ]; then t=link
    else t=other
    fi
    printf 'E\0%s\0%s\0' "$t" "$n"
  done
  printf 'X\0'
}

if [ -d "$root" ]; then root_kind=dir
elif [ -f "$root" ]; then root_kind=file
elif [ -L "$root" ]; then root_kind=link
else root_kind=missing
fi
printf 'K\0%s\0' "$root_kind"
if [ "$root_kind" != dir ]; then
  exit 0
fi

list_dir ''
count=0
for f in "$root"/* "$root"/.[!.]* "$root"/..?*; do
  if [ ! -e "$f" ] && [ ! -L "$f" ]; then
    continue
  fi
  if [ ! -d "$f" ]; then
    continue
  fi
  if [ "$count" -ge "$max_children" ]; then
    break
  fi
  list_dir "${f##*/}"
  count=$((count + 1))
done
`

func (s *sshRemoteFS) ListBatch(ctx context.Context, remotePath string) (batchListingResult, error) {
	out, err := s.runNamed(ctx, "list_batch", remotePath, "sh", "-c", batchListingScript, "sh", remotePath, strconv.Itoa(batchMaxDirectories))
	if err != nil {
		return batchListingResult{}, err
	}
	return parseBatchListings(remotePath, out)
}

func parseBatchListings(root string, output []byte) (batchListingResult, error) {
	fields := bytes.Split(output, []byte{0})
	if len(fields) < 2 || string(fields[0]) != "K" {
		return batchListingResult{}, fmt.Errorf("invalid batch listing kind")
	}
	result := batchListingResult{RootKind: string(fields[1])}
	listings := make([]remoteListing, 0)
	for i := 2; i < len(fields); {
		if len(fields[i]) == 0 {
			i++
			continue
		}
		if string(fields[i]) != "D" || i+1 >= len(fields) {
			return batchListingResult{}, fmt.Errorf("invalid batch listing header")
		}
		relativePath := string(fields[i+1])
		i += 2
		entries := make([]remoteEntry, 0)
		for {
			if i >= len(fields) {
				return batchListingResult{}, fmt.Errorf("unterminated batch listing for %q", relativePath)
			}
			switch string(fields[i]) {
			case "X":
				i++
				sortRemoteEntries(entries)
				listings = append(listings, remoteListing{
					Path:    path.Clean(path.Join(root, relativePath)),
					Entries: entries,
				})
				goto nextListing
			case "E":
				if i+2 >= len(fields) {
					return batchListingResult{}, fmt.Errorf("incomplete batch entry for %q", relativePath)
				}
				entries = append(entries, remoteEntry{
					Kind: string(fields[i+1]),
					Name: string(fields[i+2]),
				})
				i += 3
			default:
				return batchListingResult{}, fmt.Errorf("invalid batch listing record %q", fields[i])
			}
		}
	nextListing:
	}
	if result.RootKind == "dir" && len(listings) == 0 {
		return batchListingResult{}, fmt.Errorf("empty batch listing")
	}
	result.Listings = listings
	return result, nil
}

func sortRemoteEntries(entries []remoteEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind == "dir" && entries[j].Kind != "dir" {
			return true
		}
		if entries[i].Kind != "dir" && entries[j].Kind == "dir" {
			return false
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func (s *sshRemoteFS) run(ctx context.Context, args ...string) ([]byte, error) {
	return s.runNamed(ctx, "command", "", args...)
}

func (s *sshRemoteFS) runNamed(ctx context.Context, operation, remotePath string, args ...string) ([]byte, error) {
	started := time.Now()
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
	slog.Debug("ssh command start", "host", s.host, "operation", operation, "remote_path", remotePath, "timeout", commandTimeout)

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
			wrapped := fmt.Errorf("ssh %s: %w", s.host, runCtx.Err())
			slog.Debug("ssh command done", "host", s.host, "operation", operation, "remote_path", remotePath, "duration", time.Since(started), "bytes", len(out), "error", wrapped)
			return nil, wrapped
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		wrapped := fmt.Errorf("ssh %s: %s", s.host, msg)
		slog.Debug("ssh command done", "host", s.host, "operation", operation, "remote_path", remotePath, "duration", time.Since(started), "bytes", len(out), "error", wrapped)
		return nil, wrapped
	}
	slog.Debug("ssh command done", "host", s.host, "operation", operation, "remote_path", remotePath, "duration", time.Since(started), "bytes", len(out))
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
