package preview

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestConfigureLogging(t *testing.T) {
	previousLogger := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	tests := []struct {
		name      string
		debug     string
		wantDebug bool
	}{
		{name: "debug enabled", debug: "1", wantDebug: true},
		{name: "debug disabled", debug: "", wantDebug: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DEBUG", tt.debug)

			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			previousStderr := os.Stderr
			os.Stderr = writer
			defer func() {
				os.Stderr = previousStderr
				_ = reader.Close()
			}()

			configureLogging()
			slog.Debug("debug probe", "probe", "value")
			slog.Info("info probe", "probe", "value")
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			output, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			hasDebug := strings.Contains(string(output), "debug probe")
			if hasDebug != tt.wantDebug {
				t.Fatalf("debug log presence = %v, want %v; output=%q", hasDebug, tt.wantDebug, output)
			}
			if !strings.Contains(string(output), "info probe") || !strings.Contains(string(output), "probe=value") {
				t.Fatalf("structured info log missing; output=%q", output)
			}
		})
	}
}
