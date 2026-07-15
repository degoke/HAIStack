package main

import (
	"os"

	"github.com/degoke/health-ai-stack/cmd/haistack/command"
)

func main() {
	if err := command.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
