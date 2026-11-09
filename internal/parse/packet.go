package parse

import "net/netip"

// Frame 是从以太网帧里取出的一条 TCP、UDP 或 ICMP。
type Frame struct {
	Proto   string
	Src     netip.Addr
	Dst     netip.Addr
	SrcPort uint16
	DstPort uint16
	Payload []byte
	Flags   byte
	Bytes   int
}

// ParseFrame 解析单层 VLAN、IPv4/IPv6、TCP/UDP 和 ICMP。其它协议返回 false。
func ParseFrame(frame []byte) (Frame, bool) {
	if len(frame) < 14 {
		return Frame{}, false
	}
	etype := uint16(frame[12])<<8 | uint16(frame[13])
	l3 := 14
	if etype == 0x8100 || etype == 0x88a8 {
		if len(frame) < 18 {
			return Frame{}, false
		}
		etype = uint16(frame[16])<<8 | uint16(frame[17])
		l3 = 18
	}
	switch etype {
	case 0x0800:
		return parseIPv4(frame[l3:])
	case 0x86dd:
		return parseIPv6(frame[l3:])
	default:
		return Frame{}, false
	}
}

func parseIPv4(pkt []byte) (Frame, bool) {
	if len(pkt) < 20 || pkt[0]>>4 != 4 {
		return Frame{}, false
	}
	ihl := int(pkt[0]&0x0f) * 4
	if ihl < 20 || len(pkt) < ihl {
		return Frame{}, false
	}
	frag := int(pkt[6])<<8 | int(pkt[7])
	if frag&0x1fff != 0 {
		return Frame{}, false
	}
	total := int(pkt[2])<<8 | int(pkt[3])
	if total < ihl {
		total = len(pkt)
	}
	src, ok := netip.AddrFromSlice(pkt[12:16])
	if !ok {
		return Frame{}, false
	}
	dst, ok := netip.AddrFromSlice(pkt[16:20])
	if !ok {
		return Frame{}, false
	}
	return parseL4(pkt[ihl:], int(pkt[9]), src, dst, total)
}

func parseIPv6(pkt []byte) (Frame, bool) {
	if len(pkt) < 40 || pkt[0]>>4 != 6 {
		return Frame{}, false
	}
	payloadLen := int(pkt[4])<<8 | int(pkt[5])
	total := 40 + payloadLen
	if payloadLen == 0 || total > len(pkt)+65535 {
		total = len(pkt)
	}
	src, ok := netip.AddrFromSlice(pkt[8:24])
	if !ok {
		return Frame{}, false
	}
	dst, ok := netip.AddrFromSlice(pkt[24:40])
	if !ok {
		return Frame{}, false
	}
	next := int(pkt[6])
	off := 40
	for i := 0; i < 6; i++ {
		switch next {
		case 6, 17, 1, 58:
			return parseL4(pkt[off:], next, src.Unmap(), dst.Unmap(), total)
		case 0, 43, 60:
			if off+2 > len(pkt) {
				return Frame{}, false
			}
			hdr := (int(pkt[off+1]) + 1) * 8
			if hdr < 8 || off+hdr > len(pkt) {
				return Frame{}, false
			}
			next = int(pkt[off])
			off += hdr
		case 44:
			if off+8 > len(pkt) {
				return Frame{}, false
			}
			fragOff := (int(pkt[off+2])<<8 | int(pkt[off+3])) >> 3
			next = int(pkt[off])
			off += 8
			if fragOff != 0 {
				return Frame{}, false
			}
		default:
			return Frame{}, false
		}
	}
	return Frame{}, false
}

func parseL4(pkt []byte, proto int, src, dst netip.Addr, wire int) (Frame, bool) {
	src, dst = src.Unmap(), dst.Unmap()
	fr := Frame{Src: src, Dst: dst, Bytes: wire}
	switch proto {
	case 6:
		if len(pkt) < 20 {
			return Frame{}, false
		}
		dataOff := int(pkt[12]>>4) * 4
		if dataOff < 20 || len(pkt) < dataOff {
			return Frame{}, false
		}
		fr.Proto = "tcp"
		fr.SrcPort = uint16(pkt[0])<<8 | uint16(pkt[1])
		fr.DstPort = uint16(pkt[2])<<8 | uint16(pkt[3])
		fr.Flags = pkt[13]
		fr.Payload = pkt[dataOff:]
	case 17:
		if len(pkt) < 8 {
			return Frame{}, false
		}
		fr.Proto = "udp"
		fr.SrcPort = uint16(pkt[0])<<8 | uint16(pkt[1])
		fr.DstPort = uint16(pkt[2])<<8 | uint16(pkt[3])
		fr.Payload = pkt[8:]
	case 1, 58:
		if len(pkt) < 8 {
			return Frame{}, false
		}
		fr.Proto = "icmp"
	default:
		return Frame{}, false
	}
	if fr.Bytes < 1 {
		fr.Bytes = len(pkt)
	}
	return fr, true
}
