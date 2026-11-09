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
		w.WriteHeader(204)
	}))
	defer srv.Close()
	c := New(srv.URL, "test")
	rec := model.Record{Time: time.Unix(10, 0).UTC(), Host: "h", Kind: "dns", Iface: "eth0", Proto: "udp", Domain: "example.com"}
	rec.Finish()
	if err := c.Insert(context.Background(), []model.Record{rec}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"domain":"example.com"`) || !strings.Contains(got, `"_msg":"dns example.com"`) {
		t.Fatalf("%s", got)
	}
}
