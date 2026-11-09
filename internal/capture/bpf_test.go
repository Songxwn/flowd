package capture

import (
	"testing"

	"golang.org/x/net/bpf"
)

func TestCaptureFilter(t *testing.T) {
	raw, err := captureFilter()
	if err != nil {
		t.Fatal(err)
	}
	insts, ok := bpf.Disassemble(raw)
	if !ok {
		t.Fatal("过滤器无法反汇编")
	}
	vm, err := bpf.NewVM(insts)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		pkt    []byte
		accept bool
	}{
		{"ipv4 tcp", pkt(0x0800, 23, 6), true},
		{"ipv4 udp", pkt(0x0800, 23, 17), true},
		{"ipv4 icmp", pkt(0x0800, 23, 1), true},
		{"arp", pkt(0x0806, 23, 6), false},
		{"ipv6 tcp", pkt(0x86dd, 20, 6), true},
		{"ipv6 udp", pkt(0x86dd, 20, 17), true},
		{"ipv6 icmp", pkt(0x86dd, 20, 58), true},
	}
	for _, tc := range cases {
		n, err := vm.Run(tc.pkt)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if (n > 0) != tc.accept {
			t.Fatalf("%s 接受=%v 返回 %d", tc.name, tc.accept, n)
		}
	}
}

func pkt(ether uint16, setAt int, value byte) []byte {
	b := make([]byte, 64)
	b[12] = byte(ether >> 8)
	b[13] = byte(ether)
	b[setAt] = value
	return b
}
