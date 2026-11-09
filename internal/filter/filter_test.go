package filter

import "testing"

func TestAllow(t *testing.T) {
	m := New([]string{"example.com"}, []string{"ads.example.com"}, []string{"fire"}, nil)
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
	open := New(nil, nil, nil, []string{"chrome"})
	if !open.Allow("a.com", "firefox") {
		t.Fatal("仅排除其他应用时不应拒绝")
	}
	if open.Allow("a.com", "Google Chrome") {
		t.Fatal("排除应用按包含匹配")
	}
}
