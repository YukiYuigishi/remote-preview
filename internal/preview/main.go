package preview

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

func Run(args []string, program string) error {
	configureLogging()

	flags := flag.NewFlagSet(program, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	addr := flags.String("addr", "127.0.0.1:8080", "listen address")
	openPage := flags.Bool("open", false, "open the file browser in the default browser")
	verbose := flags.Bool("v", false, "log each request")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s [options] host[:/remote/path]\n\n", filepath.Base(program))
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return errors.New("exactly one remote target is required")
	}

	target, err := parseTarget(flags.Arg(0))
	if err != nil {
		return err
	}

	backend := newSSHRemoteFS(target.Host)
	if target.Home {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		home, homeErr := backend.Home(ctx)
		cancel()
		if homeErr != nil {
			return fmt.Errorf("resolve remote home: %w", homeErr)
		}
		target.setResolvedHome(home)
	}

	remote := newCachedRemoteFS(backend, target.Host)
	h := &handler{target: target, remote: remote, verbose: *verbose}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	srv.Addr = listener.Addr().String()

	previewURL := previewURLFor(listener.Addr())
	slog.Info("server ready", "host", target.Host, "root", target.Root, "url", previewURL)

	if *openPage {
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
