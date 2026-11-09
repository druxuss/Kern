package cli

import (
	"testing"

	"github.com/druxuss/Kern/internal/runner"
)

func TestEffectivePortsPreservesContainerPort(t *testing.T) {
	ports, hostPorts, err := effectivePorts([]int{3000}, []string{"8080:3000", "9000:9001"})
	if err != nil {
		t.Fatal(err)
	}
	want := []runner.PortMapping{{Host: 8080, Container: 3000}, {Host: 9000, Container: 9001}}
	if len(ports) != len(want) || len(hostPorts) != len(want) {
		t.Fatalf("got ports=%v hostPorts=%v", ports, hostPorts)
	}
	for i := range want {
		if ports[i] != want[i] || hostPorts[i] != want[i].Host {
			t.Errorf("mapping %d = %+v, host port %d; want %+v", i, ports[i], hostPorts[i], want[i])
		}
	}
}

func TestEffectivePortsRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"8080", "0:80", "80:65536", "abc:80"} {
		if _, _, err := effectivePorts(nil, []string{value}); err == nil {
			t.Errorf("mapping %q should be rejected", value)
		}
	}
}
