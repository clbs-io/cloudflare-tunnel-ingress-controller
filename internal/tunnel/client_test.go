package tunnel

import (
	"testing"
)

func TestIsInZone(t *testing.T) {
	c := &Client{}

	tests := []struct {
		hostname string
		zoneName string
		expected bool
	}{
		{"app.example.com", "example.com", true},
		{"example.com", "example.com", true},
		{"sub.app.example.com", "example.com", true},
		{"notexample.com", "example.com", false},
		{"app.other.com", "example.com", false},
		{"fakeexample.com", "example.com", false},
		{"", "example.com", false},
		{"app.example.com", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.hostname+"_in_"+tt.zoneName, func(t *testing.T) {
			result := c.isInZone(tt.hostname, tt.zoneName)
			if result != tt.expected {
				t.Errorf("isInZone(%q, %q) = %v, want %v", tt.hostname, tt.zoneName, result, tt.expected)
			}
		})
	}
}
