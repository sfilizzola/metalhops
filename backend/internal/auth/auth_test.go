package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTVerification(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	var docs []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": docs})
	}))
	t.Cleanup(server.Close)
	docs = []map[string]string{rsaJWK("rsa", &rsaKey.PublicKey), ecJWK("ec", &ecKey.PublicKey)}
	verifier := New(server.URL, "authenticated", nil, server.Client(), func() time.Time { return now })
	issuer := server.URL + "/auth/v1"
	sub := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sessionID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	rs := sign(t, jwt.SigningMethodRS256, rsaKey, "rsa", tokenClaims(sub, sessionID, issuer, now))
	userID, gotSession, err := verifier.identity(context.Background(), rs)
	if err != nil || userID != sub || gotSession != sessionID {
		t.Fatalf("rs256 %s %s %v", userID, gotSession, err)
	}
	es := sign(t, jwt.SigningMethodES256, ecKey, "ec", tokenClaims(sub, sessionID, issuer, now))
	userID, gotSession, err = verifier.identity(context.Background(), es)
	if err != nil || userID != sub || gotSession != sessionID {
		t.Fatalf("es256 %s %s %v", userID, gotSession, err)
	}

	expired := tokenClaims(sub, sessionID, issuer, now)
	expired["exp"] = now.Add(-time.Second).Unix()
	future := tokenClaims(sub, sessionID, issuer, now)
	future["nbf"] = now.Add(time.Minute).Unix()
	wrongIssuer := tokenClaims(sub, sessionID, issuer+"/other", now)
	wrongAudience := tokenClaims(sub, sessionID, issuer, now)
	wrongAudience["aud"] = "anon"
	missingSub := tokenClaims("", sessionID, issuer, now)
	delete(missingSub, "sub")
	upperSub := tokenClaims("AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", sessionID, issuer, now)
	missingSession := tokenClaims(sub, "", issuer, now)
	delete(missingSession, "session_id")
	badSession := tokenClaims(sub, "not-a-uuid", issuer, now)

	cases := []jwt.MapClaims{expired, future, wrongIssuer, wrongAudience, missingSub, upperSub, missingSession, badSession}
	for i, claims := range cases {
		token := sign(t, jwt.SigningMethodRS256, rsaKey, "rsa", claims)
		if _, _, err := verifier.identity(context.Background(), token); err != ErrUnauthorized {
			t.Fatalf("case %d err %v", i, err)
		}
	}

	hs := sign(t, jwt.SigningMethodHS256, []byte("secret"), "rsa", tokenClaims(sub, sessionID, issuer, now))
	labelledES := withAlg(t, sign(t, jwt.SigningMethodRS256, rsaKey, "rsa", tokenClaims(sub, sessionID, issuer, now)), "ES256")
	labelledRS := withAlg(t, sign(t, jwt.SigningMethodES256, ecKey, "ec", tokenClaims(sub, sessionID, issuer, now)), "RS256")
	none := withAlg(t, sign(t, jwt.SigningMethodRS256, rsaKey, "rsa", tokenClaims(sub, sessionID, issuer, now)), "none")
	for _, token := range []string{hs, labelledES, labelledRS, none, "not-a-token"} {
		if _, _, err := verifier.identity(context.Background(), token); err != ErrUnauthorized {
			t.Fatalf("rejected token err %v", err)
		}
	}
}

func tokenClaims(sub, sessionID, issuer string, now time.Time) jwt.MapClaims {
	claims := jwt.MapClaims{
		"session_id": sessionID,
		"iss":        issuer,
		"aud":        "authenticated",
		"exp":        now.Add(time.Hour).Unix(),
		"nbf":        now.Unix(),
	}
	if sub != "" {
		claims["sub"] = sub
	}
	return claims
}

func sign(t *testing.T, method jwt.SigningMethod, key any, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func withAlg(t *testing.T, signed, alg string) string {
	t.Helper()
	parts := splitToken(t, signed)
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]any
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatal(err)
	}
	header["alg"] = alg
	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	parts[0] = base64.RawURLEncoding.EncodeToString(encoded)
	return parts[0] + "." + parts[1] + "." + parts[2]
}

func splitToken(t *testing.T, signed string) []string {
	t.Helper()
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(signed); i++ {
		if signed[i] == '.' {
			parts = append(parts, signed[start:i])
			start = i + 1
		}
	}
	parts = append(parts, signed[start:])
	if len(parts) != 3 {
		t.Fatalf("token parts %d", len(parts))
	}
	return parts
}

func rsaJWK(kid string, pub *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA", "alg": "RS256", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func ecJWK(kid string, pub *ecdsa.PublicKey) map[string]string {
	x := pub.X.FillBytes(make([]byte, 32))
	y := pub.Y.FillBytes(make([]byte, 32))
	return map[string]string{
		"kty": "EC", "alg": "ES256", "crv": "P-256", "kid": kid,
		"x": base64.RawURLEncoding.EncodeToString(x),
		"y": base64.RawURLEncoding.EncodeToString(y),
	}
}
