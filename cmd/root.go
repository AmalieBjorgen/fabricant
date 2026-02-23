package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "fabricant",
	Short: "Fabricant: Git Workflow TUI for Microsoft Fabric and Azure DevOps",
	Long:  `A Terminal User Interface to help manage feature workspaces and branches in MS Fabric and Azure DevOps.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Default action is to show help for now
		cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Flags and configuration settings can be defined here
}
