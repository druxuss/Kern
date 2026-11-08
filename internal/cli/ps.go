package cli

import (
	"fmt"
	"time"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List running unikernels",
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")

		list, err := instances.List()
		if err != nil {
			return fmt.Errorf("error listing instances: %w", err)
		}

		// Filtrar stopped si no se pasa --all
		var filtered []*instances.Instance
		for _, i := range list {
			if i.Status == "running" || all {
				filtered = append(filtered, i)
			}
		}

		if len(filtered) == 0 {
			fmt.Println("No unikernels running")
			return nil
		}

		fmt.Printf("%-12s %-10s %-10s %-8s %-10s %-12s %s\n",
			"NAME", "RUNTIME", "STATUS", "MEM", "PID", "UPTIME", "PORTS")
		fmt.Println("----------------------------------------------------------------------")

		for _, i := range filtered {
			ports := ""
			for idx, p := range i.Ports {
				if idx > 0 {
					ports += ", "
				}
				ports += fmt.Sprintf("%d", p)
			}

			uptime := "-"
			if i.Status == "running" {
				uptime = formatDuration(time.Since(i.CreatedAt))
			}

			fmt.Printf("%-12s %-10s %-10s %-8s %-10d %-12s %s\n",
				i.Name,
				i.Runtime,
				i.Status,
				fmt.Sprintf("%dMi", i.MemoryMiB),
				i.PID,
				uptime,
				ports,
			)
		}
		return nil
	},
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%dh", int(d.Hours()/24), int(d.Hours())%24)
}

func init() {
	psCmd.Flags().BoolP("all", "a", false, "Show all instances including stopped")
	rootCmd.AddCommand(psCmd)
}
