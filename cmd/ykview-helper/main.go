package main

import (
	"fmt"
	"os"

	"ykview/internal/remotehelper"
)

func main() {
	if err := remotehelper.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
