package database

import (
	"database/sql"
	"testing"
	"time"
)

func TestNullString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected sql.NullString
	}{
		{
			name:     "non-empty string",
			input:    "test",
			expected: sql.NullString{String: "test", Valid: true},
		},
		{
			name:     "empty string",
			input:    "",
			expected: sql.NullString{Valid: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NullString(tt.input)
			if result.Valid != tt.expected.Valid {
				t.Errorf("NullString(%q).Valid = %v, want %v", tt.input, result.Valid, tt.expected.Valid)
			}
			if result.String != tt.expected.String {
				t.Errorf("NullString(%q).String = %v, want %v", tt.input, result.String, tt.expected.String)
			}
		})
	}
}

func TestNullTime(t *testing.T) {
	tests := []struct {
		name          string
		input         time.Time
		expectedValid bool
	}{
		{
			name:          "non-zero time",
			input:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedValid: true,
		},
		{
			name:          "zero time",
			input:         time.Time{},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NullTime(tt.input)
			if result.Valid != tt.expectedValid {
				t.Errorf("NullTime(%v).Valid = %v, want %v", tt.input, result.Valid, tt.expectedValid)
			}
			if tt.expectedValid && !result.Time.Equal(tt.input) {
				t.Errorf("NullTime(%v).Time = %v, want %v", tt.input, result.Time, tt.input)
			}
		})
	}
}
