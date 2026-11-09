package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateRustKernfileUsesCargoBinaryName(t *testing.T) {
	project := t.TempDir()
	manifest := `[package]
name = "workspace-package"
version = "0.1.0"

[[bin]]
name = "smoke-server"
path = "src/main.rs"
`
	if err := os.WriteFile(filepath.Join(project, "Cargo.toml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := createKernfile(project, "rust"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(project, "Kernfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `name    = "smoke-server"`) {
		t.Fatalf("Kernfile did not use Cargo binary name: %s", data)
	}
}
