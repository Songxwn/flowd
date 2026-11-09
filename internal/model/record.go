package model

import (
	"fmt"
	"strings"
	"time"
)

// Record 是写入 VictoriaLogs 的一条应用层流量日志。
// host、iface、kind、proto 会作为日志流字段，必须始终存在。
type Record struct {
	Time       time.Time `json:"_time"`
	Msg        string    `json:"_msg"`
	Host       string    `json:"host"`
	Kind       string    `json:"kind"`
	Iface      string    `json:"iface"`
	Proto      string    `json:"proto"`
	SrcIP      string    `json:"src_ip,omitempty"`
	SrcPort    uint16    `json:"src_port,omitempty"`
	DstIP      string    `json:"dst_ip,omitempty"`
	DstPort    uint16    `json:"dst_port,omitempty"`
	Domain     string    `json:"domain,omitempty"`
	DomainFrom string    `json:"domain_from,omitempty"`
	App        string    `json:"app,omitempty"`
	PID        int       `json:"pid,omitempty"`
	Bytes      uint64    `json:"bytes,omitempty"`
	Packets    uint64    `json:"packets,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Closed     bool      `json:"closed,omitempty"`
}

// Finish 补齐流字段并生成可读消息。
func (r *Record) Finish() {
	if strings.TrimSpace(r.Host) == "" {
		r.Host = "-"
	}
	if strings.TrimSpace(r.Iface) == "" {
		r.Iface = "-"
	}
	if r.Kind == "" {
		r.Kind = "flow"
	}
	if r.Proto == "" {
		r.Proto = "-"
	}
	r.Msg = r.message()
}

func (r Record) message() string {
	if r.Kind == "dns" {
		var b strings.Builder
		fmt.Fprintf(&b, "dns %s", r.Domain)
		if r.App != "" {
			fmt.Fprintf(&b, " app=%s", r.App)
		}
		if r.SrcIP != "" {
			fmt.Fprintf(&b, " src=%s", r.SrcIP)
		}
		if r.DstIP != "" {
			fmt.Fprintf(&b, " resolver=%s", r.DstIP)
		}
		return b.String()
	}
	var b strings.Builder
	if r.Proto == "icmp" {
		fmt.Fprintf(&b, "flow icmp %s -> %s", r.SrcIP, r.DstIP)
	} else {
		fmt.Fprintf(&b, "flow %s %s:%d -> %s:%d", r.Proto, r.SrcIP, r.SrcPort, r.DstIP, r.DstPort)
	}
	if r.Domain != "" {
		fmt.Fprintf(&b, " domain=%s", r.Domain)
	}
	if r.DomainFrom != "" {
		fmt.Fprintf(&b, " via=%s", r.DomainFrom)
	}
	if r.App != "" {
		fmt.Fprintf(&b, " app=%s", r.App)
	}
	if r.Bytes > 0 {
		fmt.Fprintf(&b, " bytes=%d", r.Bytes)
	}
	return b.String()
}
