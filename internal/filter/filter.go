package filter

import "strings"

// Matcher 决定一条流量是否写入日志。
// 域名按后缀匹配，应用名按不区分大小写的包含匹配。
// 协议只接受 tcp、udp、icmp。端口只作用于 tcp 和 udp，源端口或目标端口命中即算。
// 排除优先于包含。包含列表为空表示这一项不限制。
type Matcher struct {
	incDom []string
	excDom []string
	incApp []string
	excApp []string
	incProto map[string]struct{}
	excProto map[string]struct{}
	incPort map[uint16]struct{}
	excPort map[uint16]struct{}
}

func New(incDom, excDom, incApp, excApp, incProto, excProto []string, incPort, excPort []uint16) Matcher {
	return Matcher{
		incDom:   append([]string(nil), incDom...),
		excDom:   append([]string(nil), excDom...),
		incApp:   append([]string(nil), incApp...),
		excApp:   append([]string(nil), excApp...),
		incProto: protoSet(incProto),
		excProto: protoSet(excProto),
		incPort:  portSet(incPort),
		excPort:  portSet(excPort),
	}
}

func (m Matcher) Allow(domain, app string) bool {
	if matchDomain(domain, m.excDom) || matchApp(app, m.excApp) {
		return false
	}
	if len(m.incDom) > 0 && !matchDomain(domain, m.incDom) {
		return false
	}
	if len(m.incApp) > 0 && !matchApp(app, m.incApp) {
		return false
	}
	return true
}

// AllowL4 按协议和端口决定要不要继续跟踪这条流量。
func (m Matcher) AllowL4(proto string, srcPort, dstPort uint16) bool {
	proto = normProto(proto)
	if proto == "" {
		return false
	}
	if _, ok := m.excProto[proto]; ok {
		return false
	}
	if len(m.incProto) > 0 {
		if _, ok := m.incProto[proto]; !ok {
			return false
		}
	}
	if proto != "tcp" && proto != "udp" {
		return true
	}
	if _, ok := m.excPort[srcPort]; ok {
		return false
	}
	if _, ok := m.excPort[dstPort]; ok {
		return false
	}
	if len(m.incPort) == 0 {
		return true
	}
	_, srcOK := m.incPort[srcPort]
	_, dstOK := m.incPort[dstPort]
	return srcOK || dstOK
}

func normProto(proto string) string {
	switch strings.ToLower(strings.TrimSpace(proto)) {
	case "tcp", "udp", "icmp":
		return strings.ToLower(strings.TrimSpace(proto))
	case "icmp6", "icmpv6":
		return "icmp"
	default:
		return ""
	}
}

func protoSet(in []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range in {
		if proto := normProto(item); proto != "" {
			out[proto] = struct{}{}
		}
	}
	return out
}

func portSet(in []uint16) map[uint16]struct{} {
	out := map[uint16]struct{}{}
	for _, port := range in {
		if port == 0 {
			continue
		}
		out[port] = struct{}{}
	}
	return out
}

func matchDomain(domain string, patterns []string) bool {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" {
		return false
	}
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		p = strings.TrimPrefix(p, "*.")
		p = strings.TrimPrefix(p, ".")
		p = strings.TrimSuffix(p, ".")
		if p == "" {
			continue
		}
		if domain == p || strings.HasSuffix(domain, "."+p) {
			return true
		}
	}
	return false
}

func matchApp(app string, patterns []string) bool {
	app = strings.ToLower(strings.TrimSpace(app))
	if app == "" {
		return false
	}
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if strings.Contains(app, p) {
			return true
		}
	}
	return false
}
