package kernfile

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type KernfileConfig struct {
	KernfileVersion string            `toml:"kernfile"`
	App             AppConfig         `toml:"app"`
	Network         NetworkConfig     `toml:"network"`
	Resources       ResourcesConfig   `toml:"resources"`
	Env             map[string]string `toml:"env"`
	Volumes         map[string]string `toml:"volumes"`
	Targets         []TargetConfig    `toml:"targets"`
}

type AppConfig struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Runtime     string `toml:"runtime"`
	Entry       string `toml:"entry"`
	Description string `toml:"description"`
}

type NetworkConfig struct {
	Ports []int `toml:"ports"`
}

type ResourcesConfig struct {
	Memory string `toml:"memory"`
	CPU    int    `toml:"cpu"`
}

type TargetConfig struct {
	Name     string `toml:"name"`
	Platform string `toml:"platform"`
	Arch     string `toml:"arch"`
}

func ParseKernfile(path string) (*KernfileConfig, error) {
	var config KernfileConfig

	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, fmt.Errorf("error leyendo el Kernfile: %w", err)
	}

	if err := validate(config); err != nil {
		return nil, err
	}

	return &config, nil
}

func validate(config KernfileConfig) error {
	// Nombre: solo letras, números y guiones
	nameRegex := regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	if !nameRegex.MatchString(config.App.Name) {
		return fmt.Errorf("el campo 'name' solo permite letras, números y guiones, encontrado: %q", config.App.Name)
	}

	// Versión: formato semver básico
	semverRegex := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	if !semverRegex.MatchString(config.App.Version) {
		return fmt.Errorf("el campo 'version' tiene que ser semver (ej: 1.0.0), encontrado: %q", config.App.Version)
	}

	// Runtime must have a kernel profile and rootfs recipe.
	validRuntimes := map[string]bool{"node": true, "python": true, "go": true, "bun": true, "rust": true}
	if !validRuntimes[config.App.Runtime] {
		return fmt.Errorf("unsupported runtime %q (supported: node, python, go, rust, bun)", config.App.Runtime)
	}
	entry := strings.ReplaceAll(config.App.Entry, "\\", "/")
	cleanEntry := path.Clean(entry)
	if !entryPattern.MatchString(entry) {
		return fmt.Errorf("app.entry contains unsupported characters: %q", config.App.Entry)
	}
	if entry == "" || strings.ContainsAny(entry, "\r\n\x00") || path.IsAbs(entry) || cleanEntry == ".." || strings.HasPrefix(cleanEntry, "../") {
		return fmt.Errorf("app.entry must be a non-empty relative path inside the project, got %q", config.App.Entry)
	}
	seenPorts := make(map[int]struct{}, len(config.Network.Ports))
	for _, port := range config.Network.Ports {
		if port < 1 || port > 65535 {
			return fmt.Errorf("network port must be between 1 and 65535, got %d", port)
		}
		if _, exists := seenPorts[port]; exists {
			return fmt.Errorf("network port %d is configured more than once", port)
		}
		seenPorts[port] = struct{}{}
	}
	if config.Resources.CPU < 0 {
		return fmt.Errorf("resources.cpu cannot be negative")
	}
	if config.Resources.Memory != "" {
		if _, err := ParseMemoryMiB(config.Resources.Memory); err != nil {
			return fmt.Errorf("invalid resources.memory: %w", err)
		}
	}
	for name := range config.Env {
		if !envNamePattern.MatchString(name) {
			return fmt.Errorf("invalid environment variable name %q", name)
		}
	}

	return nil
}

var memoryPattern = regexp.MustCompile(`(?i)^([0-9]+)(b|kb|kib|mb|mib|gb|gib|mi|gi)?$`)
var entryPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseMemoryMiB converts a Kernfile memory value to whole MiB.
func ParseMemoryMiB(value string) (int, error) {
	match := memoryPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, fmt.Errorf("expected a size such as 256Mi, 512mb, or 1Gi")
	}
	amount, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || amount <= 0 {
		return 0, fmt.Errorf("memory size must be a positive integer")
	}
	factor := int64(1 << 20) // Unqualified values are MiB.
	switch strings.ToLower(match[2]) {
	case "b":
		factor = 1
	case "kb", "kib":
		factor = 1 << 10
	case "", "mb", "mib", "mi":
		factor = 1 << 20
	case "gb", "gib", "gi":
		factor = 1 << 30
	}
	if amount > math.MaxInt64/factor {
		return 0, fmt.Errorf("memory size is too large")
	}
	bytes := amount * factor
	if bytes > math.MaxInt64-(1<<20)+1 {
		return 0, fmt.Errorf("memory size is too large")
	}
	mib := (bytes + (1 << 20) - 1) / (1 << 20)
	if mib > int64(math.MaxInt) {
		return 0, fmt.Errorf("memory size is too large")
	}
	return int(mib), nil
}

func ResolveResources(config *KernfileConfig, profile *RuntimeProfile) (memoryMiB, cpus int, err error) {
	if config == nil || profile == nil {
		return 0, 0, fmt.Errorf("Kernfile and runtime profile are required")
	}
	memory := config.Resources.Memory
	if memory == "" {
		memory = profile.Memory
	}
	memoryMiB, err = ParseMemoryMiB(memory)
	if err != nil {
		return 0, 0, err
	}
	cpus = config.Resources.CPU
	if cpus == 0 {
		cpus = profile.CPU
	}
	if cpus <= 0 {
		return 0, 0, fmt.Errorf("CPU count must be greater than zero")
	}
	return memoryMiB, cpus, nil
}
