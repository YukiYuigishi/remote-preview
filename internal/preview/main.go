package preview

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

type cliOptions struct {
	flags         *flag.FlagSet
	addr          *string
	openPage      *bool
	write         *bool
	maxUploadSize *int64
	verbose       *bool
}

func newCLIOptions(program string) *cliOptions {
	flags := flag.NewFlagSet(program, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	addr := flags.String("addr", "127.0.0.1:8080", "listen address")
	openPage := flags.Bool("open", true, "open the file browser in the default browser (use -open=false to disable)")
	write := flags.Bool("write", false, "enable file uploads (disabled by default)")
	maxUploadSize := flags.Int64("max-upload-size", 0, "maximum upload request size in bytes (0 = unlimited)")
	verbose := flags.Bool("v", false, "log each request")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s [options] target\n\n", filepath.Base(program))
		flags.PrintDefaults()
	}
	return &cliOptions{flags: flags, addr: addr, openPage: openPage, write: write, maxUploadSize: maxUploadSize, verbose: verbose}
}

func Run(args []string, program string) error {
	configureLogging()

	options := newCLIOptions(program)
	if err := options.flags.Parse(args); err != nil {
		return err
	}
	if options.flags.NArg() != 1 {
		options.flags.Usage()
		return errors.New("exactly one remote target is required")
	}
	if *options.maxUploadSize < 0 {
		return errors.New("-max-upload-size must be zero or greater")
	}

	target, err := parseTarget(options.flags.Arg(0))
	if err != nil {
		return err
	}

	var backend RemoteFS
	var closer io.Closer
	if target.Local {
		target, err = resolveLocalTarget(target)
		if err != nil {
			return err
		}
		backend = newLocalRemoteFS()
	} else {
		sshBackend := newSSHRemoteFS(target.Host)
		sshBackend.enableConnectionSharing()
		backend = sshBackend
		closer = sshBackend
	}
	defer func() {
		if closer != nil {
			if closeErr := closer.Close(); closeErr != nil {
				slog.Debug("remote transport cleanup failed", "host", target.Host, "error", closeErr)
			}
		}
	}()

	if !target.Local && target.Home {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		home, homeErr := backend.Home(ctx)
		cancel()
		if homeErr != nil {
			return fmt.Errorf("resolve remote home: %w", homeErr)
		}
		target.setResolvedHome(home)
	}

	remote := newCachedRemoteFS(backend, target.Host)
	h := &handler{target: target, remote: remote, transfer: remote, writeEnabled: *options.write, maxUploadSize: *options.maxUploadSize, uploads: newUploadSessionStore(), verbose: *options.verbose}
	defer h.uploads.Close()
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := listenTCPWithPortFallback(*options.addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	srv.Addr = listener.Addr().String()

	previewURL := previewURLFor(listener.Addr())
	writeStartupInfo(os.Stdout, target, previewURL)

	if *options.openPage {
		go func() {
			time.Sleep(200 * time.Millisecond)
			if err := openBrowser(previewURL); err != nil {
				slog.Error("open browser", "error", err)
			}
		}()
	}

	serverCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-serverCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeStartupInfo(w io.Writer, target remoteTarget, previewURL string) {
	fmt.Fprintf(w, "browsing %s:%s\n", target.Host, target.Root)
	fmt.Fprintf(w, "open: %s\n", previewURL)
}

func previewURLFor(addr net.Addr) string {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return "http://" + addr.String() + "/"
	}

	host := tcpAddr.IP.String()
	if tcpAddr.IP.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, fmt.Sprintf("%d", tcpAddr.Port)) + "/"
}

func listenTCPWithPortFallback(address string) (net.Listener, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return net.Listen("tcp", address)
	}

	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return net.Listen("tcp", address)
	}

	for candidatePort := port; candidatePort <= 65535; candidatePort++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(candidatePort))
		listener, listenErr := net.Listen("tcp", candidate)
		if listenErr == nil {
			if candidatePort != port {
				slog.Debug("listen port was occupied; shifted to next available port", "requested", address, "actual", candidate)
			}
			return listener, nil
		}
		if !errors.Is(listenErr, syscall.EADDRINUSE) || candidatePort == 65535 {
			return nil, listenErr
		}
	}

	return nil, fmt.Errorf("no available TCP port from %s", address)
}
