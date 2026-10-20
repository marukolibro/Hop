//go:build darwin

package sysdns

import (
	"slices"
	"testing"
)

// TestParseServers pins how networksetup's two answers read: the servers one
// per line, and the sentence that means none are set on the service.
func TestParseServers(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want []string
	}{
		{"one server", "114.114.114.114\n", []string{"114.114.114.114"}},
		{"two servers", "223.5.5.5\n119.29.29.29\n", []string{"223.5.5.5", "119.29.29.29"}},
		{"none set", "There aren't any DNS Servers set on Thunderbolt Bridge.\n", []string{}},
		{"blank lines", "\n223.5.5.5\n\n", []string{"223.5.5.5"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseServers(tc.out); !slices.Equal(got, tc.want) {
				t.Fatalf("parseServers(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}

// TestSetArgs pins the argument lists Write builds, Empty included.
func TestSetArgs(t *testing.T) {
	cases := []struct {
		name    string
		servers []string
		want    []string
	}{
		{"servers", []string{"223.5.5.5", "119.29.29.29"}, []string{"-setdnsservers", "Wi-Fi", "223.5.5.5", "119.29.29.29"}},
		{"none", []string{}, []string{"-setdnsservers", "Wi-Fi", Empty}},
		{"nil", nil, []string{"-setdnsservers", "Wi-Fi", Empty}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := setArgs("Wi-Fi", tc.servers); !slices.Equal(got, tc.want) {
				t.Fatalf("setArgs(%q, %v) = %v, want %v", "Wi-Fi", tc.servers, got, tc.want)
			}
		})
	}
}

// TestBackupRoundTrip keeps an empty list an empty list: it is what says the
// service had no servers of its own.
func TestBackupRoundTrip(t *testing.T) {
	backup := Backup{Services: map[string][]string{
		"Wi-Fi":              {"114.114.114.114"},
		"Thunderbolt Bridge": {},
	}}
	raw, err := backup.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeBackup(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 2 {
		t.Fatalf("decoded %d services, want 2", len(got.Services))
	}
	if !slices.Equal(got.Services["Wi-Fi"], []string{"114.114.114.114"}) {
		t.Errorf("Wi-Fi = %v", got.Services["Wi-Fi"])
	}
	if servers, ok := got.Services["Thunderbolt Bridge"]; !ok || len(servers) != 0 {
		t.Errorf("Thunderbolt Bridge = %v (present=%v), want an empty list", servers, ok)
	}
}

// TestDecodeBackupRefusesEmpty keeps a corrupt file from reading as "nothing
// to restore".
func TestDecodeBackupRefusesEmpty(t *testing.T) {
	if _, err := DecodeBackup([]byte("{}")); err == nil {
		t.Fatal("a backup naming no services decoded without an error")
	}
	if _, err := DecodeBackup([]byte("not json")); err == nil {
		t.Fatal("a non-JSON backup decoded without an error")
	}
}
