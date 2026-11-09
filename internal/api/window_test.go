package api

import (
	"testing"
	"time"
)

func TestTimeWindow(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	start, end, step, err := TimeWindow(now, "15m", "", "")
	if err != nil || !start.Equal(now.Add(-15*time.Minute)) || !end.Equal(now) || step != "30s" {
		t.Fatalf("%s %s %s %v", start, end, step, err)
	}
	if _, _, _, err := TimeWindow(now, "2h", "", ""); err == nil {
		t.Fatal("未知范围应拒绝")
	}
	start, end, _, err = TimeWindow(now, "", "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z")
	if err != nil || end.Sub(start) != time.Hour {
		t.Fatal(err)
	}
}
