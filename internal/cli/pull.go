package cli

import (
	"fmt"
	"os"

	"github.com/druxuss/Kern/internal/registry"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull [reference]",
	Short: "Pull a unikernel image from a registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reference := args[0]

		destPath, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not get current directory: %w", err)
		}

		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")

		creds := registry.Credentials{
			Username: username,
			Password: password,
		}

		fmt.Printf("Pulling %s...\n", reference)
		return registry.PullImage(reference, destPath+"/.unikernel", creds)
	},
}

func init() {
	pullCmd.Flags().StringP("username", "u", "", "Registry username")
	pullCmd.Flags().StringP("password", "p", "", "Registry password")
	rootCmd.AddCommand(pullCmd)
}
