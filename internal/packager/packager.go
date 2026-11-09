package packager

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type ImageMetadata struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Runtime   string    `json:"runtime"`
	BuildTime time.Time `json:"build_time"`
	Size      int64     `json:"size"`
}

type SBOM struct {
	SPDXVersion       string           `json:"spdxVersion"`
	DataLicense       string           `json:"dataLicense"`
	SPDXID            string           `json:"SPDXID"`
	Name              string           `json:"name"`
	DocumentNamespace string           `json:"documentNamespace"`
	CreationInfo      SPDXCreationInfo `json:"creationInfo"`
	DocumentDescribes []string         `json:"documentDescribes"`
	Packages          []SPDXPackage    `json:"packages"`
}

type Component struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type SPDXCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type SPDXPackage struct {
	Name             string `json:"name"`
	SPDXID           string `json:"SPDXID"`
	VersionInfo      string `json:"versionInfo"`
	DownloadLocation string `json:"downloadLocation"`
	FilesAnalyzed    bool   `json:"filesAnalyzed"`
	LicenseConcluded string `json:"licenseConcluded"`
	LicenseDeclared  string `json:"licenseDeclared"`
	CopyrightText    string `json:"copyrightText"`
}

func GenerateKeyPair(configDir string) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	keyDir := filepath.Join(configDir, "keys")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return nil, nil, fmt.Errorf("error creating key directory: %w", err)
	}
	if err := os.Chmod(keyDir, 0700); err != nil {
		return nil, nil, fmt.Errorf("error protecting key directory: %w", err)
	}

	pubKeyPath := filepath.Join(keyDir, "kern.pub")
	privKeyPath := filepath.Join(keyDir, "kern.key")

	if _, err := os.Stat(privKeyPath); err == nil {
		privBytes, err := os.ReadFile(privKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("error reading private key: %w", err)
		}
		pubBytes, err := os.ReadFile(pubKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("error reading public key: %w", err)
		}
		if len(privBytes) != ed25519.PrivateKeySize || len(pubBytes) != ed25519.PublicKeySize {
			return nil, nil, fmt.Errorf("stored signing key has an invalid length")
		}
		if err := os.Chmod(privKeyPath, 0600); err != nil {
			return nil, nil, fmt.Errorf("error protecting private key: %w", err)
		}
		return ed25519.PublicKey(pubBytes), ed25519.PrivateKey(privBytes), nil
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("error generating key pair: %w", err)
	}

	if err := os.WriteFile(privKeyPath, privKey, 0600); err != nil {
		return nil, nil, fmt.Errorf("error saving private key: %w", err)
	}
	if err := os.WriteFile(pubKeyPath, pubKey, 0644); err != nil {
		return nil, nil, fmt.Errorf("error saving public key: %w", err)
	}

	return pubKey, privKey, nil
}

func SignImage(imagePath string, privKey ed25519.PrivateKey) error {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("error reading image: %w", err)
	}

	signature := ed25519.Sign(privKey, data)

	sigPath := imagePath + ".sig"
	if err := os.WriteFile(sigPath, signature, 0644); err != nil {
		return fmt.Errorf("error writing signature: %w", err)
	}

	return nil
}

func VerifySignature(imagePath string, pubKey ed25519.PublicKey) error {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("error reading image: %w", err)
	}

	sigPath := imagePath + ".sig"
	signature, err := os.ReadFile(sigPath)
	if err != nil {
		return fmt.Errorf("error reading signature: %w", err)
	}

	if !ed25519.Verify(pubKey, data, signature) {
		return fmt.Errorf("invalid signature — image may have been tampered with")
	}

	return nil
}

func GenerateSBOM(name, version, runtime string, components []Component) *SBOM {
	created := time.Now().UTC()
	namespaceID := make([]byte, 16)
	if _, err := rand.Read(namespaceID); err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s", name, version, runtime, created.Format(time.RFC3339Nano))))
		copy(namespaceID, sum[:16])
	}
	namespaceID[6] = (namespaceID[6] & 0x0f) | 0x40
	namespaceID[8] = (namespaceID[8] & 0x3f) | 0x80
	format := hex.EncodeToString(namespaceID)
	documentNamespace := fmt.Sprintf("https://kern.invalid/spdx/%s-%s-%s", format[:8], format[8:12], format[12:])
	packages := []SPDXPackage{newSPDXPackage(name, "SPDXRef-Application", version)}
	for i, component := range components {
		packages = append(packages, newSPDXPackage(component.Name, fmt.Sprintf("SPDXRef-Component-%d", i+1), component.Version))
	}
	return &SBOM{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              name,
		DocumentNamespace: documentNamespace,
		CreationInfo: SPDXCreationInfo{
			Created:  created.Format("2006-01-02T15:04:05Z"),
			Creators: []string{"Tool: Kern"},
		},
		DocumentDescribes: []string{"SPDXRef-Application"},
		Packages:          packages,
	}
}

func newSPDXPackage(name, spdxID, version string) SPDXPackage {
	if version == "" {
		version = "NOASSERTION"
	}
	return SPDXPackage{
		Name:             name,
		SPDXID:           spdxID,
		VersionInfo:      version,
		DownloadLocation: "NOASSERTION",
		FilesAnalyzed:    false,
		LicenseConcluded: "NOASSERTION",
		LicenseDeclared:  "NOASSERTION",
		CopyrightText:    "NOASSERTION",
	}
}

func SaveSBOM(sbom *SBOM, outputPath string) error {
	data, err := json.MarshalIndent(sbom, "", "  ")
	if err != nil {
		return fmt.Errorf("error serializing SBOM: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("error saving SBOM: %w", err)
	}

	return nil
}

func GetImageMetadata(name, version, runtime, imagePath string) (*ImageMetadata, error) {
	info, err := os.Stat(imagePath)
	if err != nil {
		return nil, fmt.Errorf("error reading image: %w", err)
	}

	return &ImageMetadata{
		Name:      name,
		Version:   version,
		Runtime:   runtime,
		BuildTime: time.Now(),
		Size:      info.Size(),
	}, nil
}
