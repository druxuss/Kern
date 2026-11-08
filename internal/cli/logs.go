package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/spf13/cobra"
)

var followLogs bool

var logsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "Show logs of a running unikernel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		instance, err := instances.GetByName(name)
		if err != nil {
			return fmt.Errorf("instance %q not found", name)
		}

		logPath := instance.LogPath
		if logPath == "" {
			logPath, err = findVMLog(instance.PID)
			if err != nil {
				return fmt.Errorf("could not find logs: %w", err)
			}
		}

		if followLogs {
			return followLog(logPath)
		}

		return printLog(logPath)
	},
}

func findVMLog(pid int) (string, error) {
	// Buscar en directorios de kern directo
	matches, _ := filepath.Glob("/tmp/kern-*/vm.log")
	for _, match := range matches {
		pidFile := filepath.Join(filepath.Dir(match), "machine.pid")
		data, err := os.ReadFile(pidFile)
		if err != nil {
			continue
		}
		var filePid int
		fmt.Sscanf(string(data), "%d", &filePid)
		if filePid == pid {
			return match, nil
		}
	}

	// Buscar en kraftkit
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	runtimeDir := filepath.Join(home, ".local", "share", "kraftkit", "runtime")
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		return "", fmt.Errorf("kraftkit runtime directory not found: %w", err)
	}

	for _, entry := range entries {
		pidFile := filepath.Join(runtimeDir, entry.Name(), "machine.pid")
		data, err := os.ReadFile(pidFile)
		if err != nil {
			continue
		}

		var filePid int
		fmt.Sscanf(string(data), "%d", &filePid)
		if filePid == pid {
			logPath := filepath.Join(runtimeDir, entry.Name(), "vm.log")
			if _, err := os.Stat(logPath); err == nil {
				return logPath, nil
			}
		}
	}

	return "", fmt.Errorf("log file not found for PID %d", pid)
}

func printLog(logPath string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return fmt.Errorf("error reading log: %w", err)
	}

	if len(data) == 0 {
		fmt.Println("No logs yet")
		return nil
	}

	fmt.Print(string(data))
	return nil
}

func followLog(logPath string) error {
	file, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("error opening log: %w", err)
	}
	defer file.Close()

	// Ir al final del archivo
	file.Seek(0, io.SeekEnd)

	fmt.Println("Following logs (Ctrl+C to stop)...")

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Print(line)
		}
		if err != nil {
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func init() {
	logsCmd.Flags().BoolVarP(&followLogs, "follow", "f", false, "Follow log output")
	rootCmd.AddCommand(logsCmd)
}
