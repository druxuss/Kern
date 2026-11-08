package compose

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/druxuss/Kern/internal/instances"
	"github.com/druxuss/Kern/internal/kernfile"
	"github.com/druxuss/Kern/internal/network"
	"github.com/druxuss/Kern/internal/runner"
	"gopkg.in/yaml.v3"
)

type ComposeFile struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
	Env      map[string]string  `yaml:"env"`
}

type Service struct {
	Build     string            `yaml:"build"`
	Ports     []string          `yaml:"ports"`
	Env       map[string]string `yaml:"env"`
	DependsOn []string          `yaml:"depends_on"`
}

var memoryMap = map[string]int{
	"node": 512, "python": 384, "go": 256, "rust": 256, "bun": 256,
}

func Parse(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading kern-compose.yml: %w", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("error parsing kern-compose.yml: %w", err)
	}

	return &compose, nil
}

func Up(composePath string, detach bool) error {
	compose, err := Parse(composePath)
	if err != nil {
		return err
	}

	baseDir := filepath.Dir(composePath)

	ordered, err := sortServices(compose.Services)
	if err != nil {
		return err
	}

	// Configurar networking si hay más de un servicio
	var networks map[string]network.ServiceNet
	if len(compose.Services) > 1 {
		fmt.Println("→ Setting up network bridge...")
		if err := network.SetupBridge(); err != nil {
			return fmt.Errorf("error setting up bridge: %w", err)
		}

		networks = network.BuildServiceNetworks(ordered)

		for _, name := range ordered {
			svc := networks[name]
			fmt.Printf("  Creating TAP for %s (%s → %s)\n", name, svc.TAP, svc.IP)
			if err := network.CreateTAP(svc.TAP); err != nil {
				return fmt.Errorf("error creating TAP for %s: %w", name, err)
			}
		}

		if err := network.StartDNSMasq(networks); err != nil {
			return fmt.Errorf("error starting DHCP: %w", err)
		}

		time.Sleep(1 * time.Second)
		fmt.Println("✓ Network ready")
		fmt.Println()
		hostsEntries := "# kern-compose-start\n"
		for _, name := range ordered {
			svc := networks[name]
			hostsEntries += fmt.Sprintf("%s %s\n", svc.IP, name)
		}
		hostsEntries += "# kern-compose-end"
		exec.Command("sudo", "sh", "-c",
			fmt.Sprintf("printf '\\n%s\\n' >> /etc/hosts", hostsEntries)).Run()

		fmt.Println("Service IPs:")
		for _, name := range ordered {
			svc := networks[name]
			fmt.Printf("  %-15s %s\n", name, svc.IP)
		}
		fmt.Println()
	}

	for _, name := range ordered {
		service := compose.Services[name]
		fmt.Printf("Starting service: %s\n", name)

		serviceDir := filepath.Join(baseDir, service.Build)

		// kern init si no existe Kernfile
		if _, err := os.Stat(filepath.Join(serviceDir, "Kernfile")); err != nil {
			initCmd := exec.Command("kern", "init")
			initCmd.Dir = serviceDir
			initCmd.Stdout = os.Stdout
			initCmd.Stderr = os.Stderr
			if err := initCmd.Run(); err != nil {
				return fmt.Errorf("error initializing service %s: %w", name, err)
			}
		}

		// kern build
		buildCmd := exec.Command("kern", "build")
		buildCmd.Dir = serviceDir
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			return fmt.Errorf("error building service %s: %w", name, err)
		}

		// Parsear Kernfile para obtener config
		config, err := kernfile.ParseKernfile(filepath.Join(serviceDir, "Kernfile"))
		if err != nil {
			return fmt.Errorf("error reading Kernfile for %s: %w", name, err)
		}

		// Rutas del kernel e initrd
		kernelPath := filepath.Join(serviceDir, ".unikraft", "build", "kernel")
		initrdPath := filepath.Join(serviceDir, ".unikraft", "build", "initramfs-x86_64.cpio")

		cmdMap := map[string]string{
			"node":   "/usr/bin/node /usr/src/" + config.App.Entry,
			"python": "/usr/local/bin/python3.12 /src/" + config.App.Entry,
			"go":     "/usr/src/app",
			"rust":   "/usr/src/app",
			"bun":    "/usr/bin/bun run /usr/src/" + config.App.Entry,
		}

		cfg := runner.RunConfig{
			KernelPath: kernelPath,
			InitrdPath: initrdPath,
			MemoryMiB:  memoryMap[config.App.Runtime],
			CPUs:       1,
			Hypervisor: runner.DetectHypervisor(),
			AppCmd:     cmdMap[config.App.Runtime],
			Env:        config.Env,
		}

		// Usar TAP si hay networking
		if networks != nil {
			svc := networks[name]
			cfg.TAPInterface = svc.TAP
			cfg.MACAddress = svc.MAC
		} else {
			// User networking con port forwarding
			for _, p := range config.Network.Ports {
				cfg.Ports = append(cfg.Ports, runner.PortMapping{
					Host:      p,
					Container: p,
				})
			}
		}

		// Lanzar QEMU directamente
		args := runner.BuildQEMUArgs(cfg)

		// Crear directorio para socket y logs
		socketDir := fmt.Sprintf("/tmp/kern-compose-%s", name)
		os.MkdirAll(socketDir, 0755)

		// Añadir log file
		qemuCmd := exec.Command("qemu-system-x86_64", args...)
		qemuCmd.Dir = serviceDir
		qemuCmd.Stdout = os.Stdout
		qemuCmd.Stderr = os.Stderr

		if err := qemuCmd.Start(); err != nil {
			return fmt.Errorf("error starting %s: %w", name, err)
		}

		time.Sleep(2 * time.Second)

		// Esperar a que el servicio esté listo
		fmt.Printf("  Waiting for %s to be ready...\n", name)
		ready := false
		for i := 0; i < 30; i++ {
			time.Sleep(1 * time.Second)
			if len(config.Network.Ports) > 0 {
				port := config.Network.Ports[0]
				var url string
				if networks != nil {
					svc := networks[name]
					url = fmt.Sprintf("http://%s:%d", svc.IP, port)
				} else {
					url = fmt.Sprintf("http://localhost:%d", port)
				}
				resp, err := http.Get(url)
				if err == nil {
					resp.Body.Close()
					ready = true
					break
				}
			} else {
				// Sin puerto, asumir listo después de 3 segundos
				ready = true
				break
			}
		}

		if ready {
			fmt.Printf("✓ %s is ready\n", name)
		} else {
			fmt.Printf("warning: %s did not respond within 30 seconds\n", name)
		}

		// Guardar instancia
		instance := &instances.Instance{
			ID:        generateID(),
			Name:      name,
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

		fmt.Printf("✓ %s started (PID %d)\n", name, qemuCmd.Process.Pid)
	}

	return nil
}

func Down(composePath string) error {
	compose, err := Parse(composePath)
	if err != nil {
		return err
	}

	ordered, _ := sortServices(compose.Services)
	networks := network.BuildServiceNetworks(ordered)

	cleanup := func() {
		if len(compose.Services) > 1 {
			fmt.Println("→ Tearing down network...")
			for _, svc := range networks {
				network.DeleteTAP(svc.TAP)
			}
			network.TeardownBridge()
			exec.Command("sudo", "sh", "-c",
				"sed -i '/# kern-compose-start/,/# kern-compose-end/d' /etc/hosts").Run()
			fmt.Println("✓ Network removed")
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\n→ Interrupted — cleaning up...")
		cleanup()
		os.Exit(0)
	}()

	for name := range compose.Services {
		fmt.Printf("Stopping service: %s\n", name)

		stopCmd := exec.Command("kern", "stop", name)
		stopCmd.Stdout = os.Stdout
		stopCmd.Stderr = os.Stderr
		stopCmd.Run()

		rmCmd := exec.Command("kern", "rm", name)
		rmCmd.Stdout = os.Stdout
		rmCmd.Stderr = os.Stderr
		rmCmd.Run()
	}

	cleanup()
	return nil
}

func Ps(composePath string) error {
	compose, err := Parse(composePath)
	if err != nil {
		return err
	}

	fmt.Printf("%-15s %-10s\n", "SERVICE", "STATUS")
	fmt.Println("-------------------------")

	for name := range compose.Services {
		psCmd := exec.Command("kern", "ps")
		out, _ := psCmd.Output()
		status := "stopped"
		if len(out) > 0 {
			status = "running"
		}
		fmt.Printf("%-15s %-10s\n", name, status)
	}

	return nil
}

func Logs(composePath string, follow bool) error {
	compose, err := Parse(composePath)
	if err != nil {
		return err
	}

	for name := range compose.Services {
		fmt.Printf("=== logs: %s ===\n", name)
		logsArgs := []string{"logs"}
		if follow {
			logsArgs = append(logsArgs, "-f")
		}
		logsArgs = append(logsArgs, name)

		logsCmd := exec.Command("kern", logsArgs...)
		logsCmd.Stdout = os.Stdout
		logsCmd.Stderr = os.Stderr
		logsCmd.Run()
	}

	return nil
}

func Restart(composePath string, serviceName string) error {
	compose, err := Parse(composePath)
	if err != nil {
		return err
	}

	if _, ok := compose.Services[serviceName]; !ok {
		return fmt.Errorf("service %q not found in kern-compose.yml", serviceName)
	}

	fmt.Printf("Restarting service: %s\n", serviceName)

	stopCmd := exec.Command("kern", "stop", serviceName)
	stopCmd.Stdout = os.Stdout
	stopCmd.Stderr = os.Stderr
	stopCmd.Run()

	rmCmd := exec.Command("kern", "rm", serviceName)
	rmCmd.Stdout = os.Stdout
	rmCmd.Stderr = os.Stderr
	rmCmd.Run()

	baseDir := filepath.Dir(composePath)
	service := compose.Services[serviceName]
	serviceDir := filepath.Join(baseDir, service.Build)

	runCmd := exec.Command("kern", "run", "-d")
	runCmd.Dir = serviceDir
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		return fmt.Errorf("error restarting service %s: %w", serviceName, err)
	}

	fmt.Printf("✓ %s restarted\n", serviceName)
	return nil
}

func sortServices(services map[string]Service) ([]string, error) {
	visited := map[string]bool{}
	ordered := []string{}

	var visit func(name string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		visited[name] = true

		service, ok := services[name]
		if !ok {
			return fmt.Errorf("service %q not found", name)
		}

		for _, dep := range service.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}

		ordered = append(ordered, name)
		return nil
	}

	names := []string{}
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}

	return ordered, nil
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
