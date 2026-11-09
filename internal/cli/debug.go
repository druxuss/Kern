package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/druxuss/Kern/internal/qmp"
	"github.com/spf13/cobra"
)

var debugCmd = &cobra.Command{
	Use:   "debug [name]",
	Short: "Show debug information of a running unikernel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		instance, err := instances.GetByName(name)
		if err != nil {
			return fmt.Errorf("instance %q not found", name)
		}

		fmt.Printf("=== Debug: %s (PID %d) ===\n\n", name, instance.PID)
		if instance.LogPath != "" {
			fmt.Printf("Serial log: %s\n", instance.LogPath)
			fmt.Printf("QEMU log:   %s\n\n", filepath.Join(filepath.Dir(instance.LogPath), name+".qemu.log"))
		}

		// Estado via QMP (QEMU Monitor Protocol)
		socketPath, err := qmp.FindSocket(instance.PID)
		if err == nil {
			client, err := qmp.Connect(socketPath)
			if err == nil {
				defer client.Close()

				fmt.Println("--- QMP Status ---")
				status, err := client.GetStatus()
				if err == nil {
					fmt.Printf("Status:  %s\n", status)
				}

				cpus, err := client.GetCPUStats()
				if err == nil {
					fmt.Printf("vCPUs:   %d\n", len(cpus))
				}

				fmt.Println()
			}
		}

		// Info del proceso en el Host (Linux)
		fmt.Println("--- Process Info ---")
		procStatus := filepath.Join(fmt.Sprintf("/proc/%d/status", instance.PID))
		if data, err := os.ReadFile(procStatus); err == nil {
			fmt.Print(string(data))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(debugCmd)
}
