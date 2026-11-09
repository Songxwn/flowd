package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Songxwn/flowd/internal/config"
)

func TestLoginSession(t *testing.T) {
	s := New(Options{Config: config.Config{Token: "secret-token", Listen: "0.0.0.0:8787"}})
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"token":"nope"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("错误口令 %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"token":"secret-token"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("登录 %d %s", rec.Code, rec.Body.String())
	}
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "flowd_session" && c.Value != "" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("没有会话 cookie")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("会话 %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Bearer %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("退出 %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("退出后 %d", rec.Code)
	}
}

func TestEmptyTokenDenied(t *testing.T) {
	s := New(Options{Config: config.Config{Listen: "0.0.0.0:8787"}})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("空口令 %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("健康检查 %d", rec.Code)
	}
}
