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

func TestConfig_AccessAppRequests(t *testing.T) {
	config := &Config{
		AccessAppRequests: make(map[string]string),
	}

	config.AccessAppRequests["app.example.com"] = "My App"
	config.AccessAppRequests["api.example.com"] = "My API"

	if len(config.AccessAppRequests) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(config.AccessAppRequests))
	}
	if config.AccessAppRequests["app.example.com"] != "My App" {
		t.Errorf("expected 'My App', got %q", config.AccessAppRequests["app.example.com"])
	}

	delete(config.AccessAppRequests, "app.example.com")
	if len(config.AccessAppRequests) != 1 {
		t.Fatalf("expected 1 entry after delete, got %d", len(config.AccessAppRequests))
	}
}

func TestKubernetesApiTunnelConfig_GetService(t *testing.T) {
	config := KubernetesApiTunnelConfig{
		Server: "https://kubernetes.default.svc",
	}

	expected := "tcp://https://kubernetes.default.svc"
	if config.GetService() != expected {
		t.Errorf("expected %q, got %q", expected, config.GetService())
	}
}
