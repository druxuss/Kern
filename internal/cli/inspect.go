package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/druxuss/Kern/internal/kernfile"
	"github.com/druxuss/Kern/internal/packager"
	"github.com/spf13/cobra"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Show details about the built unikernel image",
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

		unikernelDir := filepath.Join(projectPath, ".unikernel")
		unikraftDir := filepath.Join(projectPath, ".unikraft", "build")

		fmt.Println("=== Image Info ===")
		fmt.Printf("Name:         %s\n", config.App.Name)
		fmt.Printf("Version:      %s\n", config.App.Version)
		fmt.Printf("Runtime:      %s\n", config.App.Runtime)
		fmt.Printf("Entry:        %s\n", config.App.Entry)
		fmt.Println()

		fmt.Println("=== Image Size ===")
		ukiPath := filepath.Join(unikernelDir, "image.uki")
		if info, err := os.Stat(ukiPath); err == nil {
			fmt.Printf("Image artifact: %s\n", formatSize(info.Size()))
		} else {
			fmt.Println("UKI:          not built yet")
		}

		initrdPath := filepath.Join(unikraftDir, "initramfs-x86_64.cpio")
		if info, err := os.Stat(initrdPath); err == nil {
			fmt.Printf("Initrd:       %s\n", formatSize(info.Size()))
		}

		kernelPath := filepath.Join(unikraftDir, "kernel")
		if info, err := os.Stat(kernelPath); err == nil {
			fmt.Printf("Kernel:       %s\n", formatSize(info.Size()))
		}
		fmt.Println()

		fmt.Println("=== Runtime Resources ===")
		if profile, err := kernfile.LoadProfile(config.App.Runtime); err == nil {
			if memory, cpus, err := kernfile.ResolveResources(config, profile); err == nil {
				fmt.Printf("Memory:       %d MiB\n", memory)
				fmt.Printf("vCPUs:        %d\n", cpus)
			}
		}
		fmt.Println()

		fmt.Println("=== Network ===")
		if len(config.Network.Ports) > 0 {
			for _, p := range config.Network.Ports {
				fmt.Printf("Port:         %d\n", p)
			}
		} else {
			fmt.Println("No ports exposed")
		}
		fmt.Println()

		// The signed artifact is an ELF with embedded initrd/cmdline sections.
		if _, err := os.Stat(ukiPath); err == nil {
			fmt.Println("=== Embedded ELF Sections ===")
			sections, err := getUKISections(ukiPath)
			if err == nil {
				for _, s := range sections {
					fmt.Printf("  %s\n", s)
				}
			}
			fmt.Println()
		}

		fmt.Println("=== Security ===")
		sigPath := filepath.Join(unikernelDir, "image.uki.sig")
		if _, err := os.Stat(sigPath); err == nil {
			fmt.Println("Signature:    ✓ ed25519")
		} else {
			fmt.Println("Signature:    ✗ not signed")
		}

		sbomPath := filepath.Join(unikernelDir, "image.sbom")
		if data, err := os.ReadFile(sbomPath); err == nil {
			var sbom packager.SBOM
			if err := json.Unmarshal(data, &sbom); err == nil {
				fmt.Printf("SBOM:         ✓ %s\n", sbom.SPDXVersion)
				fmt.Println()
				fmt.Println("=== SBOM Packages ===")
				for _, pkg := range sbom.Packages {
					fmt.Printf("  %-24s %s\n", pkg.Name, pkg.VersionInfo)
				}
			}
		} else {
			fmt.Println("SBOM:         ✗ not generated")
		}

		fmt.Println()
		fmt.Println("=== Registry ===")
		fmt.Printf("Push:         kern push ghcr.io/YOUR_USER/%s:%s\n", config.App.Name, config.App.Version)

		return nil
	},
}

func getUKISections(ukiPath string) ([]string, error) {
	out, err := exec.Command("readelf", "-S", "--wide", ukiPath).Output()
	if err != nil {
		return nil, err
	}

	var sections []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") || strings.HasPrefix(line, "[Nr]") {
			continue
		}
		// Eliminar el número: [ 1] -> .text ...
		bracketEnd := strings.Index(line, "]")
		if bracketEnd == -1 {
			continue
		}
		line = strings.TrimSpace(line[bracketEnd+1:])
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		name := fields[0]
		if name == "" || name == "NULL" {
			continue
		}

		interestingPrefixes := []string{".text", ".data", ".rodata", ".cmdline", ".initrd", ".bss"}
		show := false
		for _, prefix := range interestingPrefixes {
			if strings.HasPrefix(name, prefix) {
				show = true
				break
			}
		}
		if !show {
			continue
		}

		// El tamaño está en hex en la columna 4 (índice 4)
		sizeHex := fields[4]
		var size int64
		fmt.Sscanf(sizeHex, "%x", &size)
		sections = append(sections, fmt.Sprintf("  %-30s %s", name, formatSize(size)))
	}
	return sections, nil
}

func formatSize(bytes int64) string {
	const mb = 1024 * 1024
	const kb = 1024
	if bytes >= mb {
		return fmt.Sprintf("%.1f MB", float64(bytes)/mb)
	}
	if bytes >= kb {
		return fmt.Sprintf("%.1f KB", float64(bytes)/kb)
	}
	return fmt.Sprintf("%d B", bytes)
}

func init() {
	rootCmd.AddCommand(inspectCmd)
}
