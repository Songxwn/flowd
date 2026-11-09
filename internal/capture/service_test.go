package capture

import (
	"net/netip"
	"testing"
	"time"

	"github.com/Songxwn/flowd/internal/filter"
	"github.com/Songxwn/flowd/internal/model"
)

func TestObserveSNIAndDNS(t *testing.T) {
	out := make(chan model.Record, 4)
	s := New(out, "host1")
	s.idle = time.Minute
	s.exportEvery = time.Hour
	now := time.Unix(1_700_000_000, 0).UTC()

	resp := dnsResponse("www.example.com", [4]byte{1, 2, 3, 4}, 600)
	dnsFrame := ethIPv4UDP([4]byte{10, 0, 0, 8}, [4]byte{10, 0, 0, 5}, 53, 53000, resp)
	recs := s.Observe("eth0", dnsFrame, now)
	if len(recs) != 1 || recs[0].Kind != "dns" || recs[0].Domain != "www.example.com" || recs[0].SrcIP != "10.0.0.5" {
		t.Fatalf("dns %#v", recs)
	}

	hello := ethIPv4TCP([4]byte{10, 0, 0, 5}, [4]byte{9, 9, 9, 9}, 54321, 443, clientHello("cdn.example.net"), 0x18)
	if recs := s.Observe("eth0", hello, now); len(recs) != 0 {
		t.Fatal("SNI 不应立刻生成连接日志")
	}
	got := s.ExportDue(now.Add(time.Minute))
	if len(got) != 1 {
		t.Fatalf("导出 %d 条", len(got))
	}
	if got[0].Domain != "cdn.example.net" || got[0].DomainFrom != "sni" || got[0].DstPort != 443 || got[0].Iface != "eth0" {
		t.Fatalf("%+v", got[0])
	}

	plain := ethIPv4TCP([4]byte{10, 0, 0, 5}, [4]byte{1, 2, 3, 4}, 54444, 443, nil, 0x10)
	s.Observe("eth0", plain, now)
	got = s.ExportDue(now.Add(time.Minute))
	if len(got) != 1 || got[0].Domain != "www.example.com" || got[0].DomainFrom != "dns" {
		t.Fatalf("dns 关联 %+v", got)
	}

	s.filter = filter.New(nil, []string{"example.com"}, nil, nil)
	s.Observe("eth0", plain, now)
	if got = s.ExportDue(now.Add(2 * time.Minute)); len(got) != 0 {
		t.Fatalf("排除后仍导出 %+v", got)
	}
}

func dnsResponse(name string, ip [4]byte, ttl uint32) []byte {
	q := dnsQuery(name)
	q[2] = 0x81
	q[7] = 1
	ans := []byte{0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01, byte(ttl >> 24), byte(ttl >> 16), byte(ttl >> 8), byte(ttl), 0x00, 0x04}
	ans = append(ans, ip[:]...)
	return append(q, ans...)
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

func clientHello(serverName string) []byte {
	name := []byte(serverName)
	list := append([]byte{0x00, byte(len(name) >> 8), byte(len(name))}, name...)
	bodyList := append([]byte{byte(len(list) >> 8), byte(len(list))}, list...)
	ext := append([]byte{0x00, 0x00, byte(len(bodyList) >> 8), byte(len(bodyList))}, bodyList...)
	extBlock := append([]byte{byte(len(ext) >> 8), byte(len(ext))}, ext...)
	body := []byte{0x03, 0x03}
	body = append(body, make([]byte, 32)...)
	body = append(body, 0x00, 0x00, 0x02, 0x00, 0x2f, 0x01, 0x00)
	body = append(body, extBlock...)
	hs := append([]byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}, body...)
	rec := append([]byte{0x16, 0x03, 0x01, byte(len(hs) >> 8), byte(len(hs))}, hs...)
	return rec
}

func ethIPv4TCP(src, dst [4]byte, sp, dp uint16, payload []byte, flags byte) []byte {
	return ethIPv4(6, src, dst, sp, dp, payload, flags)
}

func ethIPv4UDP(src, dst [4]byte, sp, dp uint16, payload []byte) []byte {
	return ethIPv4(17, src, dst, sp, dp, payload, 0)
}

func ethIPv4(proto byte, src, dst [4]byte, sp, dp uint16, payload []byte, flags byte) []byte {
	l4 := 8
	if proto == 6 {
		l4 = 20
	}
	ipLen := 20 + l4 + len(payload)
	buf := make([]byte, 14+ipLen)
	buf[12], buf[13] = 0x08, 0x00
	ip := buf[14:]
	ip[0] = 0x45
	ip[2], ip[3] = byte(ipLen>>8), byte(ipLen)
	ip[8] = 64
	ip[9] = proto
	copy(ip[12:16], src[:])
	copy(ip[16:20], dst[:])
	l4b := ip[20:]
	l4b[0], l4b[1] = byte(sp>>8), byte(sp)
	l4b[2], l4b[3] = byte(dp>>8), byte(dp)
	if proto == 6 {
		l4b[12] = 5 << 4
		l4b[13] = flags
		copy(l4b[20:], payload)
	} else if proto == 17 {
		n := 8 + len(payload)
		l4b[4], l4b[5] = byte(n>>8), byte(n)
		copy(l4b[8:], payload)
	}
	return buf
}

func TestMakeKeyStable(t *testing.T) {
	a := netip.MustParseAddr("10.0.0.1")
	b := netip.MustParseAddr("1.1.1.1")
	k1 := makeKey("eth0", "tcp", a, b, 1, 2)
	k2 := makeKey("eth0", "tcp", b, a, 2, 1)
	if k1 != k2 {
		t.Fatalf("%+v %+v", k1, k2)
	}
}
