// Package auth menyediakan hashing password, session token, dan seeder akun
// pertama. Session disimpan sebagai hash token di database; cookie hanya
// membawa token acak yang tidak dapat dipakai ulang setelah logout.
package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/store"
)

const (
	passwordIterations = 210_000
	passwordKeyLength  = 32
	passwordSaltLength = 16
	sessionTokenLength = 32
)

// HashPassword menghasilkan format PBKDF2-SHA256 yang menyertakan salt dan
// jumlah iterasi sehingga parameter dapat dinaikkan tanpa mematahkan akun lama.
func HashPassword(password string) (string, error) {
	if len(password) < 12 {
		return "", fmt.Errorf("%w: password minimal 12 karakter", domain.ErrInvalid)
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("buat salt password: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyLength)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return fmt.Sprintf(
		"pbkdf2-sha256$%d$%s$%s",
		passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword membandingkan password dengan hash secara constant-time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// NewSessionToken menghasilkan token cookie dan hash yang disimpan di DB.
func NewSessionToken() (token, tokenHash string, err error) {
	raw := make([]byte, sessionTokenLength)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("buat session token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken mengubah token cookie menjadi hash untuk lookup database.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// SeedSuperadmin membuat akun pertama hanya ketika tabel user masih kosong.
// Password tidak pernah ditimpa saat restart.
func SeedSuperadmin(
	ctx context.Context,
	st store.Store,
	username, displayName, password string,
) (bool, error) {
	count, err := st.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("hitung user: %w", err)
	}
	if count > 0 {
		return false, nil
	}
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return false, fmt.Errorf("%w: username superadmin wajib diisi", domain.ErrInvalid)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	_, err = st.CreateUser(ctx, domain.User{
		Username:     username,
		DisplayName:  strings.TrimSpace(displayName),
		Role:         domain.RoleSuperadmin,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		return false, fmt.Errorf("buat superadmin awal: %w", err)
	}
	return true, nil
}
