package instances

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Instance struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Runtime   string    `json:"runtime"`
	PID       int       `json:"pid"`
	Ports     []int     `json:"ports"`
	MemoryMiB int       `json:"memory_mib"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
	LogPath   string    `json:"log_path,omitempty"`
}

func instancesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "kern", "instances")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func Save(instance *Instance) error {
	dir, err := instancesDir()
	if err != nil {
		return fmt.Errorf("error accessing instances directory: %w", err)
	}

	data, err := json.MarshalIndent(instance, "", "  ")
	if err != nil {
		return fmt.Errorf("error serializing instance: %w", err)
	}

	path := filepath.Join(dir, instance.ID+".json")
	return os.WriteFile(path, data, 0644)
}

func List() ([]*Instance, error) {
	dir, err := instancesDir()
	if err != nil {
		return nil, fmt.Errorf("error accessing instances directory: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("error reading instances directory: %w", err)
	}

	var instances []*Instance
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var instance Instance
		if err := json.Unmarshal(data, &instance); err != nil {
			continue
		}

		if isRunning(instance.PID) {
			instance.Status = "running"
		} else {
			instance.Status = "stopped"
		}

		instances = append(instances, &instance)
	}
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Status != instances[j].Status {
			return instances[i].Status == "running"
		}
		return instances[i].CreatedAt.After(instances[j].CreatedAt)
	})

	return instances, nil
}

func Remove(id string) error {
	dir, err := instancesDir()
	if err != nil {
		return fmt.Errorf("error accessing instances directory: %w", err)
	}

	path := filepath.Join(dir, id+".json")
	return os.Remove(path)
}

func GetByName(name string) (*Instance, error) {
	list, err := List()
	if err != nil {
		return nil, err
	}

	for _, i := range list {
		if i.Name == name {
			return i, nil
		}
	}

	return nil, fmt.Errorf("instance %q not found", name)
}

func FindQEMUPid() (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}

	runtimeDir := filepath.Join(home, ".local", "share", "kraftkit", "runtime")
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		return 0, fmt.Errorf("kraftkit runtime directory not found: %w", err)
	}

	var latestPid int
	var latestTime int64

	for _, entry := range entries {
		pidFile := filepath.Join(runtimeDir, entry.Name(), "machine.pid")
		info, err := os.Stat(pidFile)
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > latestTime {
			latestTime = info.ModTime().Unix()
			data, err := os.ReadFile(pidFile)
			if err != nil {
				continue
			}
			var pid int
			fmt.Sscanf(string(data), "%d", &pid)
			latestPid = pid
		}
	}

	if latestPid == 0 {
		return 0, fmt.Errorf("no QEMU process found")
	}

	return latestPid, nil
}

func isRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	command := strings.ReplaceAll(string(cmdline), "\x00", " ")
	return strings.Contains(command, "qemu-system-") || strings.Contains(command, "qemu-kvm")
}
