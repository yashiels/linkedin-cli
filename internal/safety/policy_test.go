package safety

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yashiels/linkedin-cli/internal/types"
	"golang.org/x/sys/unix"
)

type clock struct {
	now   time.Time
	waits []time.Duration
}

func fixture(t *testing.T, budget int) (*Policy, *clock) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	c := &clock{now: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
	p, err := New(root, Config{Budget: budget, Now: func() time.Time { return c.now }, Wait: func(ctx context.Context, d time.Duration) error {
		if ctx.Err() != nil {
			return failure(4, "canceled")
		}
		c.waits = append(c.waits, d)
		c.now = c.now.Add(d)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return p, c
}
func code(t *testing.T, err error, want int) {
	t.Helper()
	var coded *types.LnkError
	if !errors.As(err, &coded) || coded.Code != want {
		t.Fatalf("expected exit %d; got %v", want, err)
	}
}
func admit(t *testing.T, p *Policy) {
	t.Helper()
	lease, err := p.Admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
}
func readState(t *testing.T, p *Policy) State {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p.root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s State
	if err = json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSpacingBoundaries(t *testing.T) {
	for _, elapsed := range []time.Duration{0, 5*time.Second - time.Nanosecond, 5 * time.Second, 6 * time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			p, c := fixture(t, 100)
			admit(t, p)
			start := c.now
			c.now = c.now.Add(elapsed)
			admit(t, p)
			expected := max(elapsed, 5*time.Second)
			if c.now.Sub(start) != expected {
				t.Fatalf("elapsed=%s expected=%s", c.now.Sub(start), expected)
			}
			if elapsed >= 5*time.Second && len(c.waits) != 0 {
				t.Fatal("waited at allowed boundary")
			}
		})
	}
}
func TestRollingBudgetAndExpiry(t *testing.T) {
	p, c := fixture(t, 100)
	first := c.now
	for i := 0; i < 100; i++ {
		admit(t, p)
	}
	_, err := p.Admit(context.Background())
	code(t, err, 5)
	if len(readState(t, p).Admissions) != 100 {
		t.Fatal("budget not durable")
	}
	c.now = first.Add(24*time.Hour - time.Nanosecond)
	_, err = p.Admit(context.Background())
	code(t, err, 5)
	c.now = first.Add(24 * time.Hour)
	admit(t, p)
	s := readState(t, p)
	if len(s.Admissions) != 100 || !s.Admissions[99].Equal(c.now) {
		t.Fatal("24h boundary incorrect")
	}
}
func TestBackwardClock(t *testing.T) {
	p, c := fixture(t, 100)
	admit(t, p)
	before := readState(t, p)
	c.now = c.now.Add(-time.Nanosecond)
	_, err := p.Admit(context.Background())
	code(t, err, 4)
	_, err = p.Resume(context.Background(), true)
	code(t, err, 4)
	after := readState(t, p)
	if !before.LastClock.Equal(after.LastClock) || len(after.Admissions) != 1 {
		t.Fatal("clock rollback rewrote history")
	}
}
func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, header string
		want         time.Duration
		overflow     bool
	}{
		{"missing", "", 30 * time.Minute, false}, {"invalid", "garbage", 30 * time.Minute, false},
		{"negative", "-10", 30 * time.Minute, false}, {"past", now.Add(-time.Hour).Format(time.RFC1123), 30 * time.Minute, false},
		{"seconds", "3600", time.Hour, false}, {"floor", "1", 30 * time.Minute, false},
		{"date", now.Add(48 * time.Hour).Format("Mon, 02 Jan 2006 15:04:05 GMT"), 48 * time.Hour, false},
		{"long", "3153600000", 100 * 365 * 24 * time.Hour, false},
		{"overflow", "18446744073709551616", 0, true}, {"duration-overflow", "9223372036854775807", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, c := fixture(t, 100)
			c.now = now
			l, err := p.Admit(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			code(t, l.Refusal(429, false, tc.header), 5)
			l.Close()
			fresh, err := New(p.root, p.cfg)
			if err != nil {
				t.Fatal(err)
			}
			s := readState(t, p)
			if tc.overflow {
				if s.Block != "retry_after_overflow" {
					t.Fatal("overflow not latched")
				}
				_, err = fresh.Resume(context.Background(), true)
				code(t, err, 5)
			} else {
				if !s.Cooldown.Equal(now.Add(tc.want)) {
					t.Fatalf("deadline %v expected %v", s.Cooldown, now.Add(tc.want))
				}
				_, err = fresh.Resume(context.Background(), true)
				if err != nil {
					t.Fatal(err)
				}
				if !readState(t, p).Cooldown.Equal(s.Cooldown) {
					t.Fatal("resume changed cooldown")
				}
			}
			_, err = fresh.Admit(context.Background())
			code(t, err, 5)
			if len(readState(t, p).Admissions) != 1 {
				t.Fatal("refused admission counted")
			}
			if !tc.overflow {
				c.now = s.Cooldown
				admit(t, fresh)
			}
		})
	}
}
func TestResumePreservesBudgetAndMarker(t *testing.T) {
	p, _ := fixture(t, 1)
	l, err := p.Admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	code(t, l.Refusal(403, false, ""), 3)
	l.Close()
	before := readState(t, p)
	_, err = p.Resume(context.Background(), false)
	code(t, err, 2)
	_, err = p.Admit(context.Background())
	code(t, err, 3)
	status, err := p.Resume(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if status.Block != "" || status.Admissions != 1 {
		t.Fatal("resume reset budget")
	}
	_, err = p.Admit(context.Background())
	code(t, err, 5)
	after := readState(t, p)
	if !after.Admissions[0].Equal(before.Admissions[0]) {
		t.Fatal("resume changed history")
	}
	if _, err = os.Stat(filepath.Join(p.root, "initialized")); err != nil {
		t.Fatal(err)
	}
}
func TestCanceledAndLockTimeout(t *testing.T) {
	p, _ := fixture(t, 100)
	l, err := p.Admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	fresh, err := New(p.root, Config{})
	if err != nil {
		t.Fatal(err)
	}
	fresh.lockTimeout = 20 * time.Millisecond
	_, err = fresh.Admit(context.Background())
	code(t, err, 5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = fresh.Admit(ctx)
	code(t, err, 4)
	if len(readState(t, p).Admissions) != 1 {
		t.Fatal("wait consumed quota")
	}
}
func TestUnsafeStateFixtures(t *testing.T) {
	for _, kind := range []string{"marker-missing", "state-missing", "corrupt", "unknown-field", "missing-field", "file-mode", "directory-mode", "state-symlink", "marker-symlink", "lock-symlink", "ancestor-symlink", "owner", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			p, _ := fixture(t, 100)
			admit(t, p)
			statePath := filepath.Join(p.root, "state.json")
			switch kind {
			case "marker-missing":
				os.Remove(filepath.Join(p.root, "initialized"))
			case "state-missing":
				os.Remove(statePath)
			case "corrupt":
				os.WriteFile(statePath, []byte("not json"), 0600)
			case "unknown-field":
				os.WriteFile(statePath, []byte(`{"version":2,"admissions":[],"secret":"synthetic"}`), 0600)
			case "missing-field":
				os.WriteFile(statePath, []byte(`{"version":2,"admissions":[]}`), 0600)
			case "file-mode":
				os.Chmod(statePath, 0644)
			case "directory-mode":
				os.Chmod(p.root, 0755)
			case "state-symlink", "marker-symlink", "lock-symlink":
				name := map[string]string{"state-symlink": "state.json", "marker-symlink": "initialized", "lock-symlink": "lock"}[kind]
				os.Remove(filepath.Join(p.root, name))
				if err := os.Symlink("/dev/null", filepath.Join(p.root, name)); err != nil {
					t.Fatal(err)
				}
			case "ancestor-symlink":
				parent := filepath.Dir(p.root)
				link := filepath.Join(parent, "link")
				if err := os.Symlink(p.root, link); err != nil {
					t.Fatal(err)
				}
				p.root = link
			case "owner":
				fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(fd)
				var st unix.Stat_t
				if err = unix.Fstat(fd, &st); err != nil {
					t.Fatal(err)
				}
				if st.Uid == uint32(os.Getuid()) {
					t.Skip("test requires a non-root caller")
				}
				code(t, validateFD(fd, true, true), 4)
				return

			case "hardlink":
				if err := os.Link(statePath, filepath.Join(p.root, "alias")); err != nil {
					t.Fatal(err)
				}
			}
			_, err := p.Admit(context.Background())
			code(t, err, 4)
		})
	}
}
func TestStrictConfig(t *testing.T) {
	for _, cfg := range []Config{{Delay: time.Second}, {Budget: 101}, {Budget: -1}, {Delay: -time.Second}, {Delay: MaxDelay + time.Second}} {
		_, err := New("/unused", cfg)
		code(t, err, 2)
	}
}
func TestStatePermissions(t *testing.T) {
	p, _ := fixture(t, 100)
	admit(t, p)
	for _, name := range []string{"lock", "initialized", "state.json"} {
		info, err := os.Stat(filepath.Join(p.root, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe %s mode %v", name, info.Mode())
		}
	}
}

func TestPolicySubprocess(t *testing.T) {
	mode := os.Getenv("LNK_TEST_CHILD_MODE")
	if mode == "" {
		return
	}
	root := os.Getenv("LNK_TEST_POLICY_ROOT")
	p, err := New(root, Config{Budget: 6, Now: func() time.Time {
		b, e := os.ReadFile(filepath.Join(filepath.Dir(root), "clock"))
		if e != nil {
			panic(e)
		}
		var now time.Time
		if e = now.UnmarshalText(b); e != nil {
			panic(e)
		}
		return now
	}, Wait: func(ctx context.Context, d time.Duration) error {
		if d < time.Second {
			return wait(ctx, d)
		}
		b, e := os.ReadFile(filepath.Join(filepath.Dir(root), "clock"))
		if e != nil {
			return e
		}
		var now time.Time
		if e = now.UnmarshalText(b); e != nil {
			return e
		}
		next, _ := now.Add(d).MarshalText()
		return os.WriteFile(filepath.Join(filepath.Dir(root), "clock"), next, 0600)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if mode == "marker" {
		s, e := openStore(root)
		if e != nil {
			t.Fatal(e)
		}
		if unix.Flock(int(s.lock.Fd()), unix.LOCK_EX) != nil {
			t.Fatal("lock")
		}
		if e = s.create("initialized", []byte("lnk-safety-v2\n")); e != nil {
			t.Fatal(e)
		}
		os.Exit(0)
	}
	l, err := p.Admit(context.Background())
	if err != nil {
		var coded *types.LnkError
		if errors.As(err, &coded) && coded.Code == 5 {
			fmt.Println("BUDGET")
			return
		}
		t.Fatal(err)
	}
	if mode == "429" {
		if err = l.Refusal(429, false, "7200"); err == nil {
			t.Fatal("missing refusal")
		}
		l.Close()
		return
	}
	if mode == "crash" {
		os.Exit(0)
	}
	probe := filepath.Join(filepath.Dir(root), "exchange")
	f, e := os.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal("OVERLAPPING EXCHANGE", e)
	}
	time.Sleep(10 * time.Millisecond)
	f.Close()
	os.Remove(probe)
	l.Close()
	fmt.Println("ADMITTED")
}
func child(t *testing.T, root, mode string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestPolicySubprocess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "HOME="+filepath.Dir(root), "LNK_TEST_CHILD_MODE="+mode, "LNK_TEST_POLICY_ROOT="+root)
	return cmd
}
func subprocessRoot(t *testing.T) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC).MarshalText()
	if err = os.WriteFile(filepath.Join(parent, "clock"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "policy")
}
func TestConcurrentStoreInitialization(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "policy")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := openStore(root)
			if err != nil {
				t.Error(err)
				return
			}
			s.close()
		}()
	}
	wg.Wait()
}

func TestSubprocessSharedBudgetStress(t *testing.T) {
	for run := 0; run < 5; run++ {
		t.Run(fmt.Sprint(run), func(t *testing.T) {
			root := subprocessRoot(t)
			var wg sync.WaitGroup
			outputs := make(chan string, 10)
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					b, err := child(t, root, "exchange").CombinedOutput()
					if err != nil {
						t.Errorf("child: %v %s", err, b)
					}
					outputs <- string(b)
				}()
			}
			wg.Wait()
			close(outputs)
			admitted, refused := 0, 0
			for output := range outputs {
				admitted += strings.Count(output, "ADMITTED")
				refused += strings.Count(output, "BUDGET")
			}
			if admitted != 6 || refused != 4 {
				t.Fatalf("admitted %d refused %d", admitted, refused)
			}
		})
	}
}
func TestSubprocessCrashDurability(t *testing.T) {
	for _, mode := range []string{"crash", "marker"} {
		t.Run(mode, func(t *testing.T) {
			root := subprocessRoot(t)
			if b, err := child(t, root, mode).CombinedOutput(); err != nil {
				t.Fatalf("child: %v %s", err, b)
			}
			p, err := New(root, Config{Budget: 1, Now: func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Admit(context.Background())
			expected := 5
			if mode == "marker" {
				expected = 4
			}
			code(t, err, expected)
		})
	}
}

func TestCooldownSurvivesFreshProcess(t *testing.T) {
	root := subprocessRoot(t)
	if output, err := child(t, root, "429").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, output)
	}
	output, err := child(t, root, "exchange").CombinedOutput()
	if err != nil || !strings.Contains(string(output), "BUDGET") || strings.Contains(string(output), "ADMITTED") {
		t.Fatalf("fresh process ignored cooldown: %v %s", err, output)
	}
}

func TestCanceledWhilePacingConsumesNoAdmission(t *testing.T) {
	p, _ := fixture(t, 100)
	admit(t, p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.cfg.Wait = func(ctx context.Context, d time.Duration) error { cancel(); return failure(4, "safety wait canceled") }
	_, err := p.Admit(ctx)
	code(t, err, 4)
	if len(readState(t, p).Admissions) != 1 {
		t.Fatal("pacing cancellation consumed quota")
	}
}
func TestFarFutureHTTPDateIsNotCapped(t *testing.T) {
	p, _ := fixture(t, 100)
	lease, err := p.Admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	code(t, lease.Refusal(429, false, deadline.Format("Mon, 02 Jan 2006 15:04:05 GMT")), 5)
	lease.Close()
	if !readState(t, p).Cooldown.Equal(deadline) {
		t.Fatal("long server deadline was capped")
	}
}
