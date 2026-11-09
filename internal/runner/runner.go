package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type HypervisorType string

const (
	HypervisorKVM  HypervisorType = "kvm"
	HypervisorHVF  HypervisorType = "hvf"
	HypervisorQEMU HypervisorType = "qemu-tcg"
)

func DetectHypervisor() HypervisorType {
	// macOS con HVF
	if runtime.GOOS == "darwin" {
		return HypervisorHVF
	}
	// Linux con KVM
	if device, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0); err == nil {
		device.Close()
		return HypervisorKVM
	}
	return HypervisorQEMU
}

type RunConfig struct {
	KernelPath    string
	InitrdPath    string
	MemoryMiB     int
	CPUs          int
	Ports         []PortMapping
	Hypervisor    HypervisorType
	AppCmd        string
	Env           map[string]string
	TAPInterface  string
	MACAddress    string
	SerialLogPath string
}

type PortMapping struct {
	Host      int
	Container int
}

func FindUnikraftPaths(projectPath string) (kernelPath, initrdPath string, err error) {
	initrdPath = filepath.Join(projectPath, ".unikraft", "build", "initramfs-x86_64.cpio")
	if _, err := os.Stat(initrdPath); err != nil {
		return "", "", fmt.Errorf("initrd not found — run 'kern build' first")
	}

	// Primero buscar el kernel en el proyecto
	localKernel := filepath.Join(projectPath, ".unikraft", "build", "kernel")
	if _, err := os.Stat(localKernel); err == nil {
		return localKernel, initrdPath, nil
	}

	// Fallback: buscar en kraftkit
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("error getting home directory: %w", err)
	}

	runtimeDir := filepath.Join(home, ".local", "share", "kraftkit", "runtime")
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		return "", "", fmt.Errorf("kraftkit runtime directory not found: %w", err)
	}

	var latestKernel string
	var latestTime int64

	for _, entry := range entries {
		candidate := filepath.Join(runtimeDir, entry.Name(), "unikraft", "bin", "kernel")
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > latestTime {
			latestTime = info.ModTime().Unix()
			latestKernel = candidate
		}
	}

	if latestKernel == "" {
		return "", "", fmt.Errorf("kernel not found — run 'kern build' first")
	}

	return latestKernel, initrdPath, nil
}

func RunUnikernel(cfg RunConfig) error {
	if cfg.KernelPath == "" {
		return fmt.Errorf("kernel path cannot be empty")
	}
	if cfg.MemoryMiB <= 0 {
		return fmt.Errorf("memory must be greater than 0")
	}

	args := BuildQEMUArgs(cfg)
	cmd := exec.Command("qemu-system-x86_64", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func BuildQEMUArgs(cfg RunConfig) []string {
	// Construir env.vars — el PATH siempre primero
	envList := []string{strconv.Quote("PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")}
	envNames := make([]string, 0, len(cfg.Env))
	for name := range cfg.Env {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)
	for _, name := range envNames {
		envList = append(envList, strconv.Quote(name+"="+cfg.Env[name]))
	}
	// uklibparam parses array elements as whitespace-separated quoted values;
	// comma separators prevent later variables from reaching posix-environ.
	envVars := strings.Join(envList, " ")
	appendStr := fmt.Sprintf(`vfs.fstab=[ "initrd0:/:extract:::" ] env.vars=[ %s ] -- %s`, envVars, cfg.AppCmd)
	machine := "pc"
	if cfg.Hypervisor == HypervisorHVF {
		machine = "q35"
	}

	// Crear directorio para el socket
	socketDir := fmt.Sprintf("/tmp/kern-%d-%d", os.Getpid(), time.Now().UnixNano())
	os.MkdirAll(socketDir, 0755)
	qmpSocket := filepath.Join(socketDir, "ctrl.sock")
	pidFile := filepath.Join(socketDir, "machine.pid")

	args := []string{
		"-kernel", cfg.KernelPath,
		"-initrd", cfg.InitrdPath,
		"-append", appendStr,
		"-m", fmt.Sprintf("size=%dM", cfg.MemoryMiB),
		"-smp", fmt.Sprintf("cpus=%d,threads=1,sockets=1", cfg.CPUs),
		"-machine", machine,
		"-monitor", "none",
		"-no-reboot",
		"-device", "pvpanic",
		"-display", "none",
		"-vga", "none",
		"-parallel", "none",
		"-rtc", "base=utc",
		"-qmp", fmt.Sprintf("unix:%s,server,nowait", qmpSocket),
		"-pidfile", pidFile,
	}
	if cfg.SerialLogPath == "" {
		args = append(args, "-serial", "stdio")
	} else {
		args = append(args, "-serial", "file:"+cfg.SerialLogPath)
	}

	if cfg.Hypervisor == HypervisorKVM {
		args = append(args, "-accel", "kvm", "-cpu", "host,+x2apic,-pmu")
	} else if cfg.Hypervisor == HypervisorHVF {
		args = append(args, "-accel", "hvf", "-cpu", "host")
	} else {
		args = append(args, "-accel", "tcg", "-cpu", "max")
	}
	// Networking — TAP para compose o user para run normal
	if cfg.TAPInterface != "" {
		args = append(args,
			"-netdev", fmt.Sprintf("tap,id=hostnet0,ifname=%s,script=no,downscript=no", cfg.TAPInterface),
			"-device", fmt.Sprintf("virtio-net-pci,netdev=hostnet0,mac=%s", cfg.MACAddress),
		)
	} else {
		netdev := "user,id=hostnet0"
		for _, p := range cfg.Ports {
			netdev += fmt.Sprintf(",hostfwd=tcp::%d-:%d", p.Host, p.Container)
		}
		args = append(args,
			"-netdev", netdev,
			"-device", "virtio-net-pci,netdev=hostnet0",
		)
	}
	return args
}
