package safety

import (
	"errors"
	"golang.org/x/sys/unix"
)

func validateACL(fd int) error {
	for _, name := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		n, err := unix.Fgetxattr(fd, name, nil)
		if errors.Is(err, unix.ENODATA) {
			continue
		}
		if err != nil {
			return failure(4, "safety ACL inspection unsupported; use a supported local filesystem")
		}
		if n > 0 {
			return failure(4, "safety files must have no extended ACL entries; review local ACLs")
		}
	}
	return nil
}

func validateFilesystem(fd int) error {
	var st unix.Statfs_t
	if unix.Fstatfs(fd, &st) != nil {
		return stateError()
	}
	switch uint64(st.Type) {
	case unix.EXT4_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC, unix.TMPFS_MAGIC, unix.OVERLAYFS_SUPER_MAGIC:
		return nil
	}
	return failure(4, "safety requires a supported local filesystem")
}
