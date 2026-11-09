package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/druxuss/Kern/internal/common"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a project by creating a Kernfile",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectPath, err := os.Getwd()
		choice := ""
		if err != nil {
			return fmt.Errorf("could not get current directory: %w", err)
		}

		runtime := detectRuntime(projectPath)

		if runtime == "" {
			return fmt.Errorf("Could not find any compatible runtime (%s)...", common.GetStringSupportedRuntimes())
		}
		fmt.Printf("Runtime detected: %s\n", runtime)

		fmt.Printf("Initialize kernfile in \"%s\" ? [Y/n]:\n", projectPath)
		fmt.Scanln(&choice)
		choice = strings.ToLower(choice)

		if choice != "" && choice != "y" && choice != "yes" {
			fmt.Print("Exiting project initalizing...\n")
			return nil
		}

		return createKernfile(projectPath, runtime)
	},
}

func detectRuntime(path string) string {
	checks := []struct {
		file    string
		runtime string
	}{
		{"Cargo.toml", "rust"},
		{"bun.lockb", "bun"},
		{"go.mod", "go"},
		{"requirements.txt", "python"},
		{"package.json", "node"},
	}

	for _, check := range checks {
		if _, err := os.Stat(path + "/" + check.file); err == nil {
			return check.runtime
		}
	}

	return ""
}

func createKernfile(path, runtime string) error {
	kernfilePath := filepath.Join(path, "Kernfile")
	if _, err := os.Stat(kernfilePath); err == nil {
		return fmt.Errorf("a Kernfile already exists in this directory")
	}

	entryMap := map[string]string{
		"node":   "index.js",
		"go":     "main.go",
		"rust":   "main.rs",
		"python": "main.py",
		"bun":    "index.ts",
	}
	entry := entryMap[runtime]
	name := filepath.Base(path)
	if runtime == "rust" {
		var manifest struct {
			Package struct {
				Name string `toml:"name"`
			} `toml:"package"`
			Bin []struct {
				Name string `toml:"name"`
			} `toml:"bin"`
		}
		if _, err := toml.DecodeFile(filepath.Join(path, "Cargo.toml"), &manifest); err != nil {
			return fmt.Errorf("could not read Cargo package/binary name: %w", err)
		}
		if len(manifest.Bin) > 0 && manifest.Bin[0].Name != "" {
			name = manifest.Bin[0].Name
		} else if manifest.Package.Name != "" {
			name = manifest.Package.Name
		} else {
			return fmt.Errorf("Cargo.toml must define a package name or binary target")
		}
		if !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(name) {
			return fmt.Errorf("Rust binary name %q cannot be used as Kernfile app.name; rename the Cargo binary to use letters, numbers, or hyphens", name)
		}
	} else {
		name = regexp.MustCompile(`[^a-zA-Z0-9-]+`).ReplaceAllString(name, "-")
		name = strings.Trim(name, "-")
		if name == "" {
			name = "kern-app"
		}
	}
	memory := map[string]string{
		"node": "512Mi", "python": "384Mi", "go": "128Mi", "rust": "64Mi", "bun": "512Mi",
	}[runtime]

	content := fmt.Sprintf(`kernfile = "1.0"

[app]
name    = "%s"
version = "0.1.0"
runtime = "%s"
entry   = "%s"

[network]
ports = [3000]

[resources]
memory = "%s"
cpu    = 1

[env]
NODE_ENV = "production"
`, name, runtime, entry, memory)

	if err := os.WriteFile(kernfilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("error creating Kernfile: %w", err)
	}
	fmt.Println("✓ Kernfile created — review and adjust the fields as needed")
	dockerignore := ".unikraft\n.unikernel\n.git\nnode_modules\nDockerfile\nKraftfile\n*.sig\n*.sbom\n"
	dockerignorePath := filepath.Join(filepath.Dir(kernfilePath), ".dockerignore")
	if _, err := os.Stat(dockerignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(dockerignorePath, []byte(dockerignore), 0644); err != nil {
			return fmt.Errorf("error creating .dockerignore: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("error checking .dockerignore: %w", err)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(initCmd)
}
