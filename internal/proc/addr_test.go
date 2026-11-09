package proc

import "testing"

func TestParseProcAddr(t *testing.T) {
	ip, port, ok := parseProcAddr("0100007F:0050")
	if !ok || ip.String() != "127.0.0.1" || port != 80 {
		t.Fatalf("%s %d %v", ip, port, ok)
	}
	ip, port, ok = parseProcAddr("00000000000000000000000001000000:0035")
	if !ok || ip.String() != "::1" || port != 53 {
		t.Fatalf("%s %d %v", ip, port, ok)
	}
}

func TestParseNetLine(t *testing.T) {
	line := " 1: 0100007F:C2C2 0100007F:0277 01 00000000:00000000 00:00000000 00000000 1000 0 54321 1 0000000000000000 20 4 30 10 -1"
	key, inode, ok := parseNetLine("tcp", line)
	if !ok || inode != 54321 {
		t.Fatalf("%v %d", ok, inode)
	}
	if key.Local.String() != "127.0.0.1" || key.LocalPort != 0xC2C2 || key.RemotePort != 0x0277 {
		t.Fatalf("%+v", key)
	}
	if _, _, ok := parseNetLine("tcp", " 0: 0100007F:0277 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1 1 0 100 0 0 10 0"); ok {
		t.Fatal("LISTEN 不应计入")
	}
}
