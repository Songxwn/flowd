package parse

import "net/netip"

// DNSMessage 是一次 DNS 查询或响应里用到的名字和地址。
type DNSMessage struct {
	Name     string
	Response bool
	Answers  []DNSAnswer
}

type DNSAnswer struct {
	IP  netip.Addr
	TTL uint32
}

// ParseDNS 解析 UDP 载荷，或带 2 字节长度前缀的 TCP DNS。
func ParseDNS(payload []byte, proto string) (DNSMessage, bool) {
	if proto == "tcp" && len(payload) >= 2 {
		n := int(payload[0])<<8 | int(payload[1])
		if n > 0 && n <= len(payload)-2 {
			payload = payload[2 : 2+n]
		}
	}
	if len(payload) < 12 || len(payload) > 4096 {
		return DNSMessage{}, false
	}
	flags := int(payload[2])<<8 | int(payload[3])
	qd := int(payload[4])<<8 | int(payload[5])
	an := int(payload[6])<<8 | int(payload[7])
	if qd < 1 || qd > 8 {
		return DNSMessage{}, false
	}
	off := 12
	var name string
	for q := 0; q < qd; q++ {
		label, next, ok := readName(payload, off)
		if !ok || next+4 > len(payload) {
			return DNSMessage{}, false
		}
		if q == 0 {
			name = label
		}
		off = next + 4
	}
	name = InterestingDomain(name)
	if name == "" {
		return DNSMessage{}, false
	}
	msg := DNSMessage{Name: name, Response: flags&0x8000 != 0}
	if !msg.Response || flags&0x000f != 0 {
		return msg, true
	}
	for a := 0; a < an && a < 16; a++ {
		_, next, ok := readName(payload, off)
		if !ok || next+10 > len(payload) {
			break
		}
		typ := int(payload[next])<<8 | int(payload[next+1])
		ttl := uint32(payload[next+4])<<24 | uint32(payload[next+5])<<16 | uint32(payload[next+6])<<8 | uint32(payload[next+7])
		rdlen := int(payload[next+8])<<8 | int(payload[next+9])
		off = next + 10
		if rdlen < 0 || off+rdlen > len(payload) {
			break
		}
		rdata := payload[off : off+rdlen]
		off += rdlen
		switch typ {
		case 1:
			if len(rdata) == 4 {
				if ip, ok := netip.AddrFromSlice(rdata); ok {
					msg.Answers = append(msg.Answers, DNSAnswer{IP: ip.Unmap(), TTL: ttl})
				}
			}
		case 28:
			if len(rdata) == 16 {
				if ip, ok := netip.AddrFromSlice(rdata); ok {
					msg.Answers = append(msg.Answers, DNSAnswer{IP: ip.Unmap(), TTL: ttl})
				}
			}
		}
	}
	return msg, true
}

func readName(msg []byte, off int) (string, int, bool) {
	if off < 0 || off >= len(msg) {
		return "", 0, false
	}
	var labels []byte
	jumps := 0
	end := off
	jumped := false
	for steps := 0; steps < 64; steps++ {
		if off >= len(msg) {
			return "", 0, false
		}
		n := int(msg[off])
		switch {
		case n == 0:
			off++
			if !jumped {
				end = off
			}
			if len(labels) == 0 {
				return "", end, true
			}
			return string(labels), end, true
		case n&0xc0 == 0xc0:
			if off+1 >= len(msg) {
				return "", 0, false
			}
			ptr := int(n&0x3f)<<8 | int(msg[off+1])
			if ptr >= len(msg) || jumps > 8 {
				return "", 0, false
			}
			if !jumped {
				end = off + 2
				jumped = true
			}
			off = ptr
			jumps++
		case n&0xc0 != 0:
			return "", 0, false
		default:
			off++
			if n > 63 || off+n > len(msg) {
				return "", 0, false
			}
			if len(labels) > 0 {
				labels = append(labels, '.')
			}
			labels = append(labels, msg[off:off+n]...)
			off += n
			if !jumped {
				end = off
			}
			if len(labels) > 253 {
				return "", 0, false
			}
		}
	}
	return "", 0, false
}
