package game

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// platformSteamRoots returns where Steam is installed: the paths Steam
// records in the registry, then the default folder.
func platformSteamRoots() []string {
	var roots []string
	for _, k := range []struct {
		root        syscall.Handle
		path, value string
	}{
		{syscall.HKEY_CURRENT_USER, `Software\Valve\Steam`, "SteamPath"},
		{syscall.HKEY_LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"},
		{syscall.HKEY_LOCAL_MACHINE, `SOFTWARE\Valve\Steam`, "InstallPath"},
	} {
		if v := regString(k.root, k.path, k.value); v != "" {
			roots = append(roots, filepath.Clean(v)) // SteamPath uses forward slashes
		}
	}
	if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
		roots = append(roots, filepath.Join(pf, "Steam"))
	}
	return roots
}

// regString reads a string value from the registry, or "" if it is absent.
func regString(root syscall.Handle, path, name string) string {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var h syscall.Handle
	if syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ, &h) != nil {
		return ""
	}
	defer syscall.RegCloseKey(h)
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var typ uint32
	buf := make([]uint16, 1024)
	size := uint32(len(buf) * 2)
	if syscall.RegQueryValueEx(h, n, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size) != nil ||
		(typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ) {
		return ""
	}
	return syscall.UTF16ToString(buf[:size/2])
}
