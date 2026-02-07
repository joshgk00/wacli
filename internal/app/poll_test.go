package app

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/steipete/wacli/internal/wa"
)

func TestHashPollOption(t *testing.T) {
	tests := []struct {
		name       string
		optionName string
		wantHash   []byte
	}{
		{
			name:       "simple option",
			optionName: "Red",
			wantHash:   sha256Hash("Red"),
		},
		{
			name:       "option with spaces",
			optionName: "Option with spaces",
			wantHash:   sha256Hash("Option with spaces"),
		},
		{
			name:       "option with unicode",
			optionName: "Opción 👍",
			wantHash:   sha256Hash("Opción 👍"),
		},
		{
			name:       "empty option",
			optionName: "",
			wantHash:   sha256Hash(""),
		},
		{
			name:       "long option",
			optionName: "This is a very long poll option that contains a lot of text to test hashing behavior",
			wantHash:   sha256Hash("This is a very long poll option that contains a lot of text to test hashing behavior"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wa.HashPollOption(tt.optionName)

			if !bytes.Equal(got, tt.wantHash) {
				t.Fatalf("wa.HashPollOption(%q) = %x, want %x", tt.optionName, got, tt.wantHash)
			}

			// Verify hash length (SHA-256 produces 32 bytes)
			if len(got) != 32 {
				t.Fatalf("expected hash length 32, got %d", len(got))
			}
		})
	}
}

func TestHashPollOptionConsistency(t *testing.T) {
	// Hash should be deterministic (same input -> same output)
	optionName := "Consistent Option"

	hash1 := wa.HashPollOption(optionName)
	hash2 := wa.HashPollOption(optionName)

	if !bytes.Equal(hash1, hash2) {
		t.Fatalf("wa.HashPollOption not consistent: %x != %x", hash1, hash2)
	}
}

func TestHashPollOptionUniqueness(t *testing.T) {
	// Different inputs should produce different hashes
	option1 := "Red"
	option2 := "Blue"

	hash1 := wa.HashPollOption(option1)
	hash2 := wa.HashPollOption(option2)

	if bytes.Equal(hash1, hash2) {
		t.Fatalf("different options produced same hash")
	}
}

// Helper to compute expected SHA-256 hash
func sha256Hash(input string) []byte {
	hash := sha256.Sum256([]byte(input))
	return hash[:]
}
