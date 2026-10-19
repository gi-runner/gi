package main

import (
	"os"

	"github.com/gi-runner/gi/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
