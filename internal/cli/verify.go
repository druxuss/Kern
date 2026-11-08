package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/druxuss/Kern/internal/packager"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify the integrity of a built unikernel image",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectPath, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not get current directory: %w", err)
		}

		unikernelDir := filepath.Join(projectPath, ".unikernel")
		imagePath := filepath.Join(unikernelDir, "image.uki")
		sigPath := filepath.Join(unikernelDir, "image.uki.sig")

		// Verificar que existen los archivos
		if _, err := os.Stat(imagePath); err != nil {
			return fmt.Errorf("image not found — run 'kern build' first")
		}
		if _, err := os.Stat(sigPath); err != nil {
			return fmt.Errorf("signature not found — run 'kern build' first")
		}

		// Cargar clave pública
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("error getting home directory: %w", err)
		}
		pubKeyPath := filepath.Join(home, ".config", "kern", "keys", "kern.pub")
		pubKey, err := os.ReadFile(pubKeyPath)
		if err != nil {
			return fmt.Errorf("public key not found — run 'kern build' first to generate keys")
		}

		fmt.Printf("Verifying %s...\n", imagePath)

		// Verificar firma
		if err := packager.VerifySignature(imagePath, pubKey); err != nil {
			fmt.Println("✗ Signature INVALID — image may have been tampered with")
			return err
		}

		fmt.Println("✓ Signature valid — ed25519")

		// Mostrar info del SBOM
		sbomPath := filepath.Join(unikernelDir, "image.sbom")
		if _, err := os.Stat(sbomPath); err == nil {
			fmt.Println("✓ SBOM present — SPDX-2.3")
		}

		fmt.Println("\nImage integrity verified successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}
