package vlogs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var ifaceName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)

func (f LogFilter) logsQL() (string, map[string]string) {
	var parts []string
	if clause := containsFilter("domain", f.Domain); clause != "" {
		parts = append(parts, clause)
	}
	if clause := containsFilter("app", f.App); clause != "" {
		parts = append(parts, clause)
	}
	if clause := phrase(f.Text); clause != "" {
		parts = append(parts, clause)
	}
	if clause := portFilter(f.Port); clause != "" {
		parts = append(parts, clause)
	}
	query := "*"
	if len(parts) > 0 {
		query = strings.Join(parts, " ")
	}
	extra := map[string]string{}
	if f.Kind == "flow" || f.Kind == "dns" {
		extra["kind"] = f.Kind
	}
	if ifaceName.MatchString(f.Iface) {
		extra["iface"] = f.Iface
	}
	if f.Proto == "tcp" || f.Proto == "udp" || f.Proto == "icmp" {
		extra["proto"] = f.Proto
	}
	if ipOK(f.SrcIP) {
		extra["src_ip"] = f.SrcIP
	}
	if ipOK(f.DstIP) {
		extra["dst_ip"] = f.DstIP
	}
	if len(extra) == 0 {
		extra = nil
	}
	return query, extra
}

func rankQuery(kind, field, proto, port string) (string, error) {
	switch field {
	case "domain", "app", "dst_ip", "proto", "dst_port", "src_port":
	default:
		return "", fmt.Errorf("不支持的排行字段")
	}
	q := scopeQuery(kind, proto, port)
	if field != "proto" {
		q += " " + field + ":*"
	}
	return q + fmt.Sprintf(" | stats by (%s) count() as hits, sum(bytes) as total_bytes", field), nil
}

func seriesQuery(kind, proto, port, by string) (string, error) {
	if by != "" && by != "proto" {
		return "", fmt.Errorf("不支持的曲线分组")
	}
	q := scopeQuery(kind, proto, port)
	if by == "" {
		return q + " | stats count() as hits, sum(bytes) as total_bytes", nil
	}
	return q + " | stats by (" + by + ") count() as hits, sum(bytes) as total_bytes", nil
}

func scopeQuery(kind, proto, port string) string {
	if kind != "dns" {
		kind = "flow"
	}
	q := fmt.Sprintf(`kind:="%s"`, kind)
	if proto == "tcp" || proto == "udp" || proto == "icmp" {
		q += fmt.Sprintf(` proto:="%s"`, proto)
	}
	if clause := portFilter(port); clause != "" {
		q += " " + clause
	}
	return q
}

func portFilter(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	n, err := strconv.ParseUint(value, 10, 16)
	if err != nil || n == 0 {
		return ""
	}
	return fmt.Sprintf("(src_port:%d OR dst_port:%d)", n, n)
}

func containsFilter(field, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 128 {
		value = value[:128]
	}
	return field + `:~"(?i)` + logsqlRe(value) + `"`
}

func phrase(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 128 {
		value = value[:128]
	}
	return `"` + escapeLogSQL(value) + `"`
}

func logsqlRe(value string) string {
	return escapeLogSQL(regexp.QuoteMeta(value))
}

func escapeLogSQL(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func ipOK(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		case r == '.' || r == ':' :
		default:
			return false
		}
	}
	return true
}

func strconvItoa(n int) string { return strconv.Itoa(n) }

// RankRow 是排行里的一行。
type RankRow struct {
	Key   string  `json:"key"`
	Hits  float64 `json:"hits"`
	Bytes float64 `json:"bytes"`
}

// Point 是总览曲线上的一个点。
type Point struct {
	T     int64   `json:"t"`
	Hits  float64 `json:"hits"`
	Bytes float64 `json:"bytes"`
}

type promResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string       `json:"resultType"`
		Result     []promResult `json:"result"`
	} `json:"data"`
}

type promResult struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
	Values [][]any           `json:"values"`
}

func parseRank(body []byte, field string) ([]RankRow, error) {
	resp, err := decodeProm(body)
	if err != nil {
		return nil, err
	}
	grouped := map[string]*RankRow{}
	var order []string
	for _, item := range resp.Data.Result {
		key := item.Metric[field]
		if key == "" {
			continue
		}
		row := grouped[key]
		if row == nil {
			row = &RankRow{Key: key}
			grouped[key] = row
			order = append(order, key)
		}
		_, n, ok := pair(item.Value)
		if !ok {
			continue
		}
		switch item.Metric["__name__"] {
		case "hits":
			row.Hits = n
		case "total_bytes":
			row.Bytes = n
		}
	}
	rows := make([]RankRow, 0, len(order))
	for _, key := range order {
		rows = append(rows, *grouped[key])
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Hits == rows[j].Hits {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Hits > rows[j].Hits
	})
	return rows, nil
}

func parseTotals(body []byte) (hits, bytes float64, err error) {
	resp, err := decodeProm(body)
	if err != nil {
		return 0, 0, err
	}
	for _, item := range resp.Data.Result {
		_, n, ok := pair(item.Value)
		if !ok {
			continue
		}
		switch item.Metric["__name__"] {
		case "hits":
			hits += n
		case "total_bytes":
			bytes += n
		}
	}
	return hits, bytes, nil
}

// SeriesSet 是一条已按名字分开的时间序列。
type SeriesSet struct {
	Name   string  `json:"name"`
	Points []Point `json:"points"`
}

func parseGrouped(body []byte, field string) ([]SeriesSet, error) {
	resp, err := decodeProm(body)
	if err != nil {
		return nil, err
	}
	type slot struct {
		hits  float64
		bytes float64
	}
	groups := map[string]map[int64]*slot{}
	var names []string
	var times []int64
	seenName := map[string]struct{}{}
	seenTime := map[int64]struct{}{}
	for _, item := range resp.Data.Result {
		name := item.Metric[field]
		if name == "" {
			continue
		}
		if _, ok := seenName[name]; !ok {
			seenName[name] = struct{}{}
			names = append(names, name)
			groups[name] = map[int64]*slot{}
		}
		for _, raw := range item.Values {
			ts, n, ok := pair(raw)
			if !ok {
				continue
			}
			if _, ok := seenTime[ts]; !ok {
				seenTime[ts] = struct{}{}
				times = append(times, ts)
			}
			cell := groups[name][ts]
			if cell == nil {
				cell = &slot{}
				groups[name][ts] = cell
			}
			switch item.Metric["__name__"] {
			case "hits":
				cell.hits = n
			case "total_bytes":
				cell.bytes = n
			}
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	sort.Strings(names)
	out := make([]SeriesSet, 0, len(names))
	for _, name := range names {
		points := make([]Point, 0, len(times))
		for _, ts := range times {
			cell := groups[name][ts]
			p := Point{T: ts}
			if cell != nil {
				p.Hits = cell.hits
				p.Bytes = cell.bytes
			}
			points = append(points, p)
		}
		out = append(out, SeriesSet{Name: name, Points: points})
	}
	return out, nil
}

func parseSeries(body []byte) ([]Point, error) {
	resp, err := decodeProm(body)
	if err != nil {
		return nil, err
	}
	byT := map[int64]*Point{}
	var order []int64
	for _, item := range resp.Data.Result {
		name := item.Metric["__name__"]
		for _, raw := range item.Values {
			ts, n, ok := pair(raw)
			if !ok {
				continue
			}
			p := byT[ts]
			if p == nil {
				p = &Point{T: ts}
				byT[ts] = p
				order = append(order, ts)
			}
			switch name {
			case "hits":
				p.Hits = n
			case "total_bytes":
				p.Bytes = n
			}
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]Point, 0, len(order))
	for _, ts := range order {
		out = append(out, *byT[ts])
	}
	return out, nil
}

func decodeProm(body []byte) (promResponse, error) {
	var resp promResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return promResponse{}, fmt.Errorf("解析 VictoriaLogs 统计结果失败: %w", err)
	}
	if resp.Status != "" && resp.Status != "success" {
		if resp.Error == "" {
			resp.Error = "查询失败"
		}
		return promResponse{}, fmt.Errorf("VictoriaLogs: %s", resp.Error)
	}
	return resp, nil
}

func pair(v []any) (int64, float64, bool) {
	if len(v) < 2 {
		return 0, 0, false
	}
	var ts float64
	switch n := v[0].(type) {
	case float64:
		ts = n
	default:
		return 0, 0, false
	}
	var val float64
	switch n := v[1].(type) {
	case string:
		parsed, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, 0, false
		}
		val = parsed
	case float64:
		val = n
	default:
		return 0, 0, false
	}
	if ts < 1e11 {
		ts *= 1000
	}
	return int64(ts), val, true
}

func newScanner(body []byte) *bufio.Scanner {
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	return sc
}
