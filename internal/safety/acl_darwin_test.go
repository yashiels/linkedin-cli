package safety

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeACLRefusesBeforeModeChecks(t *testing.T) {
	p, _ := fixture(t, 100)
	admit(t, p)
	name := filepath.Join(p.root, "state.json")
	command := exec.Command("/bin/chmod", "+a", "everyone allow read", name)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native ACL fixture unavailable: %v %s", err, output)
	}
	defer exec.Command("/bin/chmod", "-N", name).Run()
	if err := os.Chmod(name, 0600); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		t.Fatal(err)
	}
	if st.Mode&07777 != 0600 || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 {
		t.Fatal("fixture would fail mode or ownership before ACL guard")
	}
	err = validateACL(fd)
	code(t, err, 4)
	if !strings.Contains(err.Error(), "ACL entries") {
		t.Fatalf("did not reach native ACL entry guard: %v", err)
	}
	_, err = p.Admit(context.Background())
	code(t, err, 4)
}
