package filter

import "testing"

func TestAllow(t *testing.T) {
	m := New([]string{"example.com"}, []string{"ads.example.com"}, []string{"fire"}, nil, nil, nil, nil, nil)
	if !m.Allow("www.example.com", "firefox") {
		t.Fatal("子域名和应用应通过")
	}
	if m.Allow("ads.example.com", "firefox") {
		t.Fatal("排除域名应拒绝")
	}
	if m.Allow("example.com", "curl") {
		t.Fatal("应用不匹配应拒绝")
	}
	if m.Allow("", "firefox") {
		t.Fatal("空域名不应通过包含规则")
	}
	open := New(nil, nil, nil, []string{"chrome"}, nil, nil, nil, nil)
	if !open.Allow("a.com", "firefox") {
		t.Fatal("仅排除其他应用时不应拒绝")
	}
	if open.Allow("a.com", "Google Chrome") {
		t.Fatal("排除应用按包含匹配")
	}
}

func TestAllowL4(t *testing.T) {
	m := New(nil, nil, nil, nil, []string{"tcp", "icmp"}, nil, []uint16{443, 53}, []uint16{853})
	if !m.AllowL4("tcp", 50000, 443) {
		t.Fatal("443 应保留")
	}
	if m.AllowL4("tcp", 853, 443) {
		t.Fatal("排除端口优先")
	}
	if m.AllowL4("udp", 50000, 443) {
		t.Fatal("未包含的协议应拒绝")
	}
	if !m.AllowL4("icmp", 0, 0) || !m.AllowL4("icmpv6", 0, 0) {
		t.Fatal("icmp 不受端口包含限制")
	}
	if m.AllowL4("tcp", 12345, 80) {
		t.Fatal("端口不在包含列表应拒绝")
	}
	blocked := New(nil, nil, nil, nil, nil, []string{"icmp"}, nil, nil)
	if blocked.AllowL4("icmp", 0, 0) || !blocked.AllowL4("udp", 53, 53000) {
		t.Fatal("排除协议应只拒绝 icmp")
	}
}
