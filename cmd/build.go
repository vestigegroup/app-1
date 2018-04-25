package cmd

import (
	"fmt"

	"github.com/docker/lunchbox/internal"
	"github.com/spf13/cobra"
)

// buildCmd represents the build command
var buildCmd = &cobra.Command{
	Use:   "build <app-name>",
	Short: "Compile an app package from locally available data",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("build called")
	},
}

func init() {
	if internal.Experimental == "on" {
		rootCmd.AddCommand(buildCmd)
	}
}
