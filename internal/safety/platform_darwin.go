package safety

import (
	"encoding/binary"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func validateACL(fd int) error {
	attrs := struct {
		Count     uint16
		Reserved  uint16
		Common    uint32
		Volume    uint32
		Directory uint32
		File      uint32
		Fork      uint32
	}{Count: 5, Common: unix.ATTR_CMN_EXTENDED_SECURITY}
	var buffer [4096]byte
	_, _, errno := syscall.Syscall6(unix.SYS_FGETATTRLIST, uintptr(fd), uintptr(unsafe.Pointer(&attrs)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0, 0)
	if errno != 0 {
		return failure(4, "safety ACL inspection unsupported; use a supported local filesystem")
	}
	size := int(binary.LittleEndian.Uint32(buffer[:4]))
	offset := 4 + int(int32(binary.LittleEndian.Uint32(buffer[4:8])))
	length := int(binary.LittleEndian.Uint32(buffer[8:12]))
	if size < 12 || size > len(buffer) {
		return stateError()
	}
	if length == 0 {
		return nil
	}
	if offset < 12 || offset+length > size {
		return stateError()
	}

	if length < 44 || binary.LittleEndian.Uint32(buffer[offset:offset+4]) != 0x012cc16d {
		return stateError()
	}
	entries := binary.LittleEndian.Uint32(buffer[offset+36 : offset+40])
	if entries != 0 && entries != 0xffffffff {
		return failure(4, "safety files must have no extended ACL entries; review local ACLs")
	}
	return nil
}

func validateFilesystem(fd int) error {
	var st unix.Statfs_t
	if unix.Fstatfs(fd, &st) != nil {
		return stateError()
	}
	name := unix.ByteSliceToString(st.Fstypename[:])
	if st.Flags&unix.MNT_LOCAL == 0 || (name != "apfs" && name != "hfs") {
		return failure(4, "safety requires a supported local APFS or HFS filesystem")
	}
	return nil
}
