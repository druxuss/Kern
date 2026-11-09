package runner

import (
	"strings"
	"testing"
)

func TestDetectHypervisor(t *testing.T) {
	h := DetectHypervisor()
	if h != HypervisorKVM && h != HypervisorQEMU {
		t.Errorf("hypervisor desconocido: %s", h)
	}
	t.Logf("hypervisor detectado: %s", h)
}

func TestRunUnikernelKernelVacio(t *testing.T) {
	err := RunUnikernel(RunConfig{
		KernelPath: "",
		InitrdPath: "/tmp/test.cpio",
		MemoryMiB:  512,
		CPUs:       1,
	})
	if err == nil {
		t.Error("debería fallar con kernel vacío")
	}
}

func TestRunUnikernelMemoriaCero(t *testing.T) {
	err := RunUnikernel(RunConfig{
		KernelPath: "/tmp/test.kernel",
		InitrdPath: "/tmp/test.cpio",
		MemoryMiB:  0,
		CPUs:       1,
	})
	if err == nil {
		t.Error("debería fallar con memoria 0")
	}
}

func TestBuildQEMUArgsKVM(t *testing.T) {
	cfg := RunConfig{
		KernelPath: "/tmp/test.kernel",
		InitrdPath: "/tmp/test.cpio",
		MemoryMiB:  512,
		CPUs:       1,
		Hypervisor: HypervisorKVM,
		AppCmd:     "/usr/bin/node /usr/src/index.js",
	}
	args := BuildQEMUArgs(cfg)
	hasKVM := false
	hasTCG := false
	for _, a := range args {
		if a == "kvm" {
			hasKVM = true
		}
		if a == "tcg" {
			hasTCG = true
		}
	}
	if !hasKVM || hasTCG {
		t.Error("KVM configuration must enable KVM without TCG")
	}
}

func TestBuildQEMUArgsEmulationUsesTCG(t *testing.T) {
	args := BuildQEMUArgs(RunConfig{KernelPath: "kernel", InitrdPath: "initrd", MemoryMiB: 128, CPUs: 1})
	hasTCG := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-accel" && args[i+1] == "tcg" {
			hasTCG = true
		}
	}
	if !hasTCG {
		t.Fatal("emulation must explicitly enable TCG")
	}
}

func TestBuildQEMUArgsPuertos(t *testing.T) {
	cfg := RunConfig{
		KernelPath: "/tmp/test.kernel",
		InitrdPath: "/tmp/test.cpio",
		MemoryMiB:  512,
		CPUs:       1,
		AppCmd:     "/usr/bin/node /usr/src/index.js",
		Ports: []PortMapping{
			{Host: 8080, Container: 8080},
		},
	}
	args := BuildQEMUArgs(cfg)
	tieneNetdev := false
	for _, a := range args {
		if a == "-netdev" {
			tieneNetdev = true
		}
	}
	if !tieneNetdev {
		t.Error("debería incluir -netdev cuando hay puertos")
	}
}

func TestBuildQEMUArgsEscapesAndOrdersEnvironment(t *testing.T) {
	args := BuildQEMUArgs(RunConfig{
		KernelPath: "kernel",
		InitrdPath: "initrd",
		MemoryMiB:  128,
		CPUs:       1,
		AppCmd:     "/app",
		Env: map[string]string{
			"Z_VALUE": "last",
			"A_VALUE": `quote"and\slash`,
		},
	})
	var appendValue string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-append" {
			appendValue = args[i+1]
			break
		}
	}
	aIndex := strings.Index(appendValue, `"A_VALUE=quote\"and\\slash"`)
	zIndex := strings.Index(appendValue, `"Z_VALUE=last"`)
	if aIndex == -1 || zIndex == -1 || aIndex > zIndex {
		t.Fatalf("environment variables are missing, unescaped, or nondeterministic: %s", appendValue)
	}
	if strings.Contains(appendValue, `"A_VALUE=quote\"and\\slash",`) {
		t.Fatalf("Unikraft array items must be separated by whitespace, not commas: %s", appendValue)
	}
}
