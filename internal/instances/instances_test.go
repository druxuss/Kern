package instances

import (
	"runtime"
	"testing"
	"time"
)

func TestSaveAndList(t *testing.T) {
	setTestHome(t)

	instance := &Instance{
		ID:        "test123",
		Name:      "my-app",
		Runtime:   "node",
		PID:       12345,
		Ports:     []int{3000},
		MemoryMiB: 512,
		CreatedAt: time.Now(),
		Status:    "running",
	}

	if err := Save(instance); err != nil {
		t.Fatalf("error saving instance: %v", err)
	}

	list, err := List()
	if err != nil {
		t.Fatalf("error listing instances: %v", err)
	}

	if len(list) != 1 {
		t.Errorf("expected 1 instance, got %d", len(list))
	}

	if list[0].Name != "my-app" {
		t.Errorf("expected my-app, got %s", list[0].Name)
	}
}

func TestGetByName(t *testing.T) {
	setTestHome(t)

	instance := &Instance{
		ID:        "abc123",
		Name:      "my-api",
		Runtime:   "go",
		PID:       9999,
		Ports:     []int{8080},
		MemoryMiB: 64,
		CreatedAt: time.Now(),
		Status:    "running",
	}

	if err := Save(instance); err != nil {
		t.Fatal(err)
	}

	found, err := GetByName("my-api")
	if err != nil {
		t.Fatalf("error getting instance: %v", err)
	}

	if found.ID != "abc123" {
		t.Errorf("expected abc123, got %s", found.ID)
	}
}

func TestGetByNameNotFound(t *testing.T) {
	setTestHome(t)

	_, err := GetByName("nonexistent")
	if err == nil {
		t.Error("should return error for nonexistent instance")
	}
}

func TestRemove(t *testing.T) {
	setTestHome(t)

	instance := &Instance{
		ID:        "del123",
		Name:      "to-delete",
		Runtime:   "node",
		PID:       1111,
		Ports:     []int{3000},
		MemoryMiB: 512,
		CreatedAt: time.Now(),
		Status:    "stopped",
	}

	if err := Save(instance); err != nil {
		t.Fatal(err)
	}

	if err := Remove("del123"); err != nil {
		t.Fatalf("error removing instance: %v", err)
	}

	list, err := List()
	if err != nil {
		t.Fatal(err)
	}

	if len(list) != 0 {
		t.Errorf("expected 0 instances after remove, got %d", len(list))
	}
}

func TestIsRunning(t *testing.T) {
	// A live non-QEMU process must not be mistaken for a Kern instance.
	if runtime.GOOS == "linux" && isRunning(1) {
		t.Error("PID 1 must not be treated as a Kern QEMU instance")
	}

	// PID negativo nunca existe
	if isRunning(-1) {
		t.Error("negative PID should not be running")
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
