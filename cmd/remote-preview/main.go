package main

import (
	"log"
	"os"

	"remote-preview/internal/preview"
)

func main() {
	if err := preview.Run(os.Args[1:], os.Args[0]); err != nil {
		log.Fatal(err)
	}
}
