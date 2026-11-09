package kernfile

import (
	"os"
	"testing"
)

func escribirKernfile(t *testing.T, contenido string) string {
	t.Helper()
	f, err := os.CreateTemp("", "Kernfile-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(contenido)
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestParseKernfileValido(t *testing.T) {
	path := escribirKernfile(t, `
kernfile = "1.0"
[app]
name    = "mi-api"
version = "1.0.0"
runtime = "node"
entry   = "src/index.js"
[network]
ports = [8080]
[resources]
memory = "256mb"
cpu    = 1
`)
	config, err := ParseKernfile(path)
	if err != nil {
		t.Fatalf("no esperaba error, got: %v", err)
	}
	if config.App.Name != "mi-api" {
		t.Errorf("name esperado 'mi-api', got %q", config.App.Name)
	}
}

func TestNombreInvalido(t *testing.T) {
	path := escribirKernfile(t, `
kernfile = "1.0"
[app]
name    = "mi api!!"
version = "1.0.0"
runtime = "node"
entry   = "src/index.js"
`)
	_, err := ParseKernfile(path)
	if err == nil {
		t.Fatal("esperaba error por nombre inválido")
	}
}

func TestVersionInvalida(t *testing.T) {
	path := escribirKernfile(t, `
kernfile = "1.0"
[app]
name    = "mi-api"
version = "1.0"
runtime = "node"
entry   = "src/index.js"
`)
	_, err := ParseKernfile(path)
	if err == nil {
		t.Fatal("esperaba error por versión inválida")
	}
}

func TestRuntimeInvalido(t *testing.T) {
	path := escribirKernfile(t, `
kernfile = "1.0"
[app]
name    = "mi-api"
version = "1.0.0"
runtime = "java"
entry   = "src/index.js"
`)
	_, err := ParseKernfile(path)
	if err == nil {
		t.Fatal("esperaba error por runtime inválido")
	}
}

func TestRuntimesValidos(t *testing.T) {
	for _, runtime := range []string{"node", "python", "go", "rust", "bun"} {
		path := escribirKernfile(t, `
kernfile = "1.0"
[app]
name    = "mi-api"
version = "1.0.0"
runtime = "`+runtime+`"
entry   = "src/index.js"
`)
		_, err := ParseKernfile(path)
		if err != nil {
			t.Errorf("runtime %q debería ser válido, got: %v", runtime, err)
		}
	}
}

func TestParseMemoryMiB(t *testing.T) {
	for input, want := range map[string]int{
		"256mb": 256,
		"512Mi": 512,
		"1Gi":   1024,
		"1024":  1024,
		"1b":    1,
	} {
		got, err := ParseMemoryMiB(input)
		if err != nil || got != want {
			t.Errorf("ParseMemoryMiB(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"", "0Mi", "-1Mi", "abc", "999999999999999999999Gi"} {
		if _, err := ParseMemoryMiB(input); err == nil {
			t.Errorf("ParseMemoryMiB(%q) should fail", input)
		}
	}
}

func TestRejectsUnsafeEntryAndInvalidPorts(t *testing.T) {
	for _, tc := range []struct {
		entry string
		ports string
	}{
		{entry: "../outside.js", ports: "8080"},
		{entry: "index.js", ports: "0"},
		{entry: "index.js", ports: "8080, 8080"},
	} {
		content := `
kernfile = "1.0"
[app]
name = "sample"
version = "1.0.0"
runtime = "node"
entry = "` + tc.entry + `"
[network]
ports = [` + tc.ports + `]
`
		if _, err := ParseKernfile(escribirKernfile(t, content)); err == nil {
			t.Errorf("Kernfile with entry=%q and ports=[%s] should be rejected", tc.entry, tc.ports)
		}
	}
}
