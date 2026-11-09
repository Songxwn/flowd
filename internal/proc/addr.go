package proc

import (
	"encoding/binary"
	"net/netip"
	"strconv"
	"strings"
)

func parseNetLine(proto, line string) (Key, uint64, bool) {
	f := strings.Fields(line)
	if len(f) < 10 || strings.Contains(f[0], "local_address") || strings.Contains(line, "local_address") {
		return Key{}, 0, false
	}
	if proto == "tcp" {
		switch f[3] {
		case "01", "02", "04", "05":
		default:
			return Key{}, 0, false
		}
	}
	local, lp, ok := parseProcAddr(f[1])
	if !ok {
		return Key{}, 0, false
	}
	remote, rp, ok := parseProcAddr(f[2])
	if !ok {
		return Key{}, 0, false
	}
	inode, err := strconv.ParseUint(f[9], 10, 64)
	if err != nil || inode == 0 {
		return Key{}, 0, false
	}
	return Key{Proto: proto, Local: local, LocalPort: lp, Remote: remote, RemotePort: rp}, inode, true
}

func parseProcAddr(s string) (netip.Addr, uint16, bool) {
	ipHex, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return netip.Addr{}, 0, false
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return netip.Addr{}, 0, false
	}
	switch len(ipHex) {
	case 8:
		v, err := strconv.ParseUint(ipHex, 16, 32)
		if err != nil {
			return netip.Addr{}, 0, false
		}
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(v))
		return netip.AddrFrom4(b), uint16(port), true
	case 32:
		var b [16]byte
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(ipHex[i*8:(i+1)*8], 16, 32)
			if err != nil {
				return netip.Addr{}, 0, false
			}
			binary.LittleEndian.PutUint32(b[i*4:(i+1)*4], uint32(v))
		}
		return netip.AddrFrom16(b).Unmap(), uint16(port), true
	default:
		return netip.Addr{}, 0, false
	}
}
