package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var secretNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func secretsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "kern", "secrets")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func validateName(name string) error {
	if !secretNamePattern.MatchString(name) || name == "." || name == ".." {
		return fmt.Errorf("invalid secret name %q; use 1-128 letters, digits, dots, underscores, or hyphens", name)
	}
	return nil
}

func legacyKey(name string) []byte {
	hash := sha256.Sum256([]byte("kern-secret-" + name))
	return hash[:]
}

func masterKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, "master.key")
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		key = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("generating secret-store key: %w", err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if !os.IsExist(err) {
				return nil, fmt.Errorf("creating secret-store key: %w", err)
			}
			key, err = os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("reading secret-store key: %w", err)
			}
		} else {
			if _, err := file.Write(key); err != nil {
				file.Close()
				return nil, fmt.Errorf("writing secret-store key: %w", err)
			}
			if err := file.Close(); err != nil {
				return nil, fmt.Errorf("closing secret-store key: %w", err)
			}
		}
	} else if err != nil {
		return nil, fmt.Errorf("reading secret-store key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid secret-store key length")
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, fmt.Errorf("protecting secret-store key: %w", err)
	}
	return key, nil
}

func encrypt(key []byte, value string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return "v1:" + hex.EncodeToString(sealed), nil
}

func decrypt(key []byte, data string) (string, error) {
	data = strings.TrimPrefix(data, "v1:")
	sealed, err := hex.DecodeString(data)
	if err != nil {
		return "", fmt.Errorf("decoding secret: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid secret data")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypting secret: %w", err)
	}
	return string(plaintext), nil
}

func saveEncrypted(path string, data string) error {
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func SetSecret(name, value string) error {
	if err := validateName(name); err != nil {
		return err
	}
	dir, err := secretsDir()
	if err != nil {
		return fmt.Errorf("error accessing secrets directory: %w", err)
	}

	key, err := masterKey(dir)
	if err != nil {
		return err
	}
	encrypted, err := encrypt(key, value)
	if err != nil {
		return err
	}
	secretPath := filepath.Join(dir, name+".enc")
	if err := saveEncrypted(secretPath, encrypted); err != nil {
		return fmt.Errorf("error saving secret: %w", err)
	}

	fmt.Printf("✓ Secret %q saved\n", name)
	return nil
}

func GetSecret(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := secretsDir()
	if err != nil {
		return "", fmt.Errorf("error accessing secrets directory: %w", err)
	}

	secretPath := filepath.Join(dir, name+".enc")
	data, err := os.ReadFile(secretPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("secret %q not found", name)
		}
		return "", fmt.Errorf("reading secret %q: %w", name, err)
	}
	if strings.HasPrefix(string(data), "v1:") {
		key, err := masterKey(dir)
		if err != nil {
			return "", err
		}
		return decrypt(key, string(data))
	}

	// Migrate the legacy name-derived key format the first time a secret is read.
	value, err := decrypt(legacyKey(name), string(data))
	if err != nil {
		return "", err
	}
	key, err := masterKey(dir)
	if err != nil {
		return "", err
	}
	encrypted, err := encrypt(key, value)
	if err != nil {
		return "", err
	}
	if err := saveEncrypted(secretPath, encrypted); err != nil {
		return "", fmt.Errorf("migrating secret to protected key storage: %w", err)
	}
	return value, nil
}

func DeleteSecret(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	dir, err := secretsDir()
	if err != nil {
		return fmt.Errorf("error accessing secrets directory: %w", err)
	}

	secretPath := filepath.Join(dir, name+".enc")

	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		return fmt.Errorf("secret %q not found", name)
	}

	if err := os.Remove(secretPath); err != nil {
		return fmt.Errorf("error deleting secret: %w", err)
	}

	fmt.Printf("✓ Secret %q deleted\n", name)
	return nil
}

func ResolveSecrets(envMap map[string]string) (map[string]string, error) {
	resolved := make(map[string]string)
	for k, v := range envMap {
		if strings.HasPrefix(v, "secret:") {
			secretName := strings.TrimPrefix(v, "secret:")
			value, err := GetSecret(secretName)
			if err != nil {
				return nil, fmt.Errorf("could not resolve secret %q: %w", secretName, err)
			}
			resolved[k] = value
		} else {
			resolved[k] = v
		}
	}
	return resolved, nil
}
