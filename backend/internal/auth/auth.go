package auth

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUnauthorized = errors.New("auth: unauthorized")
var ErrProvider = errors.New("auth: provider unavailable")

var canonicalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Caller struct {
	UserID           string
	SessionID        string
	SessionCreatedAt time.Time
}

type accessClaims struct {
	jwt.RegisteredClaims
	SessionID string `json:"session_id"`
}

type Verifier struct {
	issuer   string
	audience string
	jwksURL  string
	pool     *pgxpool.Pool
	client   *http.Client
	clock    func() time.Time
	cache    keyCache
}

func New(projectURL, audience string, pool *pgxpool.Pool, client *http.Client, clock func() time.Time) *Verifier {
	base := strings.TrimRight(projectURL, "/")
	verifier := &Verifier{
		issuer:   base + "/auth/v1",
		audience: audience,
		jwksURL:  base + "/auth/v1/.well-known/jwks.json",
		pool:     pool,
		client:   client,
		clock:    clock,
	}
	verifier.cache.cond = syncNewCond(&verifier.cache.mu)
	verifier.cache.keys = map[string]cachedKey{}
	return verifier
}

func (v *Verifier) identity(ctx context.Context, encoded string) (string, string, error) {
	claims := &accessClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.clock),
	)
	_, err := parser.ParseWithClaims(encoded, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		alg, _ := token.Header["alg"].(string)
		return v.publicKey(ctx, kid, alg)
	})
	if err != nil {
		if errors.Is(err, ErrProvider) {
			return "", "", ErrProvider
		}
		return "", "", ErrUnauthorized
	}
	if !canonicalUUID.MatchString(claims.Subject) || !canonicalUUID.MatchString(claims.SessionID) {
		return "", "", ErrUnauthorized
	}
	return claims.Subject, claims.SessionID, nil
}

func (v *Verifier) Authenticate(ctx context.Context, encoded string) (Caller, error) {
	userID, sessionID, err := v.identity(ctx, encoded)
	if err != nil {
		return Caller{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var created time.Time
	err = v.pool.QueryRow(queryCtx, `
		select created_at
		from auth.sessions
		where id = $1::uuid and user_id = $2::uuid`, sessionID, userID).Scan(&created)
	if errors.Is(err, pgx.ErrNoRows) {
		return Caller{}, ErrUnauthorized
	}
	if err != nil {
		return Caller{}, fmt.Errorf("auth: session lookup: %w", err)
	}
	return Caller{UserID: userID, SessionID: sessionID, SessionCreatedAt: created}, nil
}

func (v *Verifier) publicKey(ctx context.Context, kid, alg string) (crypto.PublicKey, error) {
	if kid == "" || (alg != "RS256" && alg != "ES256") {
		return nil, ErrUnauthorized
	}
	waited := false
	for {
		v.cache.mu.Lock()
		now := v.clock()
		fresh := v.cache.have && now.Before(v.cache.expiresAt)
		key, ok := v.cache.keys[kid]
		if fresh && ok {
			v.cache.mu.Unlock()
			if key.alg != alg {
				return nil, ErrUnauthorized
			}
			return key.pub, nil
		}
		if v.cache.refreshing {
			v.cache.cond.Wait()
			v.cache.mu.Unlock()
			waited = true
			continue
		}
		if waited {
			v.cache.mu.Unlock()
			if fresh {
				return nil, ErrUnauthorized
			}
			return nil, ErrProvider
		}
		v.cache.refreshing = true
		v.cache.mu.Unlock()

		keys, err := v.fetchJWKS(ctx)
		v.cache.mu.Lock()
		v.cache.refreshing = false
		if err == nil {
			v.cache.keys = keys
			v.cache.expiresAt = v.clock().Add(10 * time.Minute)
			v.cache.have = true
		}
		expired := !v.cache.have || !v.clock().Before(v.cache.expiresAt)
		v.cache.cond.Broadcast()
		v.cache.mu.Unlock()
		if err != nil {
			if expired {
				return nil, ErrProvider
			}
			return nil, ErrUnauthorized
		}
		waited = true
	}
}
