package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafetyConfiguration(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{"", true}, {"[safety]\nbudget=10\ndelay_seconds=20", true},
		{"[safety]\nbudget=101", false}, {"[safety]\nbudget=0", false},
		{"[safety]\ndelay_seconds=4", false}, {"[safety]\ndelay_seconds=0", false},
		{"[safety]\ndelay_seconds=172800", true}, {"[safety]\ndelay_seconds=9223372036854775807", false}, {"[safety]\nforce=true", false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			path := filepath.Join(home, "config.toml")
			t.Setenv("LNK_CONFIG", path)
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if (err == nil) != tc.valid {
				t.Fatalf("cfg=%v error=%v", cfg, err)
			}
		})
	}
}
