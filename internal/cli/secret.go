package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/druxuss/Kern/internal/secrets"
	"github.com/spf13/cobra"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage secrets",
}

var secretSetCmd = &cobra.Command{
	Use:   "set [name] [value]",
	Short: "Store a secret",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		fromStdin, _ := cmd.Flags().GetBool("stdin")
		if fromStdin {
			if len(args) != 1 {
				return fmt.Errorf("use 'kern secret set NAME --stdin' without a value argument")
			}
			value, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("reading secret from stdin: %w", err)
			}
			return secrets.SetSecret(args[0], strings.TrimSuffix(strings.TrimSuffix(string(value), "\n"), "\r"))
		}
		if len(args) != 2 {
			return fmt.Errorf("provide a value or use --stdin to read it without shell history exposure")
		}
		return secrets.SetSecret(args[0], args[1])
	},
}

var secretGetCmd = &cobra.Command{
	Use:   "get [name]",
	Short: "Retrieve a secret",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		value, err := secrets.GetSecret(args[0])
		if err != nil {
			return err
		}
		fmt.Println(value)
		return nil
	},
}

var secretDeleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete a secret",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return secrets.DeleteSecret(args[0])
	},
}

func init() {
	secretSetCmd.Flags().Bool("stdin", false, "Read the secret value from stdin")
	secretCmd.AddCommand(secretSetCmd)
	secretCmd.AddCommand(secretGetCmd)
	secretCmd.AddCommand(secretDeleteCmd)
	rootCmd.AddCommand(secretCmd)
}
