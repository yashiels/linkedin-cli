package safety

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func linuxACL(ownerPermissions uint16, namedUID uint32) []byte {
	data := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(data[:4], 2)
	for i, entry := range []struct {
		tag, permissions uint16
		id               uint32
	}{
		{0x01, ownerPermissions, 0xffffffff}, {0x02, 0, namedUID}, {0x04, 0, 0xffffffff}, {0x10, 0, 0xffffffff}, {0x20, 0, 0xffffffff},
	} {
		offset := 4 + i*8
		binary.LittleEndian.PutUint16(data[offset:offset+2], entry.tag)
		binary.LittleEndian.PutUint16(data[offset+2:offset+4], entry.permissions)
		binary.LittleEndian.PutUint32(data[offset+4:offset+8], entry.id)
	}
	return data
}

func TestNativeLinuxACLRefusal(t *testing.T) {
	for _, kind := range []string{"access", "directory-default"} {
		t.Run(kind, func(t *testing.T) {
			p, _ := fixture(t, 100)
			admit(t, p)
			statePath := filepath.Join(p.root, "state.json")
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			name := statePath
			attribute := "system.posix_acl_access"
			mode := uint32(0600)
			fileType := uint32(unix.S_IFREG)
			permissions := uint16(6)
			if kind == "directory-default" {
				name = p.root
				attribute = "system.posix_acl_default"
				mode = 0700
				fileType = unix.S_IFDIR
				permissions = 7
			}
			fd, err := unix.Open(name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(fd)
			if os.Getuid() != os.Geteuid() {
				t.Fatal("native ACL fixture requires matching real/effective UID")
			}
			acl := linuxACL(permissions, uint32(os.Geteuid())+1)
			if err = unix.Fsetxattr(fd, attribute, acl, 0); err != nil {
				t.Fatalf("supported Linux filesystem must exercise native %s ACL: %v", kind, err)
			}
			defer unix.Fremovexattr(fd, attribute)
			var stat unix.Stat_t
			if err = unix.Fstat(fd, &stat); err != nil {
				t.Fatal(err)
			}
			if stat.Uid != uint32(os.Geteuid()) || uint32(stat.Mode)&07777 != mode || uint32(stat.Mode)&unix.S_IFMT != fileType || (fileType != unix.S_IFDIR && stat.Nlink != 1) || (fileType == unix.S_IFDIR && stat.Nlink == 0) {
				t.Fatalf("ACL fixture rejected before ACL check: uid=%d mode=%o links=%d", stat.Uid, stat.Mode, stat.Nlink)
			}
			buffer := make([]byte, 1024)
			n, err := unix.Fgetxattr(fd, attribute, buffer)
			if err != nil || n < 44 || binary.LittleEndian.Uint32(buffer[:4]) != 2 {
				t.Fatalf("native ACL not installed: bytes=%d error=%v", n, err)
			}
			err = validateACL(fd)
			code(t, err, 4)
			if !strings.Contains(err.Error(), "ACL entries") {
				t.Fatalf("native ACL guard not reached: %v", err)
			}
			exchanges := 0
			lease, err := p.Admit(context.Background())
			if err == nil {
				exchanges++
				lease.Close()
			}
			if exchanges != 0 {
				t.Fatalf("unsafe ACL admitted %d exchanges: %v", exchanges, err)
			}
			code(t, err, 4)
			afterState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeState, afterState) {
				t.Fatal("refused ACL admission changed persisted state")
			}
			if err = unix.Fremovexattr(fd, attribute); err != nil {
				t.Fatalf("remove injected ACL: %v", err)
			}
			lease, err = p.Admit(context.Background())
			if err != nil {
				t.Fatalf("valid-state control refused after removing injected ACL: %v", err)
			}
			lease.Close()
			if got := len(readState(t, p).Admissions); got != 2 {
				t.Fatalf("valid-state control admission count=%d, want 2", got)
			}
		})
	}
}
