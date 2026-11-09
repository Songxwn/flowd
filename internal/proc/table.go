package proc

import "net/netip"

// Info 是套接字所属的进程。
type Info struct {
	App string
	PID int
}

// Key 是 /proc/net 里的一条连接，本地地址在前。
type Key struct {
	Proto      string
	Local      netip.Addr
	LocalPort  uint16
	Remote     netip.Addr
	RemotePort uint16
}

// Table 是某一时刻的进程连接表。
type Table struct {
	Conns map[Key]Info
}

func (t *Table) Lookup(proto string, a netip.Addr, ap uint16, b netip.Addr, bp uint16) (Info, bool) {
	if t == nil || len(t.Conns) == 0 {
		return Info{}, false
	}
	a, b = a.Unmap(), b.Unmap()
	if info, ok := t.Conns[Key{proto, a, ap, b, bp}]; ok {
		return info, true
	}
	if info, ok := t.Conns[Key{proto, b, bp, a, ap}]; ok {
		return info, true
	}
	return Info{}, false
}
