package vlogs

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/Songxwn/flowd/internal/model"
)

// Sink 把日志按批写入 VictoriaLogs。
type Sink struct {
	client    *Client
	in        <-chan model.Record
	done      chan struct{}
	Inserted  atomic.Uint64
	Errors    atomic.Uint64
	LastError atomic.Value
	LastOK    atomic.Int64
	lastLog   time.Time
	lastMsg   string
}

func NewSink(client *Client, in <-chan model.Record) *Sink {
	s := &Sink{client: client, in: in, done: make(chan struct{})}
	s.LastError.Store("")
	return s
}

// Run 一直读到通道关闭，并把剩余日志写完。
func (s *Sink) Run() {
	defer close(s.done)
	buf := make([]model.Record, 0, 128)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case rec, ok := <-s.in:
			if !ok {
				s.flush(buf)
				return
			}
			buf = append(buf, rec)
			if len(buf) >= 100 {
				buf = s.flush(buf)
			}
		case <-tick.C:
			buf = s.flush(buf)
		}
	}
}

func (s *Sink) Wait() { <-s.done }

func (s *Sink) flush(buf []model.Record) []model.Record {
	if len(buf) == 0 {
		return buf
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err := s.client.Insert(ctx, buf)
	cancel()
	if err != nil {
		s.Errors.Add(1)
		s.LastError.Store(err.Error())
		if s.lastMsg != err.Error() || time.Since(s.lastLog) > 30*time.Second {
			log.Printf("写入 VictoriaLogs 失败: %v", err)
			s.lastLog = time.Now()
			s.lastMsg = err.Error()
		}
		return buf[:0]
	}
	s.Inserted.Add(uint64(len(buf)))
	s.LastError.Store("")
	s.LastOK.Store(time.Now().UnixNano())
	return buf[:0]
}
