package main

import (
	"strings"
	"testing"
)

func TestMakeBar(t *testing.T) {
	tests := []struct {
		name       string
		count      int
		total      int
		width      int
		wantFilled int
		wantEmpty  int
	}{
		{
			name:       "zero votes",
			count:      0,
			total:      10,
			width:      20,
			wantFilled: 0,
			wantEmpty:  20,
		},
		{
			name:       "full votes",
			count:      10,
			total:      10,
			width:      20,
			wantFilled: 20,
			wantEmpty:  0,
		},
		{
			name:       "half votes",
			count:      5,
			total:      10,
			width:      20,
			wantFilled: 10,
			wantEmpty:  10,
		},
		{
			name:       "one third votes",
			count:      1,
			total:      3,
			width:      15,
			wantFilled: 5,
			wantEmpty:  10,
		},
		{
			name:       "total zero",
			count:      0,
			total:      0,
			width:      20,
			wantFilled: 0,
			wantEmpty:  20,
		},
		{
			name:       "count exceeds total",
			count:      15,
			total:      10,
			width:      20,
			wantFilled: 20,
			wantEmpty:  0,
		},
		{
			name:       "small width",
			count:      1,
			total:      10,
			width:      5,
			wantFilled: 0, // rounds down
			wantEmpty:  5,
		},
		{
			name:       "width one",
			count:      50,
			total:      100,
			width:      1,
			wantFilled: 0, // 0.5 rounds down to 0
			wantEmpty:  1,
		},
		{
			name:       "width one full",
			count:      100,
			total:      100,
			width:      1,
			wantFilled: 1,
			wantEmpty:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := makeBar(tt.count, tt.total, tt.width)

			// Count filled and empty characters
			filled := strings.Count(bar, "█")
			empty := strings.Count(bar, "░")

			if filled != tt.wantFilled {
				t.Errorf("filled = %d, want %d (bar: %q)", filled, tt.wantFilled, bar)
			}
			if empty != tt.wantEmpty {
				t.Errorf("empty = %d, want %d (bar: %q)", empty, tt.wantEmpty, bar)
			}

			// Verify total width
			totalWidth := filled + empty
			if totalWidth != tt.width {
				t.Errorf("total width = %d, want %d (bar: %q)", totalWidth, tt.width, bar)
			}

			// Verify bar length (should match width)
			barLen := len([]rune(bar))
			if barLen != tt.width {
				t.Errorf("bar rune length = %d, want %d (bar: %q)", barLen, tt.width, bar)
			}
		})
	}
}

func TestMakeBarPercentages(t *testing.T) {
	// Test various percentages with standard width
	width := 20

	tests := []struct {
		name       string
		percentage float64
		wantFilled int
	}{
		{"0%", 0.0, 0},
		{"10%", 0.1, 2},
		{"25%", 0.25, 5},
		{"50%", 0.5, 10},
		{"75%", 0.75, 15},
		{"90%", 0.9, 18},
		{"100%", 1.0, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := 100
			count := int(tt.percentage * float64(total))

			bar := makeBar(count, total, width)
			filled := strings.Count(bar, "█")

			if filled != tt.wantFilled {
				t.Errorf("filled = %d, want %d for %s (bar: %q)", filled, tt.wantFilled, tt.name, bar)
			}
		})
	}
}

func TestMakeBarEdgeCases(t *testing.T) {
	// Edge case: negative count (shouldn't happen, but test defensively)
	t.Run("negative count", func(t *testing.T) {
		bar := makeBar(-5, 10, 20)
		filled := strings.Count(bar, "█")
		// Should treat as 0
		if filled != 0 {
			t.Errorf("negative count should produce 0 filled, got %d", filled)
		}
	})

	// Edge case: negative total (shouldn't happen)
	t.Run("negative total", func(t *testing.T) {
		bar := makeBar(5, -10, 20)
		empty := strings.Count(bar, "░")
		// With negative total, should default to all empty
		if empty != 20 {
			t.Errorf("negative total should produce all empty, got %d empty", empty)
		}
	})

	// Edge case: zero width
	t.Run("zero width", func(t *testing.T) {
		bar := makeBar(5, 10, 0)
		if bar != "" {
			t.Errorf("zero width should produce empty string, got %q", bar)
		}
	})

	// Edge case: very large numbers
	t.Run("large numbers", func(t *testing.T) {
		bar := makeBar(1000000, 2000000, 20)
		filled := strings.Count(bar, "█")
		// Should be exactly half (10 filled)
		if filled != 10 {
			t.Errorf("large numbers at 50%% should produce 10 filled, got %d", filled)
		}
	})
}

func TestMakeBarRounding(t *testing.T) {
	// Test rounding behavior at boundary
	width := 10

	tests := []struct {
		name       string
		count      int
		total      int
		wantFilled int
	}{
		// 0.1 / 10 = 0.01 * 10 = 0.1 -> rounds down to 0
		{"rounds down", 1, 100, 0},
		// 11 / 100 = 0.11 * 10 = 1.1 -> rounds down to 1
		{"rounds down to 1", 11, 100, 1},
		// 5 / 100 = 0.05 * 10 = 0.5 -> rounds down to 0
		{"exactly 0.5 rounds down", 5, 100, 0},
		// 51 / 100 = 0.51 * 10 = 5.1 -> rounds down to 5
		{"over half rounds down", 51, 100, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := makeBar(tt.count, tt.total, width)
			filled := strings.Count(bar, "█")

			if filled != tt.wantFilled {
				t.Errorf("filled = %d, want %d (bar: %q)", filled, tt.wantFilled, bar)
			}
		})
	}
}
