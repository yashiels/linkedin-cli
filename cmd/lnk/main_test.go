package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yashiels/linkedin-cli/internal/types"
)

func isolatedHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("LNK_CONFIG", filepath.Join(home, "config.toml"))
	return home
}
func TestErrorOutputModes(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		for _, plain := range []bool{false, true} {
			for _, verbose := range []bool{false, true} {
				for _, debug := range []bool{false, true} {
					for _, code := range []int{1, 2, 3, 4, 5} {
						t.Run(fmt.Sprintf("%t/%t/%t/%t/%d", jsonMode, plain, verbose, debug, code), func(t *testing.T) {
							isolatedHome(t)
							root := newRootCmd()
							for flag, value := range map[string]bool{"json": jsonMode, "plain": plain, "verbose": verbose, "debug": debug} {
								if err := root.PersistentFlags().Set(flag, fmt.Sprint(value)); err != nil {
									t.Fatal(err)
								}
							}
							var output bytes.Buffer
							err := fmt.Errorf("PERSONAL-SENTINEL https://URL-SENTINEL: %w", types.WrapError(code, "CODED-MESSAGE-SENTINEL https://PRIVATE-SENTINEL", &url.Error{URL: "https://URL-SENTINEL", Err: fmt.Errorf("BODY-SENTINEL")}))
							if got := writeError(root, &output, err); got != code {
								t.Fatalf("exit %d expected %d", got, code)
							}
							if strings.Contains(output.String(), "SENTINEL") {
								t.Fatal("sentinel leaked")
							}
							if jsonMode {
								var result map[string]any
								if err = json.Unmarshal(output.Bytes(), &result); err != nil {
									t.Fatal(err)
								}
								if result["code"] != float64(code) || result["error"] != map[int]string{1: "command failed", 2: "invalid arguments or configuration", 3: "authentication or refusal stop; inspect safety status", 4: "network or safety state failure; inspect safety status", 5: "request budget, cooldown or rate limit reached; inspect safety status"}[code] {
									t.Fatalf("wrong error shape: %v", result)
								}
							}
						})
					}
				}
			}
		}
	}
}
func TestSafetyCommandsAreOfflineAndJSON(t *testing.T) {
	home := isolatedHome(t)
	for _, args := range [][]string{{"safety", "status", "--json"}, {"--json", "safety", "status"}, {"safety", "resume", "--confirm", "--json"}} {
		var output bytes.Buffer
		root := newRootCmd()
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		if err := json.Unmarshal(output.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state["admissions"] != float64(0) || state["budget"] != float64(100) {
			t.Fatalf("unexpected local state %v", state)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "lnk", "credentials.json")); !os.IsNotExist(err) {
		t.Fatal("safety touched credentials")
	}
	root := newRootCmd()
	root.SetArgs([]string{"safety", "resume", "--json"})
	err := root.Execute()
	var output bytes.Buffer
	if got := writeError(root, &output, err); got != 2 || !json.Valid(output.Bytes()) {
		t.Fatalf("confirmation: exit=%d %s", got, output.String())
	}
}
func TestUnsafeConfigAndFlagsExitUsage(t *testing.T) {
	home := isolatedHome(t)
	path := filepath.Join(home, "unsafe.toml")
	if err := os.WriteFile(path, []byte("[safety]\nbudget=101\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--config", path, "safety", "status"}, {"--unknown"}} {
		root := newRootCmd()
		root.SetArgs(args)
		err := root.Execute()
		var out bytes.Buffer
		if got := writeError(root, &out, err); got != 2 {
			t.Fatalf("exit=%d error=%v", got, err)
		}
	}
}
