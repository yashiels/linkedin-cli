package safety

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yashiels/linkedin-cli/internal/types"
	"golang.org/x/sys/unix"
)

const MaxBody = 16 << 20
const MaxDelay = time.Duration(1<<63-1) - 2*time.Minute

type Config struct {
	Delay  time.Duration
	Budget int
	Now    func() time.Time
	Wait   func(context.Context, time.Duration) error
}

type Policy struct {
	root        string
	cfg         Config
	lockTimeout time.Duration
}

type State struct {
	Version       int         `json:"version"`
	Admissions    []time.Time `json:"admissions"`
	Cooldown      time.Time   `json:"cooldown"`
	Block         string      `json:"block"`
	LastClock     time.Time   `json:"last_clock"`
	LastAdmission time.Time   `json:"last_admission"`
}

type Status struct {
	Admissions   int       `json:"admissions"`
	Budget       int       `json:"budget"`
	DelaySeconds float64   `json:"delay_seconds"`
	Cooldown     time.Time `json:"cooldown"`
	Block        string    `json:"block"`
}

type Lease struct {
	policy  *Policy
	storage *store
	state   State
}

func failure(code int, message string) error { return types.NewError(code, message) }

func New(root string, cfg Config) (*Policy, error) {
	if cfg.Delay == 0 {
		cfg.Delay = 5 * time.Second
	}
	if cfg.Budget == 0 {
		cfg.Budget = 100
	}
	if cfg.Delay < 5*time.Second || cfg.Delay > MaxDelay || cfg.Budget < 1 || cfg.Budget > 100 {
		return nil, failure(2, "safety configuration requires representable delay >= 5 seconds and budget in [1,100]")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Wait == nil {
		cfg.Wait = wait
	}
	return &Policy{root: root, cfg: cfg, lockTimeout: 30 * time.Second}, nil
}

func Default(cfg Config) (*Policy, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, stateError()
	}
	return New(filepath.Join(home, ".local", "share", "lnk", "safety"), cfg)
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return failure(4, "safety wait canceled")
	case <-timer.C:
		return nil
	}
}

func (p *Policy) Delay() time.Duration { return p.cfg.Delay }

func (p *Policy) lock(ctx context.Context) (*Lease, error) {
	if ctx.Err() != nil {
		return nil, failure(4, "safety request canceled")
	}
	s, err := openStore(p.root)
	if err != nil {
		return nil, err
	}
	acquired := false
	defer func() {
		if !acquired {
			s.close()
		}
	}()
	deadline := time.Now().Add(p.lockTimeout)
	for {
		if ctx.Err() != nil {
			return nil, failure(4, "safety request canceled")
		}
		err = unix.Flock(int(s.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return nil, stateError()
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, failure(5, "safety request lock timed out")
		}
		if err = p.cfg.Wait(ctx, min(remaining, 25*time.Millisecond)); err != nil {
			return nil, err
		}
	}
	l := &Lease{policy: p, storage: s}
	if err = l.load(); err != nil {
		unix.Flock(int(s.lock.Fd()), unix.LOCK_UN)
		return nil, err
	}
	acquired = true
	return l, nil
}

func (l *Lease) Close() { unix.Flock(int(l.storage.lock.Fd()), unix.LOCK_UN); l.storage.close() }

func (l *Lease) load() error {
	marker, me := l.storage.read("initialized")
	data, se := l.storage.read("state.json")
	if errors.Is(me, os.ErrNotExist) && errors.Is(se, os.ErrNotExist) {
		if err := l.storage.create("initialized", []byte("lnk-safety-v2\n")); err != nil {
			return err
		}
		l.state = State{Version: 2, Admissions: []time.Time{}}
		return l.save()
	}
	if me != nil || se != nil || string(marker) != "lnk-safety-v2\n" {
		return stateError()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields) != 6 {
		return stateError()
	}
	for _, key := range []string{"version", "admissions", "cooldown", "block", "last_clock", "last_admission"} {
		if _, present := fields[key]; !present {
			return stateError()
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&l.state) != nil {
		return stateError()
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return stateError()
	}
	s := l.state
	if s.Version != 2 || s.Admissions == nil || len(s.Admissions) > 100 {
		return stateError()
	}
	switch s.Block {
	case "", "auth", "retry_after_overflow":
	default:
		return stateError()
	}
	if string(fields["last_admission"]) == "null" || !validTime(s.LastAdmission) || s.LastAdmission.After(s.LastClock) || !validTime(s.LastClock) || !validTime(s.Cooldown) {
		return stateError()
	}
	for i, admission := range s.Admissions {
		if admission.IsZero() || !validTime(admission) || admission.After(s.LastClock) || (i > 0 && admission.Before(s.Admissions[i-1])) {
			return stateError()
		}
	}
	if n := len(s.Admissions); n > 0 {
		if !s.LastAdmission.Equal(s.Admissions[n-1]) {
			return stateError()
		}
	} else if !s.LastAdmission.IsZero() && s.LastAdmission.After(s.LastClock.Add(-24*time.Hour)) {
		return stateError()
	}
	if !s.Cooldown.IsZero() && s.LastClock.IsZero() {
		return stateError()
	}
	return nil
}

func validTime(t time.Time) bool { return t.IsZero() || (t.Year() >= 1970 && t.Year() <= 9999) }

func (l *Lease) save() error {
	b, err := json.Marshal(l.state)
	if err != nil {
		return stateError()
	}
	return l.storage.write(b)
}

func (l *Lease) observe() (time.Time, error) {
	now := l.policy.cfg.Now().UTC()
	if !validTime(now) || now.IsZero() || now.Before(l.state.LastClock) {
		return time.Time{}, failure(4, "safety clock moved backward or is invalid; review system time")
	}
	l.state.LastClock = now
	first := 0
	for first < len(l.state.Admissions) && !l.state.Admissions[first].After(now.Add(-24*time.Hour)) {
		first++
	}
	l.state.Admissions = append([]time.Time{}, l.state.Admissions[first:]...)
	if err := l.save(); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

func (l *Lease) blocked(now time.Time) error {
	if l.state.Block == "retry_after_overflow" {
		return failure(5, "safety Retry-After exceeds supported time; manual review required")
	}
	if l.state.Block != "" {
		return failure(3, "safety manual review required; inspect official app before safety resume --confirm")
	}
	if now.Before(l.state.Cooldown) {
		return failure(5, "safety cooldown active")
	}
	return nil
}

func (p *Policy) Admit(ctx context.Context) (*Lease, error) {
	l, err := p.lock(ctx)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			l.Close()
		}
	}()
	for {
		now, err := l.observe()
		if err != nil {
			return nil, err
		}
		if err = l.blocked(now); err != nil {
			return nil, err
		}
		if len(l.state.Admissions) >= p.cfg.Budget {
			return nil, failure(5, "safety rolling request budget exhausted")
		}
		if !l.state.LastAdmission.IsZero() {
			delay := l.state.LastAdmission.Add(p.cfg.Delay).Sub(now)
			if delay > 0 {
				if err = p.cfg.Wait(ctx, delay); err != nil {
					return nil, err
				}
				continue
			}
		}
		if ctx.Err() != nil {
			return nil, failure(4, "safety request canceled")
		}
		l.state.Admissions = append(l.state.Admissions, now)
		l.state.LastAdmission = now
		if err = l.save(); err != nil {
			return nil, err
		}
		success = true
		return l, nil
	}
}

func retryDeadline(raw string, now time.Time) (time.Time, bool) {
	floor := now.Add(30 * time.Minute)
	if !validTime(floor) {
		return time.Time{}, false
	}
	raw = strings.TrimSpace(raw)
	if raw != "" && strings.Trim(raw, "0123456789") == "" {
		seconds, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || seconds > uint64(math.MaxInt64/int64(time.Second)) {
			return time.Time{}, false
		}
		result := now.Add(time.Duration(seconds) * time.Second)
		if !validTime(result) {
			return time.Time{}, false
		}
		if result.After(floor) {
			return result, true
		}
		return floor, true
	}
	if date, err := http.ParseTime(raw); err == nil {
		if !validTime(date) {
			return time.Time{}, false
		}
		if date.After(floor) {
			return date.UTC(), true
		}
	}
	return floor, true
}

func (l *Lease) Refusal(status int, authRedirect bool, retryAfter string) error {
	if status != 429 && status != 401 && status != 403 && status != 999 && !authRedirect {
		return nil
	}
	now := l.policy.cfg.Now().UTC()
	rollback := !validTime(now) || now.IsZero() || now.Before(l.state.LastClock)
	if rollback {
		now = l.state.LastClock
	} else {
		l.state.LastClock = now
	}
	code := 3
	if status == 429 {
		code = 5
		deadline, ok := retryDeadline(retryAfter, now)
		if !ok {
			l.state.Block = "retry_after_overflow"
		} else if deadline.After(l.state.Cooldown) {
			l.state.Cooldown = deadline
		}
	} else if l.state.Block != "retry_after_overflow" {
		l.state.Block = "auth"
	}
	if err := l.save(); err != nil {
		return err
	}
	if rollback {
		return failure(4, "safety clock moved backward; refusal persisted")
	}
	return failure(code, "LinkedIn refused request; safety stop persisted")
}

func (p *Policy) Status(ctx context.Context) (Status, error) { return p.inspect(ctx, false) }
func (p *Policy) Resume(ctx context.Context, confirmed bool) (Status, error) {
	if !confirmed {
		return Status{}, failure(2, "safety resume requires --confirm after manual review")
	}
	return p.inspect(ctx, true)
}
func (p *Policy) inspect(ctx context.Context, resume bool) (Status, error) {
	l, err := p.lock(ctx)
	if err != nil {
		return Status{}, err
	}
	defer l.Close()
	if _, err = l.observe(); err != nil {
		return Status{}, err
	}
	if resume {
		if l.state.Block == "retry_after_overflow" {
			return Status{}, failure(5, "safety Retry-After requires manual state review; resume cannot clear it")
		}
		l.state.Block = ""
		if err = l.save(); err != nil {
			return Status{}, err
		}
	}
	return Status{Admissions: len(l.state.Admissions), Budget: p.cfg.Budget, DelaySeconds: p.cfg.Delay.Seconds(), Cooldown: l.state.Cooldown, Block: l.state.Block}, nil
}
