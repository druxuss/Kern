package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/druxuss/Kern/internal/kernfile"
	"github.com/druxuss/Kern/internal/runner"
	"github.com/spf13/cobra"
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back to the previous unikernel image",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectPath, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not get current directory: %w", err)
		}

		unikernelDir := filepath.Join(projectPath, ".unikernel")
		ukiPath := filepath.Join(unikernelDir, "image.uki")
		prevPath := filepath.Join(unikernelDir, "image.uki.prev")
		sigPath := filepath.Join(unikernelDir, "image.uki.sig")

		// Verificar que existe imagen anterior
		if _, err := os.Stat(prevPath); err != nil {
			return fmt.Errorf("no previous image found — run 'kern build' at least twice")
		}

		kernfilePath := filepath.Join(projectPath, "Kernfile")
		config, err := kernfile.ParseKernfile(kernfilePath)
		if err != nil {
			return fmt.Errorf("error reading Kernfile: %w", err)
		}

		fmt.Printf("Rolling back %s v%s...\n", config.App.Name, config.App.Version)

		// Parar instancia si está corriendo
		instance, err := instances.GetByName(config.App.Name)
		if err == nil && instance.Status == "running" {
			fmt.Printf("→ Stopping current instance (PID %d)...\n", instance.PID)
			stopCmd := exec.Command("kern", "stop", config.App.Name)
			stopCmd.Stdout = os.Stdout
			stopCmd.Stderr = os.Stderr
			stopCmd.Run()
			exec.Command("kern", "rm", config.App.Name).Run()
			time.Sleep(1 * time.Second)
		}

		// Restaurar imagen anterior
		fmt.Println("→ Restoring previous image...")
		prevData, err := os.ReadFile(prevPath)
		if err != nil {
			return fmt.Errorf("error reading previous image: %w", err)
		}
		if err := os.WriteFile(ukiPath, prevData, 0755); err != nil {
			return fmt.Errorf("error restoring image: %w", err)
		}

		// Eliminar firma anterior — ya no es válida
		os.Remove(sigPath)
		fmt.Println("✓ Previous image restored")

		// Relanzar
		fmt.Println("→ Restarting with previous image...")
		kernelPath, initrdPath, err := runner.FindUnikraftPaths(projectPath)
		if err != nil {
			return fmt.Errorf("kernel not found — run 'kern build' first: %w", err)
		}

		cmdMap := map[string]string{
			"node":   "/usr/bin/node /usr/src/" + config.App.Entry,
			"python": "/usr/local/bin/python3.12 /src/" + config.App.Entry,
			"go":     "/usr/src/app",
			"rust":   "/usr/src/app",
			"bun":    "/usr/bin/bun run /usr/src/" + config.App.Entry,
		}

		memoryMap := map[string]int{
			"node": 512, "python": 384, "go": 128, "rust": 128, "bun": 256,
		}

		var ports []runner.PortMapping
		for _, p := range config.Network.Ports {
			ports = append(ports, runner.PortMapping{
				Host:      p,
				Container: p,
			})
		}

		cfg := runner.RunConfig{
			KernelPath: kernelPath,
			InitrdPath: initrdPath,
			MemoryMiB:  memoryMap[config.App.Runtime],
			CPUs:       1,
			Ports:      ports,
			Hypervisor: runner.DetectHypervisor(),
			AppCmd:     cmdMap[config.App.Runtime],
			Env:        config.Env,
		}

		qemuArgs := runner.BuildQEMUArgs(cfg)
		qemuCmd := exec.Command("qemu-system-x86_64", qemuArgs...)
		qemuCmd.Dir = projectPath
		qemuCmd.Stdin = nil
		qemuCmd.Stdout = nil
		qemuCmd.Stderr = nil

		if err := qemuCmd.Start(); err != nil {
			return fmt.Errorf("error starting unikernel: %w", err)
		}

		time.Sleep(2 * time.Second)

		instance = &instances.Instance{
			ID:        generateID(),
			Name:      config.App.Name,
			Runtime:   config.App.Runtime,
			PID:       qemuCmd.Process.Pid,
			Ports:     config.Network.Ports,
			MemoryMiB: memoryMap[config.App.Runtime],
			CreatedAt: time.Now(),
			Status:    "running",
		}
		if err := instances.Save(instance); err != nil {
			fmt.Printf("warning: could not save instance state: %v\n", err)
		}

		fmt.Printf("✓ %s running with previous image (PID %d)\n", config.App.Name, qemuCmd.Process.Pid)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(rollbackCmd)
}
