package cli

import (
	"fmt"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/spf13/cobra"
)

var rmCmd = &cobra.Command{
	Use:   "rm [name]",
	Short: "Remove a stopped unikernel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		instance, err := instances.GetByName(name)
		if err != nil {
			return fmt.Errorf("instance %q not found", name)
		}

		if instance.Status == "running" {
			return fmt.Errorf("%q is still running — stop it first with 'kern stop %s'", name, name)
		}

		if err := instances.Remove(instance.ID); err != nil {
			return fmt.Errorf("error removing instance: %w", err)
		}

		fmt.Printf("✓ %s removed\n", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(rmCmd)
}
