//go:build linux

package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Scan 读取 proc 根目录，把套接字 inode 对应到进程名。
func Scan(root string) (*Table, error) {
	inodes := map[uint64]Key{}
	opened := 0
	for _, spec := range []struct{ name, proto string }{
		{"net/tcp", "tcp"},
		{"net/tcp6", "tcp"},
		{"net/udp", "udp"},
		{"net/udp6", "udp"},
	} {
		b, err := os.ReadFile(filepath.Join(root, spec.name))
		if err != nil {
			continue
		}
		opened++
		for _, line := range strings.Split(string(b), "\n") {
			key, inode, ok := parseNetLine(spec.proto, line)
			if ok {
				inodes[inode] = key
			}
		}
	}
	if opened == 0 {
		return nil, os.ErrNotExist
	}
	table := &Table{Conns: make(map[Key]Info, len(inodes))}
	entries, err := os.ReadDir(root)
	if err != nil {
		return table, err
	}
	names := map[int]string{}
	lookups := 0
	for _, ent := range entries {
		if lookups > 20000 {
			break
		}
		pid, err := strconv.Atoi(ent.Name())
		if err != nil || pid <= 0 {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(root, ent.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if lookups > 20000 {
				break
			}
			lookups++
			target, err := os.Readlink(filepath.Join(root, ent.Name(), "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
				continue
			}
			inode, err := strconv.ParseUint(target[len("socket:[") : len(target)-1], 10, 64)
			if err != nil {
				continue
			}
			key, ok := inodes[inode]
			if !ok {
				continue
			}
			app, ok := names[pid]
			if !ok {
				app = appName(root, ent.Name())
				names[pid] = app
			}
			table.Conns[key] = Info{App: app, PID: pid}
		}
	}
	return table, nil
}

func appName(root, pid string) string {
	base := exeBase(root, pid)
	if base == "" {
		b, err := os.ReadFile(filepath.Join(root, pid, "comm"))
		if err != nil {
			return ""
		}
		return trimApp(strings.TrimSpace(string(b)))
	}
	if interpreter(base) {
		if script := scriptFromCmdline(filepath.Join(root, pid, "cmdline")); script != "" {
			return trimApp(base + " " + script)
		}
	}
	return trimApp(base)
}

func exeBase(root, pid string) string {
	target, err := os.Readlink(filepath.Join(root, pid, "exe"))
	if err != nil {
		return ""
	}
	target = strings.TrimSuffix(target, " (deleted)")
	base := filepath.Base(target)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func interpreter(base string) bool {
	b := strings.ToLower(base)
	switch {
	case b == "java" || b == "node" || b == "nodejs" || b == "ruby" || b == "perl" || b == "php" || b == "deno" || b == "bun":
		return true
	case strings.HasPrefix(b, "python"):
		return true
	default:
		return false
	}
}

func scriptFromCmdline(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	parts := strings.Split(string(b), "\x00")
	checked := 0
	for _, arg := range parts[1:] {
		if arg == "" {
			break
		}
		checked++
		if checked > 4 {
			break
		}
		if script := scriptName(arg); script != "" {
			return script
		}
	}
	return ""
}

func scriptName(arg string) string {
	if arg == "" || strings.HasPrefix(arg, "-") || strings.ContainsAny(arg, " \t=") || len(arg) > 180 {
		return ""
	}
	if !strings.ContainsAny(arg, `/\.`) {
		return ""
	}
	base := filepath.Base(arg)
	if base == "." || base == string(filepath.Separator) || len(base) > 40 {
		return ""
	}
	return base
}

func trimApp(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
