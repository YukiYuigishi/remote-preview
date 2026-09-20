package main

import (
	"log/slog"
	"os"

	"remote-preview/internal/preview"
)

func main() {
	if err := preview.Run(os.Args[1:], os.Args[0]); err != nil {
		slog.Error("remote-preview exited", "error", err)
		os.Exit(1)
	}
}
