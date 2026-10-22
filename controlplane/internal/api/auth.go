package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/example/controlplane/internal/auth"
	"github.com/example/controlplane/internal/domain"
)

const sessionCookieName = "meltano_session"

type userContextKey struct{}

func userFromContext(ctx context.Context) (domain.User, bool) {
	user, ok := ctx.Value(userContextKey{}).(domain.User)
	return user, ok
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.disableAuth {
			next.ServeHTTP(w, r)
			return
		}
		if isPublicRoute(r) {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeError(w, r, fmt.Errorf("%w: cookie session tidak ditemukan", domain.ErrUnauthorized))
			return
		}
		user, err := s.store.GetSessionUser(r.Context(), auth.HashToken(cookie.Value), time.Now().UTC())
		if err != nil {
			s.clearSessionCookie(w, r)
			writeError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

func isPublicRoute(r *http.Request) bool {
	switch {
	case r.URL.Path == "/healthz", r.URL.Path == "/readyz", r.URL.Path == "/v1/version":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/login":
		return true
	default:
		return false
	}
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	s.setSessionCookie(w, r, "", -1)
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	user, err := s.store.GetUserByUsername(r.Context(), username)
	if err != nil || !auth.VerifyPassword(in.Password, user.PasswordHash) {
		writeError(w, r, fmt.Errorf("%w: username atau password salah", domain.ErrUnauthorized))
		return
	}
	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		writeError(w, r, err)
		return
	}
	expiresAt := time.Now().UTC().Add(s.sessionDuration)
	if err := s.store.CreateSession(r.Context(), tokenHash, user.ID, expiresAt); err != nil {
		writeError(w, r, err)
		return
	}
	s.setSessionCookie(w, r, token, int(s.sessionDuration.Seconds()))
	writeJSON(w, http.StatusOK, map[string]any{
		"user":       user,
		"expires_at": expiresAt,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		_ = s.store.DeleteSession(r.Context(), auth.HashToken(cookie.Value))
	}
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	user, _ := userFromContext(r.Context())
	if !auth.VerifyPassword(in.CurrentPassword, user.PasswordHash) {
		writeError(w, r, fmt.Errorf("%w: password saat ini salah", domain.ErrUnauthorized))
		return
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		writeError(w, r, err)
		return
	}
	updated, err := s.store.UpdateUser(r.Context(), user, &hash)
	if err != nil {
		writeError(w, r, err)
		return
	}
	_ = s.store.DeleteUserSessions(r.Context(), user.ID)
	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		writeError(w, r, err)
		return
	}
	expiresAt := time.Now().UTC().Add(s.sessionDuration)
	if err := s.store.CreateSession(r.Context(), tokenHash, user.ID, expiresAt); err != nil {
		writeError(w, r, err)
		return
	}
	s.setSessionCookie(w, r, token, int(s.sessionDuration.Seconds()))
	writeJSON(w, http.StatusOK, map[string]any{"user": updated})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !requireSuperadmin(w, r) {
		return
	}
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": users})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !requireSuperadmin(w, r) {
		return
	}
	var in struct {
		Username    string          `json:"username"`
		DisplayName string          `json:"display_name"`
		Password    string          `json:"password"`
		Role        domain.UserRole `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if !validUsername(username) {
		writeError(w, r, fmt.Errorf("%w: username hanya boleh huruf, angka, titik, garis bawah, dan tanda hubung", domain.ErrInvalid))
		return
	}
	if !in.Role.Valid() {
		writeError(w, r, fmt.Errorf("%w: role harus superadmin atau admin", domain.ErrInvalid))
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	now := time.Now().UTC()
	user, err := s.store.CreateUser(r.Context(), domain.User{
		Username: username, DisplayName: strings.TrimSpace(in.DisplayName),
		Role: in.Role, PasswordHash: hash, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !requireSuperadmin(w, r) {
		return
	}
	user, err := s.store.GetUserByID(r.Context(), r.PathValue("userID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		Username    *string          `json:"username"`
		DisplayName *string          `json:"display_name"`
		Password    *string          `json:"password"`
		Role        *domain.UserRole `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Username != nil {
		username := strings.ToLower(strings.TrimSpace(*in.Username))
		if !validUsername(username) {
			writeError(w, r, fmt.Errorf("%w: username tidak valid", domain.ErrInvalid))
			return
		}
		user.Username = username
	}
	if in.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	if in.Role != nil {
		if !in.Role.Valid() {
			writeError(w, r, fmt.Errorf("%w: role tidak valid", domain.ErrInvalid))
			return
		}
		current, _ := userFromContext(r.Context())
		if current.ID == user.ID && *in.Role != domain.RoleSuperadmin {
			writeError(w, r, fmt.Errorf("%w: superadmin tidak dapat menurunkan role dirinya sendiri", domain.ErrInvalid))
			return
		}
		user.Role = *in.Role
	}
	var passwordHash *string
	if in.Password != nil {
		hash, err := auth.HashPassword(*in.Password)
		if err != nil {
			writeError(w, r, err)
			return
		}
		passwordHash = &hash
	}
	updated, err := s.store.UpdateUser(r.Context(), user, passwordHash)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if passwordHash != nil {
		_ = s.store.DeleteUserSessions(r.Context(), user.ID)
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !requireSuperadmin(w, r) {
		return
	}
	id := r.PathValue("userID")
	current, _ := userFromContext(r.Context())
	if current.ID == id {
		writeError(w, r, fmt.Errorf("%w: tidak dapat menghapus akun sendiri", domain.ErrInvalid))
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requireSuperadmin(w http.ResponseWriter, r *http.Request) bool {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, r, domain.ErrUnauthorized)
		return false
	}
	if user.Role != domain.RoleSuperadmin {
		writeError(w, r, fmt.Errorf("%w: hanya superadmin yang dapat mengelola user", domain.ErrForbidden))
		return false
	}
	return true
}

func validUsername(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) &&
			char != '.' && char != '_' && char != '-' {
			return false
		}
	}
	return true
}
