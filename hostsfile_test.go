package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderHostsFile(t *testing.T) {
	records := []HostRecord{
		{Name: "b.n.example.com", IPs: []netip.Addr{
			netip.MustParseAddr("fd00::2"),
			netip.MustParseAddr("192.168.128.2"),
		}},
		{Name: "a.n.example.com", IPs: []netip.Addr{netip.MustParseAddr("192.168.128.1")}},
		{Name: "no-ips.n.example.com"},
	}

	want := hostsFileHeader +
		"192.168.128.1 a.n.example.com\n" +
		"192.168.128.2 b.n.example.com\n" +
		"fd00::2 b.n.example.com\n"
	if got := string(RenderHostsFile(records)); got != want {
		t.Errorf("RenderHostsFile() =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteHostsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mesh.hosts")
	records := []HostRecord{{Name: "a.n.example.com", IPs: []netip.Addr{netip.MustParseAddr("192.168.128.1")}}}

	changed, err := WriteHostsFile(path, records)
	if err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v, want changed", changed, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}

	changed, err = WriteHostsFile(path, records)
	if err != nil || changed {
		t.Fatalf("identical write: changed=%v err=%v, want unchanged", changed, err)
	}

	changed, err = WriteHostsFile(path, nil)
	if err != nil || !changed {
		t.Fatalf("emptying write: changed=%v err=%v, want changed", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != hostsFileHeader {
		t.Errorf("contents = %q, want only the header", got)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the hosts file (temp file left behind?)", len(entries))
	}
}

func TestValidHostname(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"dns.demeter.n.example.com", true},
		{"forgejo-runner_1.n.example.com", true},
		{"", false},
		{"two words.n.example.com", false},
		{"host#1.n.example.com", false},
		{"double..dot.example.com", false},
		{"Upper.n.example.com", false},
	}
	for _, tt := range tests {
		if got := validHostname(tt.name); got != tt.want {
			t.Errorf("validHostname(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
