package cli

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/druxuss/Kern/internal/kernfile"
	"github.com/druxuss/Kern/internal/runner"
	"github.com/druxuss/Kern/internal/secrets"
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
		if instance, err := instances.GetByName(config.App.Name); err == nil && instance.Status == "running" {
			return fmt.Errorf("%q is already running (PID %d); stop it before starting another instance", config.App.Name, instance.PID)
		}

		profile, err := kernfile.LoadProfile(config.App.Runtime)
		if err != nil {
			return fmt.Errorf("error loading runtime profile: %w", err)
		}

		ports, hostPorts, err := effectivePorts(config.Network.Ports, portOverrides)
		if err != nil {
			return err
		}
		config.Network.Ports = hostPorts

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
		config.Env, err = secrets.ResolveSecrets(config.Env)
		if err != nil {
			return fmt.Errorf("resolving application secrets: %w", err)
		}
		memoryMiB, cpus, err := kernfile.ResolveResources(config, profile)
		if err != nil {
			return fmt.Errorf("invalid resource settings: %w", err)
		}

		fmt.Printf("Running %s v%s...\n", config.App.Name, config.App.Version)

		// Intentar usar runner directo de QEMU
		kernelPath, initrdPath, err := runner.FindUnikraftPaths(projectPath)
		if err != nil {
			return fmt.Errorf("QEMU kernel/initramfs unavailable: %w; run 'kern build' before 'kern run'", err)
		}
		return runDirect(config, kernelPath, initrdPath, projectPath, detach, memoryMiB, cpus, ports)
	},
}

func runDirect(config *kernfile.KernfileConfig, kernelPath, initrdPath, projectPath string, detachMode bool, memoryMiB, cpus int, ports []runner.PortMapping) error {
	hypervisor := runner.DetectHypervisor()
	fmt.Printf("Hypervisor: %s (direct QEMU)\n", hypervisor)

	cmdMap := map[string]string{
		"node":   "/usr/bin/node /usr/src/" + config.App.Entry,
		"python": "/usr/local/bin/python3.12 /src/" + config.App.Entry,
		"go":     "/usr/src/app",
		"rust":   "/usr/src/app",
		"bun":    "/usr/bin/bun run /usr/src/" + config.App.Entry,
	}

	tapIface := os.Getenv("KERN_TAP")
	macAddr := os.Getenv("KERN_MAC")

	cfg := runner.RunConfig{
		KernelPath:   kernelPath,
		InitrdPath:   initrdPath,
		MemoryMiB:    memoryMiB,
		CPUs:         cpus,
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
		qemuLogPath := filepath.Join(logDir, config.App.Name+".qemu.log")
		qemuLog, err := os.OpenFile(qemuLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("could not open QEMU log: %w", err)
		}
		args := runner.BuildQEMUArgs(cfg)
		qemuCmd := exec.Command("qemu-system-x86_64", args...)
		qemuCmd.Dir = projectPath
		qemuCmd.Stdout = qemuLog
		qemuCmd.Stderr = qemuLog
		if err := qemuCmd.Start(); err != nil {
			qemuLog.Close()
			return fmt.Errorf("error starting in background: %w", err)
		}

		waitDone := make(chan error, 1)
		go func() {
			waitDone <- qemuCmd.Wait()
			qemuLog.Close()
		}()
		select {
		case err := <-waitDone:
			if err != nil {
				return fmt.Errorf("QEMU exited during startup (details in %s): %w", qemuLogPath, err)
			}
			return fmt.Errorf("QEMU exited during startup (details in %s)", qemuLogPath)
		case <-time.After(2 * time.Second):
		}

		instance := &instances.Instance{
			ID:        generateID(),
			Name:      config.App.Name,
			Runtime:   config.App.Runtime,
			PID:       qemuCmd.Process.Pid,
			Ports:     config.Network.Ports,
			MemoryMiB: memoryMiB,
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

func effectivePorts(configured []int, overrides []string) ([]runner.PortMapping, []int, error) {
	if len(overrides) == 0 {
		ports := make([]runner.PortMapping, 0, len(configured))
		for _, port := range configured {
			ports = append(ports, runner.PortMapping{Host: port, Container: port})
		}
		return ports, append([]int(nil), configured...), nil
	}

	ports := make([]runner.PortMapping, 0, len(overrides))
	hostPorts := make([]int, 0, len(overrides))
	seenHosts := make(map[int]struct{}, len(overrides))
	for _, value := range overrides {
		parts := strings.Split(value, ":")
		if len(parts) != 2 {
			return nil, nil, fmt.Errorf("invalid port mapping %q; expected host:container", value)
		}
		host, err := strconv.Atoi(parts[0])
		if err != nil || host < 1 || host > 65535 {
			return nil, nil, fmt.Errorf("invalid host port in mapping %q", value)
		}
		if _, exists := seenHosts[host]; exists {
			return nil, nil, fmt.Errorf("host port %d is mapped more than once", host)
		}
		seenHosts[host] = struct{}{}
		container, err := strconv.Atoi(parts[1])
		if err != nil || container < 1 || container > 65535 {
			return nil, nil, fmt.Errorf("invalid container port in mapping %q", value)
		}
		ports = append(ports, runner.PortMapping{Host: host, Container: container})
		hostPorts = append(hostPorts, host)
	}
	return ports, hostPorts, nil
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
