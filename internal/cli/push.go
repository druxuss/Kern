package cli

import (
	"fmt"
	"os"

	"github.com/druxuss/Kern/internal/registry"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push [reference]",
	Short: "Push a unikernel image to a registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reference := args[0]

		projectPath, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not get current directory: %w", err)
		}

		imagePath := projectPath + "/.unikernel/image.uki"
		if _, err := os.Stat(imagePath); err != nil {
			return fmt.Errorf("no image found — run 'kern build' first")
		}

		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")

		creds := registry.Credentials{
			Username: username,
			Password: password,
		}

		fmt.Printf("Pushing to %s...\n", reference)
		return registry.PushImage(imagePath, reference, creds)
	},
}

func init() {
	pushCmd.Flags().StringP("username", "u", "", "Registry username")
	pushCmd.Flags().StringP("password", "p", "", "Registry password")
	rootCmd.AddCommand(pushCmd)
}
