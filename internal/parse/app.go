package parse

import "strings"

// TLSServerName 从一条 TCP 载荷里读取 TLS ClientHello 的 SNI。
// 载荷被截断时，只要 SNI 扩展完整就返回域名。
func TLSServerName(payload []byte) string {
	if len(payload) < 5 || payload[0] != 0x16 {
		return ""
	}
	hs := payload[5:]
	if len(hs) < 4 || hs[0] != 1 {
		return ""
	}
	hsLen := int(hs[1])<<16 | int(hs[2])<<8 | int(hs[3])
	body := hs[4:]
	if hsLen > 0 && len(body) > hsLen {
		body = body[:hsLen]
	}
	if len(body) < 35 {
		return ""
	}
	i := 34
	if i >= len(body) {
		return ""
	}
	sidLen := int(body[i])
	i++
	if sidLen > 32 || i+sidLen > len(body) {
		return ""
	}
	i += sidLen
	if i+2 > len(body) {
		return ""
	}
	csLen := int(body[i])<<8 | int(body[i+1])
	i += 2
	if csLen < 2 || csLen > 2048 || i+csLen > len(body) {
		return ""
	}
	i += csLen
	if i+1 > len(body) {
		return ""
	}
	compLen := int(body[i])
	i++
	if compLen < 1 || compLen > 32 || i+compLen > len(body) {
		return ""
	}
	i += compLen
	if i+2 > len(body) {
		return ""
	}
	extLen := int(body[i])<<8 | int(body[i+1])
	i += 2
	extEnd := i + extLen
	if extEnd > len(body) {
		extEnd = len(body)
	}
	for i+4 <= extEnd {
		typ := int(body[i])<<8 | int(body[i+1])
		n := int(body[i+2])<<8 | int(body[i+3])
		i += 4
		if n < 0 || i+n > extEnd {
			return ""
		}
		if typ == 0 {
			return serverName(body[i : i+n])
		}
		i += n
	}
	return ""
}

func serverName(b []byte) string {
	if len(b) < 5 {
		return ""
	}
	listLen := int(b[0])<<8 | int(b[1])
	if listLen > len(b)-2 {
		return ""
	}
	j := 2
	if j >= len(b) || b[j] != 0 {
		return ""
	}
	j++
	if j+2 > len(b) {
		return ""
	}
	nameLen := int(b[j])<<8 | int(b[j+1])
	j += 2
	if nameLen <= 0 || j+nameLen > len(b) {
		return ""
	}
	return InterestingDomain(string(b[j : j+nameLen]))
}

// HTTPHost 读取 HTTP/1 请求的 Host 头。
func HTTPHost(payload []byte) string {
	if len(payload) < 16 {
		return ""
	}
	n := len(payload)
	if n > 4096 {
		n = 4096
	}
	p := payload[:n]
	if !isHTTP(p) {
		return ""
	}
	rest := p
	nl := indexByte(rest, '\n')
	if nl < 0 {
		return ""
	}
	rest = rest[nl+1:]
	for lines := 0; lines < 40 && len(rest) > 0; lines++ {
		nl = indexByte(rest, '\n')
		line := rest
		if nl >= 0 {
			line = rest[:nl]
			rest = rest[nl+1:]
		} else {
			rest = nil
		}
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			break
		}
		if len(line) >= 5 && (line[0] == 'H' || line[0] == 'h') && (line[1] == 'o' || line[1] == 'O') && hasPrefixFold(line, "host:") {
			return InterestingDomain(trimPort(string(line[5:])))
		}
	}
	return ""
}

func isHTTP(p []byte) bool {
	methods := []string{"GET ", "POST ", "PUT ", "HEAD ", "DELETE ", "PATCH ", "OPTIONS ", "CONNECT "}
	for _, m := range methods {
		if len(p) >= len(m) && string(p[:len(m)]) == m {
			return true
		}
	}
	return false
}

func hasPrefixFold(line []byte, prefix string) bool {
	if len(line) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c := line[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

func trimPort(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "[") {
		return ""
	}
	if i := strings.LastIndex(s, ":"); i > 0 && strings.IndexByte(s, ':') == i {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// InterestingDomain 规范化域名，并丢掉本机名和反向解析名。
func InterestingDomain(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".")
	s = strings.ToLower(s)
	if s == "" || len(s) > 253 || strings.Contains(s, "..") {
		return ""
	}
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, "-") {
		return ""
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_':
		default:
			return ""
		}
	}
	if s == "localhost" || s == "localhost.localdomain" || strings.HasSuffix(s, ".arpa") {
		return ""
	}
	return s
}
