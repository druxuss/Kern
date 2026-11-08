package cli

import (
	"fmt"
	"os"

	"github.com/druxuss/Kern/internal/build"
	"github.com/druxuss/Kern/internal/kernfile"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Compile the application as a unikernel",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectPath, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not get current directory: %w", err)
		}

		kernfilePath := projectPath + "/Kernfile"
		config, err := kernfile.ParseKernfile(kernfilePath)
		if err != nil {
			return fmt.Errorf("error reading Kernfile: %w", err)
		}

		fmt.Printf("Building %s v%s (%s)...\n", config.App.Name, config.App.Version, config.App.Runtime)

		return build.Build(config, projectPath)
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)
}
