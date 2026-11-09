package vlogs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Songxwn/flowd/internal/config"
	"github.com/Songxwn/flowd/internal/model"
)

// Client 写入并查询 VictoriaLogs。
type Client struct {
	base    atomic.Value
	tenant  atomic.Value
	http    *http.Client
	version string
}

type tenantIDs struct {
	Account string
	Project string
}

func New(base, id, version string) *Client {
	if version == "" {
		version = "dev"
	}
	c := &Client{
		http: &http.Client{
			Timeout: 60 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		version: version,
	}
	c.SetURL(base)
	c.SetID(id)
	return c
}

func (c *Client) SetURL(base string) {
	c.base.Store(strings.TrimRight(strings.TrimSpace(base), "/"))
}

func (c *Client) URL() string {
	v, _ := c.base.Load().(string)
	return v
}

func (c *Client) SetID(id string) {
	account, project, err := config.ParseTenantID(id)
	if err != nil {
		account, project = "0", "0"
	}
	c.tenant.Store(tenantIDs{Account: account, Project: project})
}

func (c *Client) ID() string {
	t, _ := c.tenant.Load().(tenantIDs)
	if t.Account == "" {
		return "0:0"
	}
	return t.Account + ":" + t.Project
}

func (c *Client) tenantHeaders(req *http.Request) {
	t, _ := c.tenant.Load().(tenantIDs)
	if t.Account == "" {
		t = tenantIDs{Account: "0", Project: "0"}
	}
	req.Header.Set("AccountID", t.Account)
	req.Header.Set("ProjectID", t.Project)
}

func (c *Client) Healthy(ctx context.Context) error {
	u, err := c.url("/health", nil)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "flowd/"+c.version)
	c.tenantHeaders(req)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接 VictoriaLogs 失败: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("VictoriaLogs 健康检查 HTTP %d", res.StatusCode)
	}
	return nil
}

func (c *Client) Insert(ctx context.Context, recs []model.Record) error {
	if len(recs) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range recs {
		if recs[i].Msg == "" {
			recs[i].Finish()
		}
		if err := enc.Encode(&recs[i]); err != nil {
			return err
		}
	}
	q := url.Values{}
	q.Set("_time_field", "_time")
	q.Set("_msg_field", "_msg")
	q.Set("_stream_fields", "host,iface,kind,proto")
	u, err := c.url("/insert/jsonline", q)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/stream+json")
	req.Header.Set("User-Agent", "flowd/"+c.version)
	c.tenantHeaders(req)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("写入 VictoriaLogs 失败: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("VictoriaLogs HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// LogFilter 是网页上的流日志条件。域名和应用按包含匹配。
type LogFilter struct {
	Kind   string
	Domain string
	App    string
	Iface  string
	Proto  string
	Port   string
	SrcIP  string
	DstIP  string
	Text   string
	Limit  int
}

func (f LogFilter) limit() int {
	if f.Limit <= 0 {
		return 100
	}
	if f.Limit > 500 {
		return 500
	}
	return f.Limit
}

func (c *Client) Logs(ctx context.Context, f LogFilter, start, end time.Time) ([]map[string]any, error) {
	query, extra := f.logsQL()
	limit := strconvItoa(f.limit())
	body, err := c.post(ctx, "/select/logsql/query", query+" | sort by (_time desc) | limit "+limit, extra, start, end, "")
	if err != nil && statusOf(err) == 400 {
		body, err = c.post(ctx, "/select/logsql/query", query+" | limit "+limit, extra, start, end, "")
	}
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	sc := newScanner(body)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("解析日志失败: %w", err)
		}
		out = append(out, row)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

func (c *Client) Rank(ctx context.Context, kind, field, proto, port string, start, end time.Time, limit int) ([]RankRow, error) {
	q, err := rankQuery(kind, field, proto, port)
	if err != nil {
		return nil, err
	}
	body, err := c.post(ctx, "/select/logsql/stats_query", q, nil, start, end, "")
	if err != nil {
		return nil, err
	}
	rows, err := parseRank(body, field)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (c *Client) Totals(ctx context.Context, kind, proto, port string, start, end time.Time) (float64, float64, error) {
	q := scopeQuery(kind, proto, port) + " | stats count() as hits, sum(bytes) as total_bytes"
	body, err := c.post(ctx, "/select/logsql/stats_query", q, nil, start, end, "")
	if err != nil {
		return 0, 0, err
	}
	return parseTotals(body)
}

func (c *Client) Series(ctx context.Context, kind, proto, port, by string, start, end time.Time, step string) ([]SeriesSet, error) {
	q, err := seriesQuery(kind, proto, port, by)
	if err != nil {
		return nil, err
	}
	body, err := c.post(ctx, "/select/logsql/stats_query_range", q, nil, start, end, step)
	if err != nil {
		return nil, err
	}
	if by == "" {
		points, err := parseSeries(body)
		if err != nil {
			return nil, err
		}
		if points == nil {
			points = []Point{}
		}
		return []SeriesSet{{Name: "all", Points: points}}, nil
	}
	return parseGrouped(body, by)
}

func (c *Client) post(ctx context.Context, path, query string, extra map[string]string, start, end time.Time, step string) ([]byte, error) {
	form := url.Values{}
	form.Set("query", query)
	if !start.IsZero() {
		form.Set("start", start.UTC().Format(time.RFC3339Nano))
	}
	if !end.IsZero() {
		form.Set("end", end.UTC().Add(time.Second).Format(time.RFC3339Nano))
		form.Set("time", end.UTC().Format(time.RFC3339Nano))
	}
	if step != "" {
		form.Set("step", step)
	}
	if len(extra) > 0 {
		b, err := json.Marshal(extra)
		if err != nil {
			return nil, err
		}
		form.Set("extra_filters", string(b))
	}
	u, err := c.url(path, nil)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "flowd/"+c.version)
	c.tenantHeaders(req)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询 VictoriaLogs 失败: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode/100 != 2 {
		return nil, &httpError{Status: res.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return body, nil
}

func (c *Client) url(path string, q url.Values) (string, error) {
	raw := c.URL()
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("VictoriaLogs 地址无效")
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	if q != nil {
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	body := e.Body
	if len(body) > 500 {
		body = body[:500]
	}
	if body == "" {
		return fmt.Sprintf("VictoriaLogs HTTP %d", e.Status)
	}
	return fmt.Sprintf("VictoriaLogs HTTP %d: %s", e.Status, body)
}

func statusOf(err error) int {
	if e, ok := err.(*httpError); ok {
		return e.Status
	}
	return 0
}
