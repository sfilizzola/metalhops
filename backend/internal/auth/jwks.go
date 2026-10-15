package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

type cachedKey struct {
	alg string
	pub crypto.PublicKey
}

type keyCache struct {
	mu         sync.Mutex
	cond       *sync.Cond
	refreshing bool
	keys       map[string]cachedKey
	expiresAt  time.Time
	have       bool
}

func syncNewCond(mu *sync.Mutex) *sync.Cond {
	return sync.NewCond(mu)
}

type jwk struct {
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Crv string `json:"crv"`
	N   string `json:"n"`
	E   string `json:"e"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (v *Verifier) fetchJWKS(ctx context.Context) (map[string]cachedKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, ErrProvider
	}
	res, err := v.client.Do(req)
	if err != nil {
		return nil, ErrProvider
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, ErrProvider
	}
	return parseJWKS(body)
}

func parseJWKS(body []byte) (map[string]cachedKey, error) {
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || len(doc.Keys) == 0 {
		return nil, ErrProvider
	}
	keys := make(map[string]cachedKey, len(doc.Keys))
	for _, key := range doc.Keys {
		if key.Kid == "" {
			return nil, ErrProvider
		}
		if _, exists := keys[key.Kid]; exists {
			return nil, ErrProvider
		}
		parsed, err := parseJWK(key)
		if err != nil {
			return nil, ErrProvider
		}
		keys[key.Kid] = parsed
	}
	return keys, nil
}

func parseJWK(key jwk) (cachedKey, error) {
	switch {
	case key.Kty == "RSA" && key.Alg == "RS256":
		pub, err := rsaPublic(key.N, key.E)
		if err != nil {
			return cachedKey{}, err
		}
		return cachedKey{alg: "RS256", pub: pub}, nil
	case key.Kty == "EC" && key.Alg == "ES256" && key.Crv == "P-256":
		pub, err := p256Public(key.X, key.Y)
		if err != nil {
			return cachedKey{}, err
		}
		return cachedKey{alg: "ES256", pub: pub}, nil
	default:
		return cachedKey{}, ErrProvider
	}
}

func rsaPublic(nText, eText string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nText)
	if err != nil || len(nBytes) == 0 {
		return nil, ErrProvider
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eText)
	if err != nil || len(eBytes) == 0 || len(eBytes) > 8 {
		return nil, ErrProvider
	}
	e := new(big.Int).SetBytes(eBytes).Int64()
	if e < 2 {
		return nil, ErrProvider
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(e)}, nil
}

func p256Public(xText, yText string) (*ecdsa.PublicKey, error) {
	xBytes, err := base64.RawURLEncoding.DecodeString(xText)
	if err != nil || len(xBytes) != 32 {
		return nil, ErrProvider
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(yText)
	if err != nil || len(yBytes) != 32 {
		return nil, ErrProvider
	}
	pub := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}
	if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
		return nil, ErrProvider
	}
	return pub, nil
}
