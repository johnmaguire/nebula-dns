package main

import "testing"

func TestTrimSuffix(t *testing.T) {
	tests := []struct {
		hostname string
		suffix   string
		want     string
	}{
		{"host.example.com", "example.com", "host"},
		{"host.example.com", ".example.com", "host"},
		{"forgejo-runner-1.demeter.example.com", "example.com", "forgejo-runner-1.demeter"},
		// Suffix absent: hostname unchanged
		{"forgejo-runner-1.demeter", "example.com", "forgejo-runner-1.demeter"},
		// Label boundary required: notexample.com is a different domain
		{"host.notexample.com", "example.com", "host.notexample.com"},
		// Never trim down to an empty hostname
		{"example.com", "example.com", "example.com"},
		// DNS names are case-insensitive
		{"Host.Example.COM", "example.com", "Host"},
	}

	for _, tt := range tests {
		if got := trimSuffix(tt.hostname, tt.suffix); got != tt.want {
			t.Errorf("trimSuffix(%q, %q) = %q, want %q", tt.hostname, tt.suffix, got, tt.want)
		}
	}
}
