package cli

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/druxuss/Kern/internal/kernfile"
	"github.com/druxuss/Kern/internal/runner"
	"github.com/spf13/cobra"
)

var detach bool
var portOverrides []string
var envOverrides []string

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the unikernel locally",
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

		profile, err := kernfile.LoadProfile(config.App.Runtime)
		if err != nil {
			return fmt.Errorf("error loading runtime profile: %w", err)
		}

		// Aplicar overrides de puertos
		if len(portOverrides) > 0 {
			config.Network.Ports = []int{}
			for _, p := range portOverrides {
				parts := strings.Split(p, ":")
				if len(parts) == 2 {
					var port int
					fmt.Sscanf(parts[0], "%d", &port)
					config.Network.Ports = append(config.Network.Ports, port)
				}
			}
		}

		// Aplicar overrides de env
		if len(envOverrides) > 0 {
			if config.Env == nil {
				config.Env = map[string]string{}
			}
			for _, e := range envOverrides {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 {
					config.Env[parts[0]] = parts[1]
				}
			}
		}

		fmt.Printf("Running %s v%s...\n", config.App.Name, config.App.Version)

		// Intentar usar runner directo de QEMU
		kernelPath, initrdPath, err := runner.FindUnikraftPaths(projectPath)
		if err == nil {
			return runDirect(config, kernelPath, initrdPath, projectPath, detach)
		}

		// Fallback a kraft
		return kraftRun(config, profile, projectPath, detach)
	},
}

func runDirect(config *kernfile.KernfileConfig, kernelPath, initrdPath, projectPath string, detachMode bool) error {
	hypervisor := runner.DetectHypervisor()
	fmt.Printf("Hypervisor: %s (direct QEMU)\n", hypervisor)

	cmdMap := map[string]string{
		"node":   "/usr/bin/node /usr/src/" + config.App.Entry,
		"python": "/usr/local/bin/python3.12 /src/" + config.App.Entry,
		"go":     "/usr/src/app",
		"rust":   "/usr/src/app",
		"bun":    "/usr/bin/bun run /usr/src/" + config.App.Entry,
	}

	var ports []runner.PortMapping
	for _, p := range config.Network.Ports {
		ports = append(ports, runner.PortMapping{
			Host:      p,
			Container: p,
		})
	}

	memoryMap := map[string]int{
		"node": 512, "python": 384, "go": 128, "rust": 128, "bun": 256,
	}

	tapIface := os.Getenv("KERN_TAP")
	macAddr := os.Getenv("KERN_MAC")

	cfg := runner.RunConfig{
		KernelPath:   kernelPath,
		InitrdPath:   initrdPath,
		MemoryMiB:    memoryMap[config.App.Runtime],
		CPUs:         1,
		Ports:        ports,
		Hypervisor:   hypervisor,
		AppCmd:       cmdMap[config.App.Runtime],
		Env:          config.Env,
		TAPInterface: tapIface,
		MACAddress:   macAddr,
	}

	if detachMode {
		logDir := filepath.Join(projectPath, ".unikernel", "logs")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("could not create serial log directory: %w", err)
		}
		cfg.SerialLogPath = filepath.Join(logDir, config.App.Name+".log")
		args := runner.BuildQEMUArgs(cfg)
		qemuCmd := exec.Command("qemu-system-x86_64", args...)
		qemuCmd.Dir = projectPath
		if err := qemuCmd.Start(); err != nil {
			sleepTime := 2 * time.Second
			if config.App.Runtime == "python" {
				sleepTime = 8 * time.Second
			}
			time.Sleep(sleepTime)
			return fmt.Errorf("error starting in background: %w", err)
		}

		time.Sleep(2 * time.Second)

		instance := &instances.Instance{
			ID:        generateID(),
			Name:      config.App.Name,
			Runtime:   config.App.Runtime,
			PID:       qemuCmd.Process.Pid,
			Ports:     config.Network.Ports,
			MemoryMiB: memoryMap[config.App.Runtime],
			CreatedAt: time.Now(),
			Status:    "running",
			LogPath:   cfg.SerialLogPath,
		}
		if err := instances.Save(instance); err != nil {
			fmt.Printf("warning: could not save instance state: %v\n", err)
		}

		fmt.Printf("✓ %s running in background (PID %d)\n", config.App.Name, qemuCmd.Process.Pid)
		return nil
	}

	return runner.RunUnikernel(cfg)
}

func kraftRun(config *kernfile.KernfileConfig, profile *kernfile.RuntimeProfile, projectPath string, detachMode bool) error {
	kraftArgs := []string{"run", "--plat", "qemu", "--arch", "x86_64", "-M", profile.Memory}

	if detachMode {
		kraftArgs = append(kraftArgs, "--detach")
	}

	for _, p := range config.Network.Ports {
		kraftArgs = append(kraftArgs, "-p", fmt.Sprintf("%d:%d", p, p))
	}

	kraftArgs = append(kraftArgs, ".")

	kraftCmd := exec.Command("kraft", kraftArgs...)
	kraftCmd.Dir = projectPath
	kraftCmd.Stdout = os.Stdout
	kraftCmd.Stderr = os.Stderr

	if err := kraftCmd.Run(); err != nil {
		return err
	}

	if detachMode {
		time.Sleep(2 * time.Second)

		qemuPid, err := instances.FindQEMUPid()
		if err != nil {
			qemuPid = kraftCmd.Process.Pid
		}

		memoryMap := map[string]int{
			"node": 512, "python": 384, "go": 128, "rust": 128, "bun": 256,
		}

		instance := &instances.Instance{
			ID:        generateID(),
			Name:      config.App.Name,
			Runtime:   config.App.Runtime,
			PID:       qemuPid,
			Ports:     config.Network.Ports,
			MemoryMiB: memoryMap[config.App.Runtime],
			CreatedAt: time.Now(),
			Status:    "running",
		}
		if err := instances.Save(instance); err != nil {
			fmt.Printf("warning: could not save instance state: %v\n", err)
		}

		fmt.Printf("✓ %s running in background (PID %d)\n", config.App.Name, qemuPid)
	}

	return nil
}

func generateID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[r.Intn(len(chars))]
	}
	return string(b)
}

func init() {
	runCmd.Flags().BoolVarP(&detach, "detach", "d", false, "Run in background")
	runCmd.Flags().StringArrayVarP(&portOverrides, "port", "p", []string{}, "Override ports (format: host:container)")
	runCmd.Flags().StringArrayVarP(&envOverrides, "env", "e", []string{}, "Set environment variables (format: KEY=VALUE)")
	rootCmd.AddCommand(runCmd)
}
