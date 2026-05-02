package commands

import "github.com/spf13/cobra"

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debugging utilities",
}

func init() {
	debugCmd.AddCommand(debugDumpCmd)
	rootCmd.AddCommand(debugCmd)
}
