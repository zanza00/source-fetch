package main

import (
	"os"

	"github.com/zanza00/source-fetch/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
