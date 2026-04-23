package main

import (
	"os"

	"github.com/upuai-cloud/cli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
