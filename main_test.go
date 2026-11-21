package main

import (
	"net/netip"
	"testing"
)

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

func TestUnderSuffix(t *testing.T) {
	tests := []struct {
		name   string
		suffix string
		want   bool
	}{
		{"host.n.example.com", "n.example.com", true},
		{"a.b.n.example.com", "n.example.com", true},
		{"host.n.example.com", "N.Example.com", true},
		// Label boundary required
		{"xn.example.com", "n.example.com", false},
		// The suffix itself (possibly the zone apex) is never ours
		{"n.example.com", "n.example.com", false},
		{"example.com", "example.com", false},
	}

	for _, tt := range tests {
		if got := underSuffix(tt.name, tt.suffix); got != tt.want {
			t.Errorf("underSuffix(%q, %q) = %v, want %v", tt.name, tt.suffix, got, tt.want)
		}
	}
}

func TestManagedTypes_AccumulatesAcrossHostsSharingAName(t *testing.T) {
	managed := managedTypes([]HostRecord{
		{Name: "db.example.com", IPs: []netip.Addr{netip.MustParseAddr("10.0.0.1")}},
		{Name: "db.example.com", IPs: []netip.Addr{netip.MustParseAddr("fd00::1")}},
	})

	for _, recordType := range []string{"A", "AAAA"} {
		if _, ok := managed["db.example.com"][recordType]; !ok {
			t.Errorf("managed[db.example.com] lacks %s: %v", recordType, managed)
		}
	}
}
