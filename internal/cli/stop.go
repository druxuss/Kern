package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop [name]",
	Short: "Stop a running unikernel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		instance, err := instances.GetByName(name)
		if err != nil {
			return fmt.Errorf("instance %q not found", name)
		}
		if instance.Status != "running" {
			return fmt.Errorf("instance %q is not running", name)
		}

		process, err := os.FindProcess(instance.PID)
		if err != nil {
			return fmt.Errorf("could not find process %d: %w", instance.PID, err)
		}

		if err := process.Kill(); err != nil {
			return fmt.Errorf("could not stop %q: %w", name, err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			current, err := instances.GetByName(name)
			if err != nil || current.Status != "running" {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if current, err := instances.GetByName(name); err == nil && current.Status == "running" {
			return fmt.Errorf("QEMU process %d did not stop within 5 seconds", instance.PID)
		}

		instance.Status = "stopped"
		if err := instances.Save(instance); err != nil {
			fmt.Printf("warning: could not update instance state: %v\n", err)
		}

		fmt.Printf("✓ %s stopped\n", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
