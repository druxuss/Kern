package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSetAndGetSecret(t *testing.T) {
	// Usar directorio temporal para los tests
	setTestHome(t)

	if err := SetSecret("db_url", "postgres://user:pass@localhost/db"); err != nil {
		t.Fatalf("error setting secret: %v", err)
	}

	value, err := GetSecret("db_url")
	if err != nil {
		t.Fatalf("error getting secret: %v", err)
	}

	if value != "postgres://user:pass@localhost/db" {
		t.Errorf("expected postgres://user:pass@localhost/db, got %s", value)
	}
}

func TestGetNonExistentSecret(t *testing.T) {
	setTestHome(t)

	_, err := GetSecret("nonexistent")
	if err == nil {
		t.Error("should return error for nonexistent secret")
	}
}

func TestSetSecretOverwrite(t *testing.T) {
	setTestHome(t)

	if err := SetSecret("key", "value1"); err != nil {
		t.Fatal(err)
	}
	if err := SetSecret("key", "value2"); err != nil {
		t.Fatal(err)
	}

	value, err := GetSecret("key")
	if err != nil {
		t.Fatal(err)
	}
	if value != "value2" {
		t.Errorf("expected value2, got %s", value)
	}
}

func TestResolveSecrets(t *testing.T) {
	setTestHome(t)

	if err := SetSecret("db_url", "postgres://localhost/db"); err != nil {
		t.Fatal(err)
	}

	envMap := map[string]string{
		"DATABASE_URL": "secret:db_url",
		"NODE_ENV":     "production",
	}

	resolved, err := ResolveSecrets(envMap)
	if err != nil {
		t.Fatalf("error resolving secrets: %v", err)
	}

	if resolved["DATABASE_URL"] != "postgres://localhost/db" {
		t.Errorf("expected postgres://localhost/db, got %s", resolved["DATABASE_URL"])
	}
	if resolved["NODE_ENV"] != "production" {
		t.Errorf("expected production, got %s", resolved["NODE_ENV"])
	}
}

func setTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
}

func TestResolveSecretsNonExistent(t *testing.T) {
	setTestHome(t)

	envMap := map[string]string{
		"DATABASE_URL": "secret:nonexistent",
	}

	_, err := ResolveSecrets(envMap)
	if err == nil {
		t.Error("should return error for nonexistent secret")
	}
}

func TestSecretIsEncryptedWithProtectedMasterKey(t *testing.T) {
	setTestHome(t)
	if err := SetSecret("api-token", "super-secret-value"); err != nil {
		t.Fatal(err)
	}
	dir, err := secretsDir()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(dir, "api-token.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "super-secret-value") || !strings.HasPrefix(string(stored), "v1:") {
		t.Fatal("secret is not stored in the versioned encrypted format")
	}
	key, err := os.ReadFile(filepath.Join(dir, "master.key"))
	if err != nil || len(key) != 32 {
		t.Fatalf("invalid master key: length=%d err=%v", len(key), err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "master.key"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("master key permissions = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestLegacySecretMigratesOnRead(t *testing.T) {
	setTestHome(t)
	dir, err := secretsDir()
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := encrypt(legacyKey("old-secret"), "legacy-value")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old-secret.enc"), []byte(strings.TrimPrefix(legacy, "v1:")), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := GetSecret("old-secret")
	if err != nil || got != "legacy-value" {
		t.Fatalf("GetSecret() = %q, %v", got, err)
	}
	updated, err := os.ReadFile(filepath.Join(dir, "old-secret.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(updated), "v1:") {
		t.Fatal("legacy secret was not migrated")
	}
}

func TestSecretNamesCannotEscapeStore(t *testing.T) {
	setTestHome(t)
	for _, name := range []string{"../outside", "nested/name", "", "."} {
		if err := SetSecret(name, "value"); err == nil {
			t.Errorf("SetSecret(%q) should reject the name", name)
		}
	}
}
