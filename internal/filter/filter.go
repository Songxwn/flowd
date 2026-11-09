package filter

import "strings"

// Matcher 决定一条已识别的域名和应用是否写入日志。
// 域名按后缀匹配，应用名按不区分大小写的包含匹配。
type Matcher struct {
	incDom []string
	excDom []string
	incApp []string
	excApp []string
}

func New(incDom, excDom, incApp, excApp []string) Matcher {
	return Matcher{
		incDom: append([]string(nil), incDom...),
		excDom: append([]string(nil), excDom...),
		incApp: append([]string(nil), incApp...),
		excApp: append([]string(nil), excApp...),
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
