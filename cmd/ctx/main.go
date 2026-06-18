package main

import (
	"os"

	"github.com/kimhyoyeon/context-manager/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
