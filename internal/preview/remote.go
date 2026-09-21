package preview

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ykview/internal/remotehelper"
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

	transportMu       sync.Mutex
	controlDir        string
	controlPath       string
	transportClosed   bool
	controlDirFactory func() (string, error)

	helperMu        sync.Mutex
	helperDone      chan struct{}
	helperAttempted bool
	helperPath      string
	helperPlatform  string
	helperErr       error
	helperDisabled  bool
}

type sshCommandFactory func(context.Context, string, ...string) *exec.Cmd

func newSSHRemoteFS(host string) *sshRemoteFS {
	return &sshRemoteFS{
		host:           host,
		commandTimeout: 30 * time.Second,
		connectTimeout: 30 * time.Second,
		command:        exec.CommandContext,
		controlDirFactory: func() (string, error) {
			return os.MkdirTemp("", "remote-preview-ssh-")
		},
		helperDone: make(chan struct{}),
	}
}

const controlPersist = "30s"

// enableConnectionSharing opts this backend into a process-local OpenSSH
// ControlMaster. It is deliberately separate from newSSHRemoteFS so tests and
// other callers that only need an independent SSH invocation do not allocate a
// temporary directory.
func (s *sshRemoteFS) enableConnectionSharing() {
	s.transportMu.Lock()
	defer s.transportMu.Unlock()
	if s.transportClosed || s.controlDir != "" {
		return
	}

	mkdir := s.controlDirFactory
	if mkdir == nil {
		mkdir = func() (string, error) {
			return os.MkdirTemp("", "remote-preview-ssh-")
		}
	}
	dir, err := mkdir()
	if err != nil {
		slog.Debug("remote transport connection sharing setup failed", "host", s.host, "error", err)
		return
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.Remove(dir)
		slog.Debug("remote transport connection sharing setup failed", "host", s.host, "error", err)
		return
	}

	s.controlDir = dir
	s.controlPath = filepath.Join(dir, "control")
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
	helperPath, err := s.ensureHelper(ctx)
	if err == nil {
		result, helperErr := s.listBatchWithHelper(ctx, helperPath, remotePath)
		if helperErr == nil {
			return result, nil
		}
		slog.Debug("remote helper failed; falling back to shell batch", "host", s.host, "remote_path", remotePath, "error", helperErr)
		s.invalidateHelper(ctx)
		s.disableHelper()
	} else {
		slog.Debug("remote helper unavailable; falling back to shell batch", "host", s.host, "remote_path", remotePath, "error", err)
	}

	return s.listBatchWithShell(ctx, remotePath)
}

func (s *sshRemoteFS) listBatchWithHelper(ctx context.Context, helperPath, remotePath string) (batchListingResult, error) {
	out, err := s.runNamed(ctx, "list_batch_helper", remotePath, helperPath, "list-batch", remotehelper.ProtocolVersion, remotePath, strconv.Itoa(batchMaxDirectories))
	if err != nil {
		return batchListingResult{}, err
	}
	return parseBatchListings(remotePath, out)
}

func (s *sshRemoteFS) listBatchWithShell(ctx context.Context, remotePath string) (batchListingResult, error) {
	out, err := s.runNamed(ctx, "list_batch_shell", remotePath, "sh", "-c", batchListingScript, "sh", remotePath, strconv.Itoa(batchMaxDirectories))
	if err != nil {
		return batchListingResult{}, err
	}
	return parseBatchListings(remotePath, out)
}

const remoteHelperUploadScript = `set -eu
tmpdir=${TMPDIR:-/tmp}
path="$tmpdir/$1"
temp="$tmpdir/.remote-preview-helper-upload-$2"
if [ -f "$path" ] && [ -x "$path" ]; then
  printf '%s' "$path"
  exit 0
fi
umask 077
trap 'rm -f "$temp"' EXIT HUP INT TERM
cat > "$temp"
chmod 700 "$temp"
mv "$temp" "$path"
trap - EXIT HUP INT TERM
printf '%s' "$path"
`

const remoteHelperProbeScript = `set -eu
case "$(uname -s):$(uname -m)" in
  Linux:x86_64|Linux:amd64) platform=linux/amd64; name=$1;;
  Linux:aarch64|Linux:arm64) platform=linux/arm64; name=$2;;
  Darwin:x86_64|Darwin:amd64) platform=darwin/amd64; name=$3;;
  Darwin:arm64) platform=darwin/arm64; name=$4;;
  *) printf 'unsupported\n'; exit 0;;
esac
path="${TMPDIR:-/tmp}/$name"
if [ -f "$path" ] && [ -x "$path" ]; then
  printf 'ready\n%s\n%s\n' "$platform" "$path"
else
  printf 'missing\n%s\n%s\n' "$platform" "$path"
fi
`

const remoteHelperCacheVersion = "1"

type remoteHelperCacheCandidate struct {
	platform string
	name     string
	binary   []byte
}

type remoteHelperProbeResult struct {
	platform string
	path     string
	name     string
	binary   []byte
	hit      bool
}

func remoteHelperCacheName(platform string, binary []byte) string {
	digest := sha256.Sum256(binary)
	return fmt.Sprintf(".remote-preview-helper-v%s-%s-%x", remoteHelperCacheVersion, strings.ReplaceAll(platform, "/", "-"), digest)
}

func remoteHelperCacheCandidates() ([]remoteHelperCacheCandidate, error) {
	platforms := []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"}
	candidates := make([]remoteHelperCacheCandidate, 0, len(platforms))
	for _, platform := range platforms {
		binary, err := embeddedRemoteHelper(platform)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, remoteHelperCacheCandidate{
			platform: platform,
			name:     remoteHelperCacheName(platform, binary),
			binary:   binary,
		})
	}
	return candidates, nil
}

func parseRemoteHelperProbe(output []byte) (status, platform, helperPath string, err error) {
	text := strings.TrimSuffix(string(output), "\n")
	fields := strings.Split(text, "\n")
	if len(fields) == 1 && fields[0] == "unsupported" {
		return "", "", "", fmt.Errorf("unsupported remote platform")
	}
	if len(fields) != 3 || (fields[0] != "ready" && fields[0] != "missing") || fields[1] == "" || fields[2] == "" {
		return "", "", "", fmt.Errorf("invalid remote helper probe output: %q", output)
	}
	return fields[0], fields[1], fields[2], nil
}

func (s *sshRemoteFS) ensureHelper(ctx context.Context) (string, error) {
	s.helperMu.Lock()
	if s.helperAttempted {
		done := s.helperDone
		s.helperMu.Unlock()
		select {
		case <-done:
			return s.helperState()
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	s.helperAttempted = true
	done := s.helperDone
	s.helperMu.Unlock()

	probe, err := s.probeHelper(ctx)
	var helperPath string
	if err == nil {
		if probe.hit {
			helperPath = probe.path
			slog.Debug("remote helper cache hit", "host", s.host, "platform", probe.platform, "remote_path", helperPath, "cache_name", probe.name)
		} else {
			slog.Debug("remote helper cache miss", "host", s.host, "platform", probe.platform, "cache_name", probe.name)
			helperPath, err = s.uploadHelper(ctx, probe.platform, probe.name, probe.binary)
		}
	}

	s.helperMu.Lock()
	s.helperPlatform = probe.platform
	s.helperPath = helperPath
	s.helperErr = err
	close(done)
	s.helperMu.Unlock()
	return helperPath, err
}

func (s *sshRemoteFS) helperState() (string, error) {
	s.helperMu.Lock()
	defer s.helperMu.Unlock()
	if s.helperDisabled {
		if s.helperErr != nil {
			return "", s.helperErr
		}
		return "", fmt.Errorf("remote helper disabled")
	}
	return s.helperPath, s.helperErr
}

func (s *sshRemoteFS) disableHelper() {
	s.helperMu.Lock()
	s.helperDisabled = true
	s.helperMu.Unlock()
}

func (s *sshRemoteFS) invalidateHelper(ctx context.Context) {
	s.helperMu.Lock()
	helperPath := s.helperPath
	s.helperMu.Unlock()
	if helperPath == "" {
		return
	}
	_, err := s.runNamed(ctx, "helper_invalidate", helperPath, "sh", "-c", `rm -f "$1"`, "sh", helperPath)
	if err != nil {
		slog.Debug("remote helper cache invalidation failed", "host", s.host, "remote_path", helperPath, "error", err)
		return
	}
	slog.Debug("remote helper cache invalidated", "host", s.host, "remote_path", helperPath)
}

func (s *sshRemoteFS) probeHelper(ctx context.Context) (remoteHelperProbeResult, error) {
	candidates, err := remoteHelperCacheCandidates()
	if err != nil {
		return remoteHelperProbeResult{}, err
	}
	args := []string{"sh", "-c", remoteHelperProbeScript, "sh"}
	for _, candidate := range candidates {
		args = append(args, candidate.name)
	}
	out, err := s.runNamed(ctx, "helper_probe", "", args...)
	if err != nil {
		return remoteHelperProbeResult{}, err
	}
	status, platform, helperPath, err := parseRemoteHelperProbe(out)
	if err != nil {
		return remoteHelperProbeResult{}, err
	}
	for _, candidate := range candidates {
		if candidate.platform == platform {
			return remoteHelperProbeResult{
				platform: platform,
				path:     helperPath,
				name:     candidate.name,
				binary:   candidate.binary,
				hit:      status == "ready",
			}, nil
		}
	}
	return remoteHelperProbeResult{}, fmt.Errorf("unsupported remote platform: %s", platform)
}

func remoteHelperPlatform(goos, arch string) (string, error) {
	normalizedOS := strings.ToLower(strings.TrimSpace(goos))
	normalizedArch := strings.ToLower(strings.TrimSpace(arch))
	switch normalizedOS {
	case "linux":
		switch normalizedArch {
		case "x86_64", "amd64":
			return "linux/amd64", nil
		case "aarch64", "arm64":
			return "linux/arm64", nil
		}
	case "darwin":
		switch normalizedArch {
		case "x86_64", "amd64":
			return "darwin/amd64", nil
		case "arm64":
			return "darwin/arm64", nil
		}
	}
	return "", fmt.Errorf("unsupported remote platform: %s/%s", goos, arch)
}

func (s *sshRemoteFS) uploadHelper(ctx context.Context, platform, cacheName string, binary []byte) (string, error) {
	nonceBytes := make([]byte, 16)
	if _, err := cryptorand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("generate helper name: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	out, err := s.runNamedInputWithCompression(ctx, "helper_upload_compressed", "", binary, true, "sh", "-c", remoteHelperUploadScript, "sh", cacheName, nonce)
	if err != nil && ctx.Err() == nil {
		slog.Debug("compressed remote helper upload failed; retrying without compression", "host", s.host, "error", err)
		out, err = s.runNamedInput(ctx, "helper_upload", "", binary, "sh", "-c", remoteHelperUploadScript, "sh", cacheName, nonce)
	}
	if err != nil {
		return "", err
	}
	helperPath := strings.TrimSpace(string(out))
	if helperPath == "" {
		return "", fmt.Errorf("remote helper upload returned an empty path")
	}
	slog.Debug("remote helper uploaded", "host", s.host, "platform", platform, "remote_path", helperPath, "cache_name", cacheName, "bytes", len(binary))
	return helperPath, nil
}

func (s *sshRemoteFS) Close() error {
	s.helperMu.Lock()
	s.helperPath = ""
	s.helperMu.Unlock()

	s.transportMu.Lock()
	controlDir := s.controlDir
	controlPath := s.controlPath
	s.controlDir = ""
	s.controlPath = ""
	s.transportClosed = true
	s.transportMu.Unlock()
	if controlDir == "" {
		return nil
	}

	commandTimeout := s.commandTimeout
	if commandTimeout <= 0 {
		commandTimeout = 30 * time.Second
	}
	connectTimeout := s.connectTimeout
	if connectTimeout <= 0 {
		connectTimeout = commandTimeout
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	command := s.command
	if command == nil {
		command = exec.CommandContext
	}
	cmd := command(closeCtx, "ssh",
		"-T",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+sshTimeoutSeconds(connectTimeout),
		"-S", controlPath,
		"-O", "exit",
		s.host,
	)
	if err := cmd.Run(); err != nil {
		// No socket is a normal case when no command used the backend. The
		// exact temporary directory is still removed below.
		slog.Debug("remote transport master exit failed", "host", s.host, "error", err)
	}
	if err := os.RemoveAll(controlDir); err != nil {
		return fmt.Errorf("remove remote transport directory: %w", err)
	}
	return nil
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
	return s.runNamedInput(ctx, operation, remotePath, nil, args...)
}

func (s *sshRemoteFS) runNamedInput(ctx context.Context, operation, remotePath string, input []byte, args ...string) ([]byte, error) {
	return s.runNamedInputWithCompression(ctx, operation, remotePath, input, false, args...)
}

func (s *sshRemoteFS) runNamedInputWithCompression(ctx context.Context, operation, remotePath string, input []byte, compression bool, args ...string) ([]byte, error) {
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
	slog.Debug("ssh command start", "host", s.host, "operation", operation, "remote_path", remotePath, "timeout", commandTimeout, "input_bytes", len(input), "compression", compression)

	sshArgs := []string{"-T"}
	if compression {
		sshArgs = append(sshArgs, "-C")
	}
	sshArgs = append(sshArgs,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+sshTimeoutSeconds(connectTimeout),
	)
	s.transportMu.Lock()
	controlPath := s.controlPath
	s.transportMu.Unlock()
	if controlPath != "" {
		sshArgs = append(sshArgs,
			"-o", "ControlMaster=auto",
			"-o", "ControlPersist="+controlPersist,
			"-o", "ControlPath="+controlPath,
		)
	}
	sshArgs = append(sshArgs, s.host)
	sshArgs = append(sshArgs, shellJoin(args...))

	command := s.command
	if command == nil {
		command = exec.CommandContext
	}
	cmd := command(runCtx, "ssh", sshArgs...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
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
