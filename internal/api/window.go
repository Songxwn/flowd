package api

import (
	"fmt"
	"time"
)

func TimeWindow(now time.Time, key, startRaw, endRaw string) (start, end time.Time, step string, err error) {
	if startRaw != "" || endRaw != "" {
		start, err = time.Parse(time.RFC3339, startRaw)
		if err != nil {
			return time.Time{}, time.Time{}, "", fmt.Errorf("开始时间无效")
		}
		end, err = time.Parse(time.RFC3339, endRaw)
		if err != nil {
			return time.Time{}, time.Time{}, "", fmt.Errorf("结束时间无效")
		}
		if !end.After(start) {
			return time.Time{}, time.Time{}, "", fmt.Errorf("结束时间必须晚于开始时间")
		}
		if end.Sub(start) > 31*24*time.Hour {
			return time.Time{}, time.Time{}, "", fmt.Errorf("时间范围不能超过 31 天")
		}
		return start, end, "1m", nil
	}
	switch key {
	case "", "1h":
		return now.Add(-time.Hour), now, "1m", nil
	case "15m":
		return now.Add(-15 * time.Minute), now, "30s", nil
	case "6h":
		return now.Add(-6 * time.Hour), now, "5m", nil
	case "24h":
		return now.Add(-24 * time.Hour), now, "15m", nil
	case "7d":
		return now.Add(-7 * 24 * time.Hour), now, "1h", nil
	default:
		return time.Time{}, time.Time{}, "", fmt.Errorf("不支持的时间范围")
	}
}
