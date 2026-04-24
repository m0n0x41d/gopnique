package main

import (
	"os"

	"github.com/ivanzakutnii/error-tracker/internal/runtime/commands"
)

func main() {
	exitCode := commands.Run(os.Args[1:], os.Environ(), os.Stdout, os.Stderr)

	os.Exit(exitCode)
}
