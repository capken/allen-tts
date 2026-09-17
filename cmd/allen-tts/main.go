package main

import (
	"os"

	"github.com/capken/allen-tts/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
