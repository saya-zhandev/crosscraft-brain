// Package auth is the lightweight API-key bearer-token authentication layer for
// mobile / third-party clients. Keys are stored SHA-256-hashed; the raw key is
// only shown once at creation time (cc_ prefix + 21 nanoid chars).
//
// By default auth is optional — requests without keys still work. Set the env
// var AUTH_REQUIRED=true to reject unauthenticated requests.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/id"
)

// Service manages API keys and provides auth middleware.
type Service struct {
	pool     *pgxpool.Pool
	required bool
}

// New creates an auth service. Set required=true to reject unauthenticated requests.
func New(pool *pgxpool.Pool) *Service {
	required := strings.ToLower(os.Getenv("AUTH_REQUIRED")) == "true"
	if required {
		log.Println("auth: AUTH_REQUIRED=true — unauthenticated requests will be rejected")
	}
	return &Service{pool: pool, required: required}
}

// APIKey is the safe (no secret) view of an API key.
type APIKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

// GenerateKey creates a new API key. Returns the safe row AND the raw key
// (only time the raw key is available). Format: cc_<nanoid>.
func (s *Service) GenerateKey(ctx context.Context, name string) (APIKey, string, error) {
	raw := "cc_" + id.New()
	hash := hashKey(raw)
	kid := id.New()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name, created_at) VALUES ($1,$2,$3, now())`,
		kid, hash, name)
	if err != nil {
		return APIKey{}, "", err
	}
	return APIKey{ID: kid, Name: name, CreatedAt: time.Now().UTC().Format(time.RFC3339)}, raw, nil
}

// ListKeys returns all API keys (never the raw secret).
func (s *Service) ListKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, created_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		var ts time.Time
		if err := rows.Scan(&k.ID, &k.Name, &ts); err != nil {
			return nil, err
		}
		k.CreatedAt = ts.UTC().Format(time.RFC3339)
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeKey deletes an API key.
func (s *Service) RevokeKey(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE id=$1`, id)
	return err
}

// Validate checks a raw key string and returns its metadata if valid.
func (s *Service) Validate(ctx context.Context, raw string) (*APIKey, error) {
	if raw == "" || !strings.HasPrefix(raw, "cc_") {
		return nil, nil
	}
	h := hashKey(raw)
	var k APIKey
	var ts time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, created_at FROM api_keys WHERE key_hash=$1`, h,
	).Scan(&k.ID, &k.Name, &ts)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	k.CreatedAt = ts.UTC().Format(time.RFC3339)
	return &k, nil
}

// Middleware is HTTP middleware that populates request context with auth info.
// When AUTH_REQUIRED is false (default), unauthenticated requests pass through.
// When true, requests without a valid key get 401.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := extractBearer(r)
		key, err := s.Validate(r.Context(), raw)
		if err != nil {
			http.Error(w, `{"error":"auth internal error"}`, http.StatusInternalServerError)
			return
		}
		if key != nil {
			r.Header.Set("X-Authenticated", "true")
			r.Header.Set("X-Key-ID", key.ID)
			r.Header.Set("X-Key-Name", key.Name)
		} else if s.required {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── helpers ─────────────────────────────────────────────────────────────────

func hashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func extractBearer(r *http.Request) string {
	// Prefer Authorization: Bearer <token>
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	// Fallback: X-API-Key header (simpler for mobile HTTP clients)
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}
	// Fallback: ?api_key= query param (webhook URLs)
	if key := r.URL.Query().Get("api_key"); key != "" {
		return key
	}
	return ""
}

// constantTimeEq prevents timing attacks on key comparison.
func constantTimeEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
