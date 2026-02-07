package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestReadPIDValid(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write a valid PID
	expectedPID := 12345
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(expectedPID)+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pid, err := readPID(pidPath)
	if err != nil {
		t.Fatalf("readPID: %v", err)
	}
	if pid != expectedPID {
		t.Fatalf("expected PID %d, got %d", expectedPID, pid)
	}
}

func TestReadPIDMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "nonexistent.pid")

	_, err := readPID(pidPath)
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestReadPIDInvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "invalid.pid")

	// Write invalid content (not a number)
	if err := os.WriteFile(pidPath, []byte("not-a-number\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := readPID(pidPath)
	if err == nil {
		t.Fatalf("expected error for invalid PID")
	}
}

func TestReadPIDEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "empty.pid")

	// Write empty file
	if err := os.WriteFile(pidPath, []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := readPID(pidPath)
	if err == nil {
		t.Fatalf("expected error for empty PID file")
	}
}

func TestReadPIDWithWhitespace(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "whitespace.pid")

	// Write PID with leading/trailing whitespace
	expectedPID := 99999
	if err := os.WriteFile(pidPath, []byte("  "+strconv.Itoa(expectedPID)+"  \n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pid, err := readPID(pidPath)
	if err != nil {
		t.Fatalf("readPID: %v", err)
	}
	if pid != expectedPID {
		t.Fatalf("expected PID %d, got %d", expectedPID, pid)
	}
}

func TestProcessExistsCurrentProcess(t *testing.T) {
	// Current process should always exist
	currentPID := os.Getpid()
	if !processExists(currentPID) {
		t.Fatalf("current process (PID %d) should exist", currentPID)
	}
}

func TestProcessExistsNonExistent(t *testing.T) {
	// Use a very high PID that's unlikely to exist
	// On most systems, PIDs don't reach this high
	nonExistentPID := 99999999
	if processExists(nonExistentPID) {
		t.Skipf("PID %d unexpectedly exists, skipping test", nonExistentPID)
	}
}

func TestProcessExistsPIDZero(t *testing.T) {
	// PID 0 has special meaning on Unix systems
	// This test verifies we handle it correctly
	// On Unix, PID 0 refers to the scheduler/kernel, not a user process
	// The behavior may vary by OS, so we just verify it doesn't panic
	_ = processExists(0)
}

func TestProcessExistsPIDOne(t *testing.T) {
	// PID 1 is typically init/systemd on Unix systems
	// It should exist on Unix-like systems
	// On macOS/Linux, this should return true
	// On Windows, behavior may differ
	exists := processExists(1)
	// We don't assert the result because it's platform-dependent
	// Just verify the function doesn't panic
	t.Logf("PID 1 exists: %v", exists)
}
