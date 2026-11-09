package capture

import (
	"context"
	"log"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Songxwn/flowd/internal/config"
	"github.com/Songxwn/flowd/internal/filter"
	"github.com/Songxwn/flowd/internal/model"
	"github.com/Songxwn/flowd/internal/parse"
	"github.com/Songxwn/flowd/internal/proc"
)

const maxFlows = 20000

// IfaceStat 是一块正在采集的网卡的计数。
type IfaceStat struct {
	Packets uint64
	Drops   uint64
	Error   string
	Running bool
}

// Stats 是采集器启动以来的计数。
type Stats struct {
	Tracked int
	Dropped uint64
	Ifaces  map[string]IfaceStat
}

type runner struct {
	cancel  context.CancelFunc
	packets atomic.Uint64
	drops   atomic.Uint64
	err     atomic.Value
}

type flowKey struct {
	iface string
	proto string
	a     netip.Addr
	ap    uint16
	b     netip.Addr
	bp    uint16
}

type flowState struct {
	iface      string
	proto      string
	src, dst   netip.Addr
	sp, dp     uint16
	oriented   bool
	domain     string
	domainFrom string
	app        string
	pid        int
	bytes      uint64
	packets    uint64
	first      time.Time
	last       time.Time
	fin        bool
}

type dnsItem struct {
	name  string
	until time.Time
}

// Service 把网卡上的包汇成连接和 DNS 日志。
type Service struct {
	out          chan<- model.Record
	host         string
	mu           sync.Mutex
	ctx          context.Context
	flows        map[flowKey]*flowState
	dns          map[netip.Addr]dnsItem
	seen         map[string]time.Time
	locals       map[netip.Addr]struct{}
	running      map[string]*runner
	procs        atomic.Pointer[proc.Table]
	filter       filter.Matcher
	idle         time.Duration
	exportEvery  time.Duration
	procOn       bool
	procRoot     string
	procEvery    time.Duration
	dropped      atomic.Uint64
	lastProcErr  string
	startedLoops bool
}

func New(out chan<- model.Record, host string) *Service {
	if host == "" {
		host = "-"
	}
	return &Service{
		out:         out,
		host:        host,
		flows:       map[flowKey]*flowState{},
		dns:         map[netip.Addr]dnsItem{},
		seen:        map[string]time.Time{},
		locals:      map[netip.Addr]struct{}{},
		running:     map[string]*runner{},
		idle:        30 * time.Second,
		exportEvery: 15 * time.Second,
		procEvery:   2 * time.Second,
		procRoot:    "/proc",
		procOn:      true,
	}
}

// Start 启动后台汇聚，并按配置开始采集。
func (s *Service) Start(ctx context.Context, cfg config.Config) {
	s.mu.Lock()
	s.ctx = ctx
	startLoops := !s.startedLoops
	s.startedLoops = true
	s.mu.Unlock()
	if startLoops {
		go s.procLoop(ctx)
		go s.janitor(ctx)
		go s.addrLoop(ctx)
	}
	s.Update(cfg)
}

// Close 停止采集并交出尚未写出的连接。
func (s *Service) Close() {
	s.stopAll()
	s.enqueue(s.FlushAll(time.Now()))
}

// Update 立即换上新的网卡和过滤规则。
func (s *Service) Update(cfg config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil {
		return
	}
	s.filter = filter.New(cfg.Filter.IncludeDomains, cfg.Filter.ExcludeDomains, cfg.Filter.IncludeApps, cfg.Filter.ExcludeApps, cfg.Filter.IncludeProtos, cfg.Filter.ExcludeProtos, cfg.Filter.IncludePorts, cfg.Filter.ExcludePorts)
	s.idle = cfg.Capture.IdleTimeout
	s.exportEvery = cfg.Capture.ExportInterval
	if s.idle < 5*time.Second {
		s.idle = 30 * time.Second
	}
	if s.exportEvery < 5*time.Second {
		s.exportEvery = 15 * time.Second
	}
	s.procOn = cfg.Proc.On()
	if cfg.Proc.Root != "" {
		s.procRoot = cfg.Proc.Root
	}
	if cfg.Proc.Interval > 0 {
		s.procEvery = cfg.Proc.Interval
	}
	want := map[string]struct{}{}
	for _, name := range cfg.Capture.Ifaces {
		want[name] = struct{}{}
	}
	for name, rn := range s.running {
		if _, ok := want[name]; ok {
			continue
		}
		rn.cancel()
		delete(s.running, name)
		log.Printf("停止采集网卡 %s", name)
	}
	for name := range want {
		if _, ok := s.running[name]; ok {
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		rn := &runner{cancel: cancel}
		rn.err.Store("")
		s.running[name] = rn
		go s.capture(ctx, name, rn)
	}
}

func (s *Service) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, rn := range s.running {
		rn.cancel()
		delete(s.running, name)
	}
}

// Stats 返回当前计数。调用方可以并发使用。
func (s *Service) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Stats{Tracked: len(s.flows), Dropped: s.dropped.Load(), Ifaces: map[string]IfaceStat{}}
	for name, rn := range s.running {
		errStr, _ := rn.err.Load().(string)
		out.Ifaces[name] = IfaceStat{
			Packets: rn.packets.Load(),
			Drops:   rn.drops.Load(),
			Error:   errStr,
			Running: errStr == "",
		}
	}
	return out
}

func (s *Service) capture(ctx context.Context, name string, rn *runner) {
	lastLog := ""
	for ctx.Err() == nil {
		src, err := Open(name)
		if err != nil {
			rn.err.Store(err.Error())
			if err.Error() != lastLog {
				log.Printf("网卡 %s 采集失败: %v", name, err)
				lastLog = err.Error()
			}
			if !sleep(ctx, 5*time.Second) {
				return
			}
			continue
		}
		rn.err.Store("")
		lastLog = ""
		log.Printf("开始采集网卡 %s", name)
		err = s.readLoop(ctx, name, rn, src)
		_ = src.Close()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			rn.err.Store(err.Error())
			if err.Error() != lastLog {
				log.Printf("网卡 %s 中断: %v", name, err)
				lastLog = err.Error()
			}
		}
		if !sleep(ctx, 2*time.Second) {
			return
		}
	}
}

func (s *Service) readLoop(ctx context.Context, name string, rn *runner, src PacketSource) error {
	for ctx.Err() == nil {
		frame, err := src.Read()
		if err != nil {
			if err == ErrTimeout {
				rn.drops.Store(src.Drops())
				continue
			}
			return err
		}
		rn.packets.Add(1)
		rn.drops.Store(src.Drops())
		s.enqueue(s.Observe(name, frame, time.Now()))
	}
	return ctx.Err()
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Observe 处理一个已经复制出来的以太网帧。
func (s *Service) Observe(iface string, frame []byte, now time.Time) []model.Record {
	fr, ok := parse.ParseFrame(frame)
	if !ok || !fr.Src.IsValid() || !fr.Dst.IsValid() {
		return nil
	}
	payload := fr.Payload
	if len(payload) > 4096 {
		payload = payload[:4096]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if isDNS(fr) {
		msg, ok := parse.ParseDNS(payload, fr.Proto)
		if !ok {
			return nil
		}
		s.rememberDNS(msg, now)
		if !s.filter.AllowL4(fr.Proto, fr.SrcPort, fr.DstPort) {
			return nil
		}
		if rec, ok := s.dnsRecord(iface, fr, msg, now); ok {
			return []model.Record{rec}
		}
		return nil
	}
	if !s.filter.AllowL4(fr.Proto, fr.SrcPort, fr.DstPort) {
		return nil
	}
	if fr.Dst.IsMulticast() || fr.Src.IsMulticast() || s.bothLocal(fr.Src, fr.Dst) {
		return nil
	}
	s.touchFlow(iface, fr, payload, now)
	return nil
}

func isDNS(fr parse.Frame) bool {
	return fr.SrcPort == 53 || fr.DstPort == 53 || fr.SrcPort == 5353 || fr.DstPort == 5353
}

func (s *Service) rememberDNS(msg parse.DNSMessage, now time.Time) {
	if len(s.dns) > 20000 {
		for ip, item := range s.dns {
			if item.until.Before(now) {
				delete(s.dns, ip)
			}
		}
		if len(s.dns) > 20000 {
			s.dns = map[netip.Addr]dnsItem{}
		}
	}
	for _, ans := range msg.Answers {
		if !ans.IP.IsValid() || ans.IP.IsMulticast() || ans.IP.IsUnspecified() {
			continue
		}
		ttl := time.Duration(ans.TTL) * time.Second
		if ttl < 10*time.Second {
			ttl = 30 * time.Second
		}
		if ttl > 30*time.Minute {
			ttl = 30 * time.Minute
		}
		s.dns[ans.IP.Unmap()] = dnsItem{name: msg.Name, until: now.Add(ttl)}
	}
}

func (s *Service) dnsRecord(iface string, fr parse.Frame, msg parse.DNSMessage, now time.Time) (model.Record, bool) {
	src, dst := fr.Src, fr.Dst
	sp, dp := fr.SrcPort, fr.DstPort
	if sp == 53 || sp == 5353 {
		src, dst = dst, src
		sp, dp = dp, sp
	}
	key := src.String() + "|" + msg.Name
	if at, ok := s.seen[key]; ok && now.Sub(at) < 2*time.Second {
		return model.Record{}, false
	}
	if len(s.seen) > 5000 {
		s.seen = map[string]time.Time{}
	}
	s.seen[key] = now
	app, pid := s.lookupApp(fr.Proto, src, sp, dst, dp)
	rec := model.Record{
		Time: now, Host: s.host, Kind: "dns", Iface: iface, Proto: fr.Proto,
		SrcIP: src.String(), SrcPort: sp, DstIP: dst.String(), DstPort: dp,
		Domain: msg.Name, DomainFrom: "dns", App: app, PID: pid,
	}
	if !s.filter.AllowL4(fr.Proto, sp, dp) || !s.filter.Allow(rec.Domain, rec.App) {
		return model.Record{}, false
	}
	rec.Finish()
	return rec, true
}

func (s *Service) touchFlow(iface string, fr parse.Frame, payload []byte, now time.Time) {
	key := makeKey(iface, fr.Proto, fr.Src, fr.Dst, fr.SrcPort, fr.DstPort)
	st := s.flows[key]
	if st == nil {
		if len(s.flows) >= maxFlows {
			s.dropped.Add(1)
			return
		}
		st = &flowState{iface: iface, proto: fr.Proto, first: now}
		s.flows[key] = st
	}
	st.bytes += uint64(fr.Bytes)
	st.packets++
	st.last = now
	if st.first.IsZero() {
		st.first = now
	}
	if fr.Flags&0x05 != 0 {
		st.fin = true
	}
	if fr.Proto == "tcp" && len(payload) > 0 {
		if name := parse.TLSServerName(payload); name != "" {
			st.setClient(fr.Src, fr.Dst, fr.SrcPort, fr.DstPort)
			st.setDomain(name, "sni")
		} else if name := parse.HTTPHost(payload); name != "" {
			st.setClient(fr.Src, fr.Dst, fr.SrcPort, fr.DstPort)
			st.setDomain(name, "http")
		}
	}
	if !st.oriented {
		if fr.SrcPort > fr.DstPort {
			st.setClient(fr.Src, fr.Dst, fr.SrcPort, fr.DstPort)
		} else if fr.DstPort > fr.SrcPort {
			st.setClient(fr.Dst, fr.Src, fr.DstPort, fr.SrcPort)
		} else if _, ok := s.locals[fr.Src]; ok {
			st.setClient(fr.Src, fr.Dst, fr.SrcPort, fr.DstPort)
		} else {
			st.setClient(fr.Dst, fr.Src, fr.DstPort, fr.SrcPort)
		}
	}
}

func (st *flowState) setClient(src, dst netip.Addr, sp, dp uint16) {
	st.src, st.dst = src, dst
	st.sp, st.dp = sp, dp
	st.oriented = true
}

func (st *flowState) setDomain(name, from string) {
	rank := map[string]int{"dns": 1, "http": 2, "sni": 3}
	if st.domain == "" || rank[from] >= rank[st.domainFrom] {
		st.domain = name
		st.domainFrom = from
	}
}

func makeKey(iface, proto string, src, dst netip.Addr, sp, dp uint16) flowKey {
	src, dst = src.Unmap(), dst.Unmap()
	k := flowKey{iface: iface, proto: proto}
	if src.Compare(dst) < 0 || (src == dst && sp <= dp) {
		k.a, k.ap, k.b, k.bp = src, sp, dst, dp
	} else {
		k.a, k.ap, k.b, k.bp = dst, dp, src, sp
	}
	return k
}

// ExportDue 写出空闲、结束或到达导出间隔的连接。
func (s *Service) ExportDue(now time.Time) []model.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Record
	for key, st := range s.flows {
		idle := !st.last.IsZero() && now.Sub(st.last) >= s.idle
		span := st.packets > 0 && !st.first.IsZero() && now.Sub(st.first) >= s.exportEvery
		if !idle && !st.fin && !span {
			continue
		}
		if rec, ok := s.snapshot(st, now, idle || st.fin); ok {
			out = append(out, rec)
		}
		if idle || st.fin {
			delete(s.flows, key)
			continue
		}
		st.bytes = 0
		st.packets = 0
		st.first = now
		st.fin = false
	}
	return out
}

// FlushAll 写出并清掉所有连接。
func (s *Service) FlushAll(now time.Time) []model.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Record, 0, len(s.flows))
	for key, st := range s.flows {
		if rec, ok := s.snapshot(st, now, true); ok {
			out = append(out, rec)
		}
		delete(s.flows, key)
	}
	return out
}

func (s *Service) snapshot(st *flowState, now time.Time, closed bool) (model.Record, bool) {
	if st.packets == 0 {
		return model.Record{}, false
	}
	s.attach(st, now)
	if !s.filter.AllowL4(st.proto, st.sp, st.dp) || !s.filter.Allow(st.domain, st.app) {
		return model.Record{}, false
	}
	dur := st.last.Sub(st.first).Milliseconds()
	if dur < 0 {
		dur = 0
	}
	rec := model.Record{
		Time: now, Host: s.host, Kind: "flow", Iface: st.iface, Proto: st.proto,
		SrcIP: st.src.String(), SrcPort: st.sp, DstIP: st.dst.String(), DstPort: st.dp,
		Domain: st.domain, DomainFrom: st.domainFrom, App: st.app, PID: st.pid,
		Bytes: st.bytes, Packets: st.packets, DurationMS: dur, Closed: closed,
	}
	rec.Finish()
	return rec, true
}

func (s *Service) attach(st *flowState, now time.Time) {
	if st.domain == "" {
		if n := s.dnsGet(st.dst, now); n != "" {
			st.domain, st.domainFrom = n, "dns"
		} else if n := s.dnsGet(st.src, now); n != "" {
			st.domain, st.domainFrom = n, "dns"
		}
	}
	app, pid := s.lookupApp(st.proto, st.src, st.sp, st.dst, st.dp)
	if app != "" {
		st.app = app
		st.pid = pid
	}
}

func (s *Service) dnsGet(ip netip.Addr, now time.Time) string {
	item, ok := s.dns[ip.Unmap()]
	if !ok || !item.until.After(now) {
		return ""
	}
	return item.name
}

func (s *Service) lookupApp(proto string, src netip.Addr, sp uint16, dst netip.Addr, dp uint16) (string, int) {
	tab := s.procs.Load()
	info, ok := tab.Lookup(proto, src, sp, dst, dp)
	if !ok {
		return "", 0
	}
	return info.App, info.PID
}

func (s *Service) bothLocal(a, b netip.Addr) bool {
	if len(s.locals) == 0 {
		return false
	}
	_, aok := s.locals[a.Unmap()]
	_, bok := s.locals[b.Unmap()]
	return aok && bok
}

func (s *Service) enqueue(recs []model.Record) {
	for _, rec := range recs {
		select {
		case s.out <- rec:
		default:
			s.dropped.Add(1)
		}
	}
}

func (s *Service) janitor(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.enqueue(s.ExportDue(now))
		}
	}
}

func (s *Service) procLoop(ctx context.Context) {
	s.scanProc()
	t := time.NewTimer(s.procInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.scanProc()
			t.Reset(s.procInterval())
		}
	}
}

func (s *Service) procInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.procEvery <= 0 {
		return 2 * time.Second
	}
	return s.procEvery
}

func (s *Service) scanProc() {
	s.mu.Lock()
	on := s.procOn
	root := s.procRoot
	s.mu.Unlock()
	if !on {
		s.procs.Store(nil)
		return
	}
	table, err := proc.Scan(root)
	if err != nil {
		if err.Error() != s.lastProcErr {
			log.Printf("读取进程连接失败: %v", err)
			s.lastProcErr = err.Error()
		}
		return
	}
	s.lastProcErr = ""
	s.procs.Store(table)
}

func (s *Service) addrLoop(ctx context.Context) {
	s.refreshLocals()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refreshLocals()
		}
	}
}

func (s *Service) refreshLocals() {
	next := map[netip.Addr]struct{}{}
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			ipn, ok := addr.(*net.IPNet)
			if !ok || ipn.IP == nil {
				continue
			}
			if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
				next[ip.Unmap()] = struct{}{}
			}
		}
	}
	s.mu.Lock()
	s.locals = next
	s.mu.Unlock()
}
