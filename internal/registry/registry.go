package registry

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

type Credentials struct {
	Username string
	Password string
}

func PushImage(imagePath, reference string, creds Credentials) error {
	ref, err := name.ParseReference(reference)
	if err != nil {
		return fmt.Errorf("invalid image reference %q: %w", reference, err)
	}

	// Crear una capa OCI desde el archivo .uki
	layer, err := tarball.LayerFromFile(imagePath)
	if err != nil {
		return fmt.Errorf("error creating layer from file: %w", err)
	}

	// Añadir la capa a una imagen OCI vacía
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return fmt.Errorf("error creating image: %w", err)
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: creds.Username,
		Password: creds.Password,
	})

	if err := remote.Write(ref, img, remote.WithAuth(auth)); err != nil {
		return fmt.Errorf("error pushing image: %w", err)
	}

	fmt.Printf("✓ Image pushed to %s\n", reference)
	return nil
}

func PullImage(reference, destPath string, creds Credentials) error {
	ref, err := name.ParseReference(reference)
	if err != nil {
		return fmt.Errorf("invalid image reference %q: %w", reference, err)
	}

	auth := authn.FromConfig(authn.AuthConfig{
		Username: creds.Username,
		Password: creds.Password,
	})

	img, err := remote.Image(ref, remote.WithAuth(auth))
	if err != nil {
		return fmt.Errorf("error pulling image: %w", err)
	}

	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("error creating destination directory: %w", err)
	}

	ukiPath := filepath.Join(destPath, "image.uki")
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("error reading image layers: %w", err)
	}
	if len(layers) != 1 {
		return fmt.Errorf("unsupported Kern image: expected one artifact layer, got %d", len(layers))
	}
	reader, err := layers[0].Uncompressed()
	if err != nil {
		return fmt.Errorf("error opening image artifact layer: %w", err)
	}
	defer reader.Close()
	if err := extractUKI(reader, ukiPath); err != nil {
		return err
	}

	fmt.Printf("✓ Image pulled to %s\n", ukiPath)
	return nil
}

func extractUKI(reader io.Reader, ukiPath string) error {
	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return fmt.Errorf("image does not contain a Kern image.uki artifact")
		}
		if err != nil {
			return fmt.Errorf("error reading image artifact: %w", err)
		}
		if header.Name != "image.uki" || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size <= 0 {
			return fmt.Errorf("image artifact is empty")
		}
		file, err := os.OpenFile(ukiPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("error creating image artifact: %w", err)
		}
		_, copyErr := io.CopyN(file, archive, header.Size)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("error extracting image artifact: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("error closing image artifact: %w", closeErr)
		}
		return nil
	}
}
