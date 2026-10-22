package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/controlplane/internal/auth"
	"github.com/example/controlplane/internal/store/memory"
)

func newAuthTestServer(t *testing.T) (*Server, *memory.Store) {
	t.Helper()
	st := memory.New()
	if _, err := auth.SeedSuperadmin(
		context.Background(), st, "superadmin", "Super Admin", "SuperAdmin!2026-Meltano",
	); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, log, "test"), st
}

func requestJSONWithCookie(
	t *testing.T,
	srv *Server,
	method, target string,
	body any,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func loginTestUser(t *testing.T, srv *Server, username, password string) *http.Cookie {
	t.Helper()
	rec := requestJSONWithCookie(t, srv, http.MethodPost, "/v1/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s = %d (%s)", username, rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login tidak mengirim cookie session")
	}
	return cookies[0]
}

func TestAuthLoginAndRoleAccess(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	superCookie := loginTestUser(t, srv, "superadmin", "SuperAdmin!2026-Meltano")

	rec := requestJSONWithCookie(t, srv, http.MethodGet, "/v1/auth/me", nil, superCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d (%s)", rec.Code, rec.Body.String())
	}

	rec = requestJSONWithCookie(t, srv, http.MethodPost, "/v1/users", map[string]any{
		"username": "operator", "display_name": "Operator",
		"password": "Operator!2026-Meltano", "role": "admin",
	}, superCookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create admin = %d (%s)", rec.Code, rec.Body.String())
	}

	adminCookie := loginTestUser(t, srv, "operator", "Operator!2026-Meltano")
	rec = requestJSONWithCookie(t, srv, http.MethodGet, "/v1/tenants", nil, adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin tenants = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = requestJSONWithCookie(t, srv, http.MethodGet, "/v1/users", nil, adminCookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin list users = %d, ingin 403", rec.Code)
	}
}

func TestAuthRejectsAnonymousAndLogout(t *testing.T) {
	srv, _ := newAuthTestServer(t)
	rec := requestJSONWithCookie(t, srv, http.MethodGet, "/v1/tenants", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous tenants = %d, ingin 401", rec.Code)
	}

	cookie := loginTestUser(t, srv, "superadmin", "SuperAdmin!2026-Meltano")
	rec = requestJSONWithCookie(t, srv, http.MethodPost, "/v1/auth/logout", nil, cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = requestJSONWithCookie(t, srv, http.MethodGet, "/v1/auth/me", nil, cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me setelah logout = %d, ingin 401", rec.Code)
	}
}
