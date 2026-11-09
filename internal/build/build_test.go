package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateDockerfileRefreshesLegacyNodeTemplate(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "Dockerfile"), []byte(legacyNodeDockerfile("index.js")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := generateDockerfile("node", "index.js", project); err != nil {
		t.Fatal(err)
	}

	dockerfile, err := os.ReadFile(filepath.Join(project, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"COPY package*.json ./",
		"npm install --omit=dev --no-audit --no-fund && mkdir -p node_modules",
	} {
		if !strings.Contains(string(dockerfile), want) {
			t.Errorf("generated Dockerfile missing %q", want)
		}
	}

	ignore, err := os.ReadFile(filepath.Join(project, ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "node_modules") {
		t.Fatal("generated .dockerignore must exclude host node_modules")
	}
}

func TestGenerateDockerfilePreservesCustomNodeDockerfile(t *testing.T) {
	project := t.TempDir()
	want := []byte("FROM custom-node-image\n")
	if err := os.WriteFile(filepath.Join(project, "Dockerfile"), want, 0644); err != nil {
		t.Fatal(err)
	}
	if err := generateDockerfile("node", "index.js", project); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(project, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("custom Dockerfile changed: got %q, want %q", got, want)
	}
}
