package safety

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLongSpacingSurvivesPruningAndRestart(t *testing.T) {
	for _, elapsed := range []time.Duration{24 * time.Hour, 25 * time.Hour} {
		for _, action := range []string{"none", "status", "resume"} {
			t.Run(elapsed.String()+"/"+action, func(t *testing.T) {
				p, c := fixture(t, 1)
				p.cfg.Delay = 48 * time.Hour
				first := c.now
				lease, err := p.Admit(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if action == "resume" {
					code(t, lease.Refusal(401, false, ""), 3)
				}
				lease.Close()
				c.now = first.Add(elapsed)
				fresh, err := New(p.root, p.cfg)
				if err != nil {
					t.Fatal(err)
				}
				if action != "none" {
					var status Status
					if action == "status" {
						status, err = fresh.Status(context.Background())
					} else {
						status, err = fresh.Resume(context.Background(), true)
					}
					if err != nil {
						t.Fatal(err)
					}
					if status.Admissions != 0 {
						t.Fatal("expired admission counted in budget")
					}
				}
				c.waits = nil
				fresh.cfg.Wait = func(ctx context.Context, d time.Duration) error {
					if len(readState(t, fresh).Admissions) != 0 {
						t.Fatal("expired admission counted during pacing")
					}
					c.waits = append(c.waits, d)
					c.now = c.now.Add(d)
					return nil
				}
				admit(t, fresh)
				if len(c.waits) != 1 || c.waits[0] != 48*time.Hour-elapsed || !c.now.Equal(first.Add(48*time.Hour)) {
					t.Fatalf("spacing shortened after prune/restart: waits=%v now=%v", c.waits, c.now)
				}
				if len(readState(t, fresh).Admissions) != 1 {
					t.Fatal("rolling history not pruned")
				}
			})
		}
	}
}

func TestLongSpacingFreshProcessHelper(t *testing.T) {
	root := os.Getenv("LNK_TEST_LONG_SPACING_ROOT")
	if root == "" {
		return
	}
	elapsed, err := strconv.Atoi(os.Getenv("LNK_TEST_LONG_SPACING_HOURS"))
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	now := first.Add(time.Duration(elapsed) * time.Hour)
	var waits []time.Duration
	p, err := New(root, Config{Delay: 48 * time.Hour, Budget: 1, Now: func() time.Time { return now }, Wait: func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		now = now.Add(d)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	status, err := p.Status(context.Background())
	if err != nil || status.Admissions != 0 {
		t.Fatalf("expired budget: %v %v", status, err)
	}
	admit(t, p)
	if !now.Equal(first.Add(48*time.Hour)) || len(waits) != 1 || waits[0] != (48-time.Duration(elapsed))*time.Hour {
		t.Fatalf("fresh process released spacing: now=%v waits=%v", now, waits)
	}
	fmt.Println("LONG_SPACING_PRESERVED")
}

func TestLongSpacingFreshProcess(t *testing.T) {
	for _, hours := range []int{24, 25} {
		t.Run(strconv.Itoa(hours), func(t *testing.T) {
			p, _ := fixture(t, 1)
			p.cfg.Delay = 48 * time.Hour
			admit(t, p)
			command := exec.Command(os.Args[0], "-test.run=^TestLongSpacingFreshProcessHelper$", "-test.count=1")
			command.Env = append(os.Environ(), "HOME="+filepath.Dir(p.root), "LNK_TEST_LONG_SPACING_ROOT="+p.root, "LNK_TEST_LONG_SPACING_HOURS="+strconv.Itoa(hours))
			output, err := command.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "LONG_SPACING_PRESERVED") {
				t.Fatalf("fresh-process spacing failed: %v %s", err, output)
			}
		})
	}
}

func TestLastAdmissionMetadataValidation(t *testing.T) {
	for _, kind := range []string{"missing", "null", "future", "older-than-history", "newer-than-history", "zero"} {
		t.Run(kind, func(t *testing.T) {
			p, c := fixture(t, 100)
			admit(t, p)
			filename := filepath.Join(p.root, "state.json")
			data, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err = json.Unmarshal(data, &state); err != nil {
				t.Fatal(err)
			}
			if _, ok := state["last_admission"]; !ok {
				t.Fatal("latest admission is not separately persisted")
			}
			switch kind {
			case "missing":
				delete(state, "last_admission")
			case "null":
				state["last_admission"] = nil
			case "future":
				state["last_admission"] = c.now.Add(time.Hour).Format(time.RFC3339Nano)
			case "older-than-history":
				state["last_admission"] = c.now.Add(-time.Hour).Format(time.RFC3339Nano)
			case "newer-than-history":
				state["last_clock"] = c.now.Add(time.Hour).Format(time.RFC3339Nano)
				state["last_admission"] = c.now.Add(time.Minute).Format(time.RFC3339Nano)
			case "zero":
				state["last_admission"] = time.Time{}.Format(time.RFC3339Nano)
			}
			data, err = json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filename, data, 0600); err != nil {
				t.Fatal(err)
			}
			_, err = p.Admit(context.Background())
			code(t, err, 4)
		})
	}
}

func TestLongSpacingExactBoundary(t *testing.T) {
	p, c := fixture(t, 1)
	p.cfg.Delay = 48 * time.Hour
	admit(t, p)
	c.now = c.now.Add(48 * time.Hour)
	fresh, err := New(p.root, p.cfg)
	if err != nil {
		t.Fatal(err)
	}
	fresh.cfg.Wait = func(context.Context, time.Duration) error { t.Fatal("waited at exact pacing boundary"); return nil }
	admit(t, fresh)
	if len(readState(t, fresh).Admissions) != 1 {
		t.Fatal("expired request counted at pacing boundary")
	}
}
