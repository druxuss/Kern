package cli

import (
	"fmt"
	"time"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/druxuss/Kern/internal/qmp"
	"github.com/spf13/cobra"
)

var watchMetrics bool

var metricsCmd = &cobra.Command{
	Use:   "metrics [name]",
	Short: "Show real-time metrics of a running unikernel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		instance, err := instances.GetByName(name)
		if err != nil {
			return fmt.Errorf("instance %q not found", name)
		}

		if instance.Status == "stopped" {
			return fmt.Errorf("%q is not running", name)
		}

		socketPath, err := qmp.FindSocket(instance.PID)
		if err != nil {
			return fmt.Errorf("could not find QMP socket: %w", err)
		}

		if watchMetrics {
			fmt.Printf("Watching metrics for %s (Ctrl+C to stop)...\n\n", name)
			for {
				if err := printMetrics(name, instance.PID, socketPath); err != nil {
					return err
				}
				time.Sleep(2 * time.Second)
				fmt.Print("\033[2J\033[H") // clear screen
			}
		}

		return printMetrics(name, instance.PID, socketPath)
	},
}

func printMetrics(name string, pid int, socketPath string) error {
	client, err := qmp.Connect(socketPath)
	if err != nil {
		return fmt.Errorf("could not connect to QMP: %w", err)
	}
	defer client.Close()

	fmt.Printf("=== %s metrics ===\n", name)
	fmt.Printf("PID:    %d\n", pid)

	status, err := client.GetStatus()
	if err == nil {
		fmt.Printf("Status: %s\n", status)
	}

	memBytes, err := client.GetMemoryStats()
	if err == nil {
		fmt.Printf("Memory: %.1f MB (%.1f MiB)\n",
			float64(memBytes)/1000/1000,
			float64(memBytes)/1024/1024)
	} else {
		fmt.Printf("Memory: unavailable (%v)\n", err)
	}

	cpus, err := client.GetCPUStats()
	if err == nil {
		fmt.Printf("vCPUs:  %d\n", len(cpus))
	}

	fmt.Printf("Time:   %s\n", time.Now().Format("15:04:05"))

	return nil
}

func init() {
	metricsCmd.Flags().BoolVarP(&watchMetrics, "watch", "w", false, "Watch metrics in real-time")
	rootCmd.AddCommand(metricsCmd)
}
