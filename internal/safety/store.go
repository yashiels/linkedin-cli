package safety

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type store struct {
	dir  int
	lock *os.File
}

func stateError() error {
	return failure(4, "safety state unavailable or unsafe; review local ownership, permissions, ACLs and initialization")
}

func validateFD(fd int, directory, private bool) error {
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil {
		return stateError()
	}
	expected := uint32(unix.S_IFREG)
	if directory {
		expected = unix.S_IFDIR
	}
	if uint32(st.Mode)&unix.S_IFMT != expected || (st.Uid != uint32(os.Getuid()) && (private || st.Uid != 0)) {
		return stateError()
	}
	if private {
		mode := uint32(0600)
		if directory {
			mode = 0700
		}
		if uint32(st.Mode)&07777 != mode || (!directory && st.Nlink != 1) {
			return stateError()
		}
		if err := validateACL(fd); err != nil {
			return err
		}
	} else if st.Mode&0022 != 0 && st.Mode&unix.S_ISVTX == 0 {
		return stateError()
	}
	return nil
}

func openStore(root string) (*store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
		return nil, stateError()
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, stateError()
	}
	for _, part := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) {
			e = unix.Mkdirat(fd, part, 0700)
			if e == nil || errors.Is(e, unix.EEXIST) {
				if unix.Fsync(fd) != nil {
					unix.Close(fd)
					return nil, stateError()
				}
				next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		unix.Close(fd)
		if e != nil {
			return nil, stateError()
		}
		fd = next
		if e = validateFD(fd, true, false); e != nil {
			unix.Close(fd)
			return nil, e
		}
	}
	if err = validateFD(fd, true, true); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if err = validateFilesystem(fd); err != nil {
		unix.Close(fd)
		return nil, err
	}
	s := &store{dir: fd}
	lock, err := s.lockFile()
	if err != nil {
		s.close()
		return nil, err
	}
	s.lock = lock
	if unix.Fsync(fd) != nil {
		s.close()
		return nil, stateError()
	}
	return s, nil
}

func (s *store) lockFile() (*os.File, error) {
	fd, err := unix.Openat(s.dir, "lock", unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) {
		fd, err = unix.Openat(s.dir, "lock", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
		if errors.Is(err, unix.EEXIST) {
			fd, err = unix.Openat(s.dir, "lock", unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		}
	}
	if err != nil {
		return nil, stateError()
	}
	if err = validateFD(fd, false, true); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), "lock"), nil
}

func (s *store) close() {
	if s.lock != nil {
		s.lock.Close()
	}
	unix.Close(s.dir)
}

func (s *store) open(name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(s.dir, name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if errors.Is(err, unix.ENOENT) {
		if flags&unix.O_CREAT != 0 {
			return nil, stateError()
		}
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, stateError()
	}
	if err = validateFD(fd, false, true); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (s *store) read(name string) ([]byte, error) {
	f, err := s.open(name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 32769))
	if err != nil || len(b) > 32768 {
		return nil, stateError()
	}
	return b, nil
}

func (s *store) create(name string, b []byte) error {
	f, err := s.open(name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return err
	}
	defer f.Close()
	if unix.Fchmod(int(f.Fd()), 0600) != nil {
		return stateError()
	}
	if _, err = f.Write(b); err != nil {
		return stateError()
	}
	if f.Sync() != nil || unix.Fsync(s.dir) != nil {
		return stateError()
	}
	return nil
}

func (s *store) write(b []byte) error {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return stateError()
	}
	name := ".state-" + hex.EncodeToString(entropy[:])
	defer unix.Unlinkat(s.dir, name, 0)
	if err := s.create(name, b); err != nil {
		return err
	}
	if unix.Renameat(s.dir, name, s.dir, "state.json") != nil || unix.Fsync(s.dir) != nil {
		return stateError()
	}
	return nil
}
