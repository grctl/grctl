package main

import (
	"os"

	"grctl/server/grctl/commands"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	commands.SetVersion(version, commit, date)
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
