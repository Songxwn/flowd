package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Songxwn/flowd/internal/capture"
	"github.com/Songxwn/flowd/internal/config"
	"github.com/Songxwn/flowd/internal/vlogs"
	"github.com/Songxwn/flowd/internal/web"
)

// Server 提供本机网页和查询接口。
type Server struct {
	mu       sync.RWMutex
	cfg      config.Config
	path     string
	bound    string
	version  string
	svc      *capture.Service
	logs     *vlogs.Client
	sink     *vlogs.Sink
	vlErr    atomic.Value
	sessions map[string]time.Time
}

type Options struct {
	Path    string
	Bound   string
	Version string
	Config  config.Config
	Service *capture.Service
	Logs    *vlogs.Client
	Sink    *vlogs.Sink
}

func New(opt Options) *Server {
	s := &Server{
		cfg:      opt.Config,
		path:     opt.Path,
		bound:    opt.Bound,
		version:  opt.Version,
		svc:      opt.Service,
		logs:     opt.Logs,
		sink:     opt.Sink,
		sessions: map[string]time.Time{},
	}
	s.vlErr.Store("正在检查 VictoriaLogs")
	return s
}

func (s *Server) Watch(stop <-chan struct{}) {
	s.checkVL()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			s.checkVL()
		}
	}
}

func (s *Server) checkVL() {
	ctx, cancel := contextTimeout(5 * time.Second)
	defer cancel()
	if err := s.logs.Healthy(ctx); err != nil {
		s.vlErr.Store(err.Error())
		return
	}
	s.vlErr.Store("")
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/status", s.authed(s.status))
	mux.HandleFunc("GET /api/ifaces", s.authed(s.ifaces))
	mux.HandleFunc("GET /api/config", s.authed(s.getConfig))
	mux.HandleFunc("PUT /api/config", s.authed(s.putConfig))
	mux.HandleFunc("GET /api/flows", s.authed(s.flows))
	mux.HandleFunc("GET /api/rank", s.authed(s.rank))
	mux.HandleFunc("GET /api/series", s.authed(s.series))
	mux.Handle("/", web.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.version})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求无效"})
		return
	}
	s.mu.RLock()
	want := s.cfg.Token
	s.mu.RUnlock()
	if want == "" || !tokenMatch(in.Token, want) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "口令不正确"})
		return
	}
	id, err := newSessionID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "无法创建登录状态"})
		return
	}
	s.mu.Lock()
	s.pruneSessionsLocked(time.Now())
	s.sessions[id] = time.Now().Add(14 * 24 * time.Hour)
	s.mu.Unlock()
	http.SetCookie(w, sessionCookie(id, 86400*14))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("flowd_session"); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, sessionCookie("", -1))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshot()
	st := s.svc.Stats()
	vlErr, _ := s.vlErr.Load().(string)
	var last string
	if nano := s.sink.LastOK.Load(); nano > 0 {
		last = time.Unix(0, nano).UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":            s.version,
		"host":               cfg.Hostname,
		"listen":             s.bound,
		"vl_url":             s.logs.URL(),
		"vl_id":              s.logs.ID(),
		"vl_ok":              vlErr == "",
		"vl_error":           vlErr,
		"inserted":           s.sink.Inserted.Load(),
		"insert_errors":      s.sink.Errors.Load(),
		"dropped":            st.Dropped,
		"tracked":            st.Tracked,
		"capture_supported":  capture.Supported(),
		"last_insert":        last,
		"ifaces_selected":    len(cfg.Capture.Ifaces),
		"packets":            sumPackets(st),
		"kernel_drops":       sumDrops(st),
	})
}

func (s *Server) ifaces(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshot()
	selected := map[string]struct{}{}
	for _, name := range cfg.Capture.Ifaces {
		selected[name] = struct{}{}
	}
	stats := s.svc.Stats().Ifaces
	nics, err := net.Interfaces()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	seen := map[string]struct{}{}
	var out []map[string]any
	for _, nic := range nics {
		seen[nic.Name] = struct{}{}
		out = append(out, ifaceView(nic, selected, stats))
	}
	for name := range selected {
		if _, ok := seen[name]; ok {
			continue
		}
		st := stats[name]
		out = append(out, map[string]any{
			"name": name, "selected": true, "up": false,
			"packets": st.Packets, "drops": st.Drops, "error": firstNonEmpty(st.Error, "网卡不存在"),
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, out)
}

func ifaceView(nic net.Interface, selected map[string]struct{}, stats map[string]capture.IfaceStat) map[string]any {
	_, on := selected[nic.Name]
	addrs, _ := nic.Addrs()
	var list []string
	for _, addr := range addrs {
		list = append(list, addr.String())
	}
	if list == nil {
		list = []string{}
	}
	st := stats[nic.Name]
	return map[string]any{
		"name":      nic.Name,
		"mtu":       nic.MTU,
		"mac":       nic.HardwareAddr.String(),
		"up":        nic.Flags&net.FlagUp != 0,
		"loopback":  nic.Flags&net.FlagLoopback != 0,
		"addrs":     list,
		"selected":  on,
		"packets":   st.Packets,
		"drops":     st.Drops,
		"error":     st.Error,
	}
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"listen":           cfg.Listen,
		"vl_url":           cfg.VictoriaLogs.URL,
		"vl_id":            string(cfg.VictoriaLogs.ID),
		"ifaces":           cfg.Capture.Ifaces,
		"include_domains":  cfg.Filter.IncludeDomains,
		"exclude_domains":  cfg.Filter.ExcludeDomains,
		"include_apps":     cfg.Filter.IncludeApps,
		"exclude_apps":     cfg.Filter.ExcludeApps,
		"include_protos":   cfg.Filter.IncludeProtos,
		"exclude_protos":   cfg.Filter.ExcludeProtos,
		"include_ports":    cfg.Filter.IncludePorts,
		"exclude_ports":    cfg.Filter.ExcludePorts,
		"proc_enabled":     cfg.Proc.On(),
		"exclude_own_web":  cfg.Capture.HideOwnWeb(),
		"token_set":        cfg.Token != "",
		"idle_timeout":     cfg.Capture.IdleTimeout.String(),
		"export_interval":  cfg.Capture.ExportInterval.String(),
		"config_path":      s.path,
	})
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Listen          string   `json:"listen"`
		VLURL           string   `json:"vl_url"`
		VLID            *string  `json:"vl_id"`
		Ifaces          []string `json:"ifaces"`
		IncludeDomains  []string `json:"include_domains"`
		ExcludeDomains  []string `json:"exclude_domains"`
		IncludeApps     []string `json:"include_apps"`
		ExcludeApps     []string `json:"exclude_apps"`
		IncludeProtos   []string `json:"include_protos"`
		ExcludeProtos   []string `json:"exclude_protos"`
		IncludePorts    []string `json:"include_ports"`
		ExcludePorts    []string `json:"exclude_ports"`
		ProcEnabled     *bool    `json:"proc_enabled"`
		ExcludeOwnWeb   *bool    `json:"exclude_own_web"`
		Token           string   `json:"token"`
		ClearToken      bool     `json:"clear_token"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求无效"})
		return
	}
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	if strings.TrimSpace(in.Listen) != "" {
		cfg.Listen = strings.TrimSpace(in.Listen)
	}
	if strings.TrimSpace(in.VLURL) != "" {
		cfg.VictoriaLogs.URL = strings.TrimSpace(in.VLURL)
	}
	if in.VLID != nil {
		cfg.VictoriaLogs.ID = config.Tenant(strings.TrimSpace(*in.VLID))
	}
	cfg.Capture.Ifaces = in.Ifaces
	cfg.Filter.IncludeDomains = in.IncludeDomains
	cfg.Filter.ExcludeDomains = in.ExcludeDomains
	cfg.Filter.IncludeApps = in.IncludeApps
	cfg.Filter.ExcludeApps = in.ExcludeApps
	cfg.Filter.IncludeProtos = in.IncludeProtos
	cfg.Filter.ExcludeProtos = in.ExcludeProtos
	cfg.Filter.IncludePorts = config.CleanPorts(in.IncludePorts)
	cfg.Filter.ExcludePorts = config.CleanPorts(in.ExcludePorts)
	if in.ProcEnabled != nil {
		cfg.Proc.Enabled = in.ProcEnabled
	}
	if in.ExcludeOwnWeb != nil {
		cfg.Capture.ExcludeOwnWeb = in.ExcludeOwnWeb
	}
	if in.ClearToken {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "口令不能清除"})
		return
	}
	prev := cfg.Token
	if strings.TrimSpace(in.Token) != "" {
		cfg.Token = strings.TrimSpace(in.Token)
	}
	if cfg.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "口令不能为空"})
		return
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := config.Save(s.path, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存配置失败: " + err.Error()})
		return
	}
	s.logs.SetURL(cfg.VictoriaLogs.URL)
	s.logs.SetID(string(cfg.VictoriaLogs.ID))
	s.svc.Update(cfg)
	s.mu.Lock()
	s.cfg = cfg
	if cfg.Token != prev {
		s.sessions = map[string]time.Time{}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"restart_required": cfg.Listen != s.bound,
	})
}

func (s *Server) flows(w http.ResponseWriter, r *http.Request) {
	start, end, _, err := s.window(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	f := vlogs.LogFilter{
		Kind:   r.URL.Query().Get("kind"),
		Domain: r.URL.Query().Get("domain"),
		App:    r.URL.Query().Get("app"),
		Iface:  r.URL.Query().Get("iface"),
		Proto:  r.URL.Query().Get("proto"),
		Port:   r.URL.Query().Get("port"),
		SrcIP:  r.URL.Query().Get("src_ip"),
		DstIP:  r.URL.Query().Get("dst_ip"),
		Text:   r.URL.Query().Get("q"),
		Limit:  limit,
	}
	ctx, cancel := contextTimeout(45 * time.Second)
	defer cancel()
	rows, err := s.logs.Logs(ctx, f, start, end)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": rows})
}

func (s *Server) rank(w http.ResponseWriter, r *http.Request) {
	start, end, _, err := s.window(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ctx, cancel := contextTimeout(45 * time.Second)
	defer cancel()
	kind := r.URL.Query().Get("kind")
	proto := r.URL.Query().Get("proto")
	port := r.URL.Query().Get("port")
	rows, err := s.logs.Rank(ctx, kind, r.URL.Query().Get("by"), proto, port, r.URL.Query().Get("sort"), start, end, limit)
	if err != nil {
		code := http.StatusBadGateway
		if strings.Contains(err.Error(), "不支持") {
			code = http.StatusBadRequest
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	hits, bytes, err := s.logs.Totals(ctx, kind, proto, port, start, end)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []vlogs.RankRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows, "hits": hits, "bytes": bytes})
}

func (s *Server) series(w http.ResponseWriter, r *http.Request) {
	start, end, step, err := s.window(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := contextTimeout(45 * time.Second)
	defer cancel()
	by := r.URL.Query().Get("by")
	kind := r.URL.Query().Get("kind")
	list, err := s.logs.Series(ctx, kind, r.URL.Query().Get("proto"), r.URL.Query().Get("port"), by, start, end, step)
	if err != nil {
		code := http.StatusBadGateway
		if strings.Contains(err.Error(), "不支持") {
			code = http.StatusBadRequest
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	if list == nil {
		list = []vlogs.SeriesSet{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": flattenSeries(list), "series": list})
}

func flattenSeries(list []vlogs.SeriesSet) []vlogs.Point {
	byT := map[int64]*vlogs.Point{}
	var order []int64
	for _, series := range list {
		for _, point := range series.Points {
			got := byT[point.T]
			if got == nil {
				copy := point
				byT[point.T] = &copy
				order = append(order, point.T)
				continue
			}
			got.Hits += point.Hits
			got.Bytes += point.Bytes
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]vlogs.Point, 0, len(order))
	for _, ts := range order {
		out = append(out, *byT[ts])
	}
	return out
}

func (s *Server) window(r *http.Request) (time.Time, time.Time, string, error) {
	q := r.URL.Query()
	return TimeWindow(time.Now(), q.Get("range"), q.Get("start"), q.Get("end"))
}

func (s *Server) snapshot() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		want := s.cfg.Token
		ok := tokenMatch(tokenFrom(r), want) || s.sessionOKLocked(r, time.Now())
		s.mu.RUnlock()
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "需要口令"})
			return
		}
		next(w, r)
	}
}

func (s *Server) sessionOKLocked(r *http.Request, now time.Time) bool {
	c, err := r.Cookie("flowd_session")
	if err != nil || c.Value == "" {
		return false
	}
	exp, ok := s.sessions[c.Value]
	return ok && now.Before(exp)
}

func (s *Server) pruneSessionsLocked(now time.Time) {
	for id, exp := range s.sessions {
		if !now.Before(exp) {
			delete(s.sessions, id)
		}
	}
}

func tokenFrom(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("X-Flowd-Token"))
}

func tokenMatch(got, want string) bool {
	if want == "" || got == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func newSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "flowd_session",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

func decodeJSON(r *http.Request, dest any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dest)
}

func contextTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func sumPackets(st capture.Stats) uint64 {
	var n uint64
	for _, iface := range st.Ifaces {
		n += iface.Packets
	}
	return n
}

func sumDrops(st capture.Stats) uint64 {
	var n uint64
	for _, iface := range st.Ifaces {
		n += iface.Drops
	}
	return n
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
