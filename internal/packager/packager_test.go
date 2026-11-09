package packager

import (
	"os"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	dir := t.TempDir()

	pub, priv, err := GenerateKeyPair(dir)
	if err != nil {
		t.Fatalf("error generating key pair: %v", err)
	}
	if len(pub) == 0 {
		t.Error("public key should not be empty")
	}
	if len(priv) == 0 {
		t.Error("private key should not be empty")
	}

	// Segunda llamada debe devolver las mismas claves
	pub2, priv2, err := GenerateKeyPair(dir)
	if err != nil {
		t.Fatalf("error loading existing key pair: %v", err)
	}
	if string(pub) != string(pub2) {
		t.Error("public key should be the same on second call")
	}
	if string(priv) != string(priv2) {
		t.Error("private key should be the same on second call")
	}
}

func TestSignAndVerify(t *testing.T) {
	dir := t.TempDir()

	// Crear imagen de prueba
	imagePath := dir + "/test.uki"
	if err := os.WriteFile(imagePath, []byte("fake unikernel image"), 0644); err != nil {
		t.Fatal(err)
	}

	pub, priv, err := GenerateKeyPair(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Firmar
	if err := SignImage(imagePath, priv); err != nil {
		t.Fatalf("error signing image: %v", err)
	}

	// Verificar
	if err := VerifySignature(imagePath, pub); err != nil {
		t.Fatalf("error verifying signature: %v", err)
	}
}

func TestVerifyTamperedImage(t *testing.T) {
	dir := t.TempDir()

	imagePath := dir + "/test.uki"
	if err := os.WriteFile(imagePath, []byte("fake unikernel image"), 0644); err != nil {
		t.Fatal(err)
	}

	pub, priv, err := GenerateKeyPair(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := SignImage(imagePath, priv); err != nil {
		t.Fatal(err)
	}

	// Modificar la imagen después de firmar
	if err := os.WriteFile(imagePath, []byte("tampered image"), 0644); err != nil {
		t.Fatal(err)
	}

	// La verificación debe fallar
	if err := VerifySignature(imagePath, pub); err == nil {
		t.Error("verification should fail for tampered image")
	}
}

func TestGenerateSBOM(t *testing.T) {
	components := []Component{
		{Name: "unikraft", Version: "0.21.0"},
		{Name: "node", Version: "runtime"},
	}

	sbom := GenerateSBOM("my-app", "1.0.0", "node", components)

	if sbom.SPDXVersion != "SPDX-2.3" {
		t.Errorf("expected SPDX-2.3, got %s", sbom.SPDXVersion)
	}
	if sbom.Name != "my-app" {
		t.Errorf("expected my-app, got %s", sbom.Name)
	}
	if len(sbom.Packages) != 3 {
		t.Errorf("expected application and 2 packages, got %d", len(sbom.Packages))
	}
	if sbom.SPDXID != "SPDXRef-DOCUMENT" || sbom.DataLicense != "CC0-1.0" || sbom.DocumentNamespace == "" {
		t.Fatal("SPDX document is missing required metadata")
	}
	for _, pkg := range sbom.Packages {
		if pkg.SPDXID == "" || pkg.DownloadLocation == "" || pkg.LicenseConcluded == "" || pkg.LicenseDeclared == "" || pkg.CopyrightText == "" {
			t.Errorf("SPDX package %q is missing required fields", pkg.Name)
		}
	}
}

func TestSaveSBOM(t *testing.T) {
	dir := t.TempDir()

	sbom := GenerateSBOM("my-app", "1.0.0", "node", []Component{
		{Name: "unikraft", Version: "0.21.0"},
	})

	sbomPath := dir + "/image.sbom"
	if err := SaveSBOM(sbom, sbomPath); err != nil {
		t.Fatalf("error saving SBOM: %v", err)
	}

	if _, err := os.Stat(sbomPath); err != nil {
		t.Error("SBOM file should exist")
	}
}
