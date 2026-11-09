package parse

import (
	"net/netip"
	"testing"
)

func TestTLSServerNameAndHTTPHost(t *testing.T) {
	name := TLSServerName(clientHello("Example.COM"))
	if name != "example.com" {
		t.Fatalf("sni %q", name)
	}
	host := HTTPHost([]byte("GET / HTTP/1.1\r\nHost: www.example.com:443\r\n\r\n"))
	if host != "www.example.com" {
		t.Fatalf("host %q", host)
	}
	if HTTPHost([]byte{0x16, 0x03, 0x01, 0x00, 0x01}) != "" {
		t.Fatal("二进制不应识别成 HTTP")
	}
	if InterestingDomain("1.2.3.4.in-addr.arpa") != "" {
		t.Fatal("反向解析应丢弃")
	}
}

func TestParseFrameSNI(t *testing.T) {
	payload := clientHello("www.example.com")
	frame := ethIPv4TCP([4]byte{10, 0, 0, 5}, [4]byte{1, 2, 3, 4}, 54321, 443, payload, 0x18)
	fr, ok := ParseFrame(frame)
	if !ok {
		t.Fatal("解析失败")
	}
	if fr.Proto != "tcp" || fr.SrcPort != 54321 || fr.DstPort != 443 {
		t.Fatalf("%+v", fr)
	}
	if TLSServerName(fr.Payload) != "www.example.com" {
		t.Fatal("帧内 SNI 丢失")
	}
	if fr.Bytes < len(payload) {
		t.Fatalf("bytes %d", fr.Bytes)
	}
}

func TestIPv6UDP(t *testing.T) {
	frame := make([]byte, 14+40+8+4)
	frame[12], frame[13] = 0x86, 0xdd
	ip := frame[14:]
	ip[0] = 0x60
	ip[4], ip[5] = 0x00, 12
	ip[6] = 17
	ip[8+15] = 1
	ip[24+15] = 2
	udp := ip[40:]
	udp[0], udp[1] = 0x00, 53
	udp[2], udp[3] = 0x01, 0x00
	fr, ok := ParseFrame(frame)
	if !ok || fr.Proto != "udp" || fr.SrcPort != 53 || fr.DstPort != 256 {
		t.Fatalf("ok %v %+v", ok, fr)
	}
	if fr.Src != netip.MustParseAddr("::1") || fr.Dst != netip.MustParseAddr("::2") {
		t.Fatalf("addr %s %s", fr.Src, fr.Dst)
	}
}

func TestParseDNS(t *testing.T) {
	q := dnsQuery("www.example.com")
	msg, ok := ParseDNS(q, "udp")
	if !ok || msg.Response || msg.Name != "www.example.com" {
		t.Fatalf("%+v %v", msg, ok)
	}
	resp := append([]byte(nil), q...)
	resp[2] = 0x81
	resp[7] = 1
	resp = append(resp, 0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 30, 0x00, 0x04, 1, 2, 3, 4)
	msg, ok = ParseDNS(resp, "udp")
	if !ok || !msg.Response || len(msg.Answers) != 1 || msg.Answers[0].IP.String() != "1.2.3.4" || msg.Answers[0].TTL != 30 {
		t.Fatalf("%+v", msg)
	}
	loop := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01}
	if _, ok := ParseDNS(loop, "udp"); ok {
		t.Fatal("压缩指针环应失败")
	}
}

func clientHello(serverName string) []byte {
	name := []byte(serverName)
	list := []byte{0x00, byte(len(name) >> 8), byte(len(name))}
	list = append(list, name...)
	bodyList := []byte{byte(len(list) >> 8), byte(len(list))}
	bodyList = append(bodyList, list...)
	ext := []byte{0x00, 0x00, byte(len(bodyList) >> 8), byte(len(bodyList))}
	ext = append(ext, bodyList...)
	extBlock := []byte{byte(len(ext) >> 8), byte(len(ext))}
	extBlock = append(extBlock, ext...)

	body := []byte{0x03, 0x03}
	body = append(body, make([]byte, 32)...)
	body = append(body, 0x00)
	body = append(body, 0x00, 0x02, 0x00, 0x2f)
	body = append(body, 0x01, 0x00)
	body = append(body, extBlock...)
	hs := []byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	hs = append(hs, body...)
	rec := []byte{0x16, 0x03, 0x01, byte(len(hs) >> 8), byte(len(hs))}
	return append(rec, hs...)
}

func TestParseICMP(t *testing.T) {
	buf := make([]byte, 14+20+8)
	buf[12], buf[13] = 0x08, 0x00
	ip := buf[14:]
	ip[0] = 0x45
	ip[2], ip[3] = 0, 28
	ip[9] = 1
	ip[12], ip[15] = 10, 1
	ip[16], ip[19] = 1, 1
	ip[20] = 8
	fr, ok := ParseFrame(buf)
	if !ok || fr.Proto != "icmp" || fr.SrcPort != 0 || fr.DstPort != 0 {
		t.Fatalf("%v %+v", ok, fr)
	}
}

func dnsQuery(name string) []byte {
	h := make([]byte, 12)
	h[5] = 1
	var q []byte
	for _, label := range splitDot(name) {
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0, 0, 1, 0, 1)
	return append(h, q...)
}

func splitDot(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

func ethIPv4TCP(src, dst [4]byte, sp, dp uint16, payload []byte, flags byte) []byte {
	ipLen := 20 + 20 + len(payload)
	buf := make([]byte, 14+ipLen)
	buf[12], buf[13] = 0x08, 0x00
	ip := buf[14:]
	ip[0] = 0x45
	ip[2], ip[3] = byte(ipLen>>8), byte(ipLen)
	ip[8] = 64
	ip[9] = 6
	copy(ip[12:16], src[:])
	copy(ip[16:20], dst[:])
	tcp := ip[20:]
	tcp[0], tcp[1] = byte(sp>>8), byte(sp)
	tcp[2], tcp[3] = byte(dp>>8), byte(dp)
	tcp[12] = 5 << 4
	tcp[13] = flags
	copy(tcp[20:], payload)
	return buf
}
