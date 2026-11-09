package vlogs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Songxwn/flowd/internal/model"
)

func TestLogsQLEscapesQuotes(t *testing.T) {
	q, extra := (LogFilter{Domain: `a"b\c`, Kind: "flow", Iface: "eth0"}).logsQL()
	if strings.Contains(q, `a"b`) {
		t.Fatalf("引号未转义: %s", q)
	}
	if extra["kind"] != "flow" || extra["iface"] != "eth0" {
		t.Fatalf("%v", extra)
	}
	if _, err := rankQuery("flow", "pid"); err == nil {
		t.Fatal("非法排行字段应拒绝")
	}
	q, extra := (LogFilter{Proto: "icmp", Port: "443"}).logsQL()
	if extra["proto"] != "icmp" || !strings.Contains(q, "src_port:443") || !strings.Contains(q, "dst_port:443") {
		t.Fatalf("query %s extra %v", q, extra)
	}
}

func TestParseRankAndSeries(t *testing.T) {
	rows, err := parseRank([]byte(`{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"__name__":"hits","domain":"a.com"},"value":[1700000000,"3"]},
		{"metric":{"__name__":"total_bytes","domain":"a.com"},"value":[1700000000,"90"]},
		{"metric":{"__name__":"hits","domain":"b.com"},"value":[1700000000,"8"]}
	]}}`), "domain")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Key != "b.com" || rows[0].Hits != 8 || rows[1].Bytes != 90 {
		t.Fatalf("%+v", rows)
	}
	points, err := parseSeries([]byte(`{"status":"success","data":{"resultType":"matrix","result":[
		{"metric":{"__name__":"hits"},"values":[[1700000060,"4"],[1700000000,"2"]]},
		{"metric":{"__name__":"total_bytes"},"values":[[1700000000,"10"]]}
	]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0].T != 1700000000000 || points[0].Hits != 2 || points[0].Bytes != 10 || points[1].Hits != 4 {
		t.Fatalf("%+v", points)
	}
}

func TestInsert(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "_stream_fields=host") && !strings.Contains(r.URL.RawQuery, "_stream_fields=host%2C") && !strings.Contains(r.URL.RawQuery, "_stream_fields=") {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		if r.Header.Get("AccountID") != "0" || r.Header.Get("ProjectID") != "0" {
			t.Errorf("tenant %s:%s", r.Header.Get("AccountID"), r.Header.Get("ProjectID"))
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	c := New(srv.URL, "", "test")
	rec := model.Record{Time: time.Unix(10, 0).UTC(), Host: "h", Kind: "dns", Iface: "eth0", Proto: "udp", Domain: "example.com"}
	rec.Finish()
	if err := c.Insert(context.Background(), []model.Record{rec}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"domain":"example.com"`) || !strings.Contains(got, `"_msg":"dns example.com"`) {
		t.Fatalf("%s", got)
	}
}

func TestTenantHeaders(t *testing.T) {
	var account, project, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account = r.Header.Get("AccountID")
		project = r.Header.Get("ProjectID")
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, "12:34", "test")
	rec := model.Record{Time: time.Unix(10, 0).UTC(), Host: "h", Kind: "flow", Iface: "eth0", Proto: "tcp"}
	rec.Finish()
	if err := c.Insert(context.Background(), []model.Record{rec}); err != nil {
		t.Fatal(err)
	}
	if account != "12" || project != "34" || !strings.Contains(path, "/insert/") {
		t.Fatalf("insert %s %s %s", account, project, path)
	}
	c.SetID("0:0")
	if _, err := c.Logs(context.Background(), LogFilter{Kind: "flow"}, time.Unix(1, 0), time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}
	if account != "0" || project != "0" || !strings.Contains(path, "/select/") {
		t.Fatalf("query %s %s %s", account, project, path)
	}
	if c.ID() != "0:0" {
		t.Fatalf("id %s", c.ID())
	}
}
