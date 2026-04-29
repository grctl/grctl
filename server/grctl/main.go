package main

import (
	"os"

	"grctl/server/grctl/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
