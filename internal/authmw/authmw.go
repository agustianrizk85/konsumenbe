// Package authmw verifies Ed25519-signed SSO access tokens issued by the
// Greenpark master auth service, using its JWKS public key — no per-request call
// back to auth. Copied from the other division backends (stdlib-only) so this
// Konsumen backend can accept the unified dashboard login token directly for its
// division-facing (Sales / Teknik / Keuangan / Legal / Perencanaan) endpoints.
package authmw

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

var b64 = base64.RawURLEncoding

// Claims mirrors the access-token payload issued by the auth service.
type Claims struct {
	Issuer    string            `json:"iss"`
	Subject   string            `json:"sub"`
	Username  string            `json:"username"`
	Email     string            `json:"email,omitempty"`
	Name      string            `json:"name"`
	Super     bool              `json:"super,omitempty"`
	Roles     map[string]string `json:"roles,omitempty"`
	IssuedAt  int64             `json:"iat"`
	ExpiresAt int64             `json:"exp"`
	JTI       string            `json:"jti"`
}

// CanAccess reports whether the caller may use the given department at all.
func (c Claims) CanAccess(dept string) bool {
	if c.Super {
		return true
	}
	_, ok := c.Roles[dept]
	return ok
}

// Role returns the caller's role string in the given department (empty if none).
func (c Claims) Role(dept string) string { return c.Roles[dept] }

// Options configures a Verifier.
type Options struct {
	JWKSURL    string
	Issuer     string
	HTTPClient *http.Client
}

// Verifier authenticates requests using auth-service access tokens.
type Verifier struct {
	opts   Options
	client *http.Client

	mu   sync.RWMutex
	keys map[string]ed25519.PublicKey // kid -> key
}

// New builds a Verifier. Returns nil when no JWKSURL is configured (SSO
// acceptance simply stays off).
func New(opts Options) *Verifier {
	if strings.TrimSpace(opts.JWKSURL) == "" {
		return nil
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Verifier{opts: opts, client: client, keys: make(map[string]ed25519.PublicKey)}
}

// Verify validates a compact EdDSA JWT and returns its claims.
func (v *Verifier) Verify(tok string) (Claims, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("token tidak valid")
	}
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	hb, err := b64.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &h) != nil {
		return Claims{}, errors.New("token tidak valid")
	}
	if h.Alg != "EdDSA" {
		return Claims{}, fmt.Errorf("alg %q tidak didukung", h.Alg)
	}
	key, err := v.keyFor(h.Kid)
	if err != nil {
		return Claims{}, err
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return Claims{}, errors.New("token tidak valid")
	}
	if !ed25519.Verify(key, []byte(parts[0]+"."+parts[1]), sig) {
		return Claims{}, errors.New("tanda tangan token tidak cocok")
	}
	cb, err := b64.DecodeString(parts[1])
	if err != nil {
		return Claims{}, errors.New("token tidak valid")
	}
	var c Claims
	if err := json.Unmarshal(cb, &c); err != nil {
		return Claims{}, errors.New("token tidak valid")
	}
	if c.ExpiresAt > 0 && time.Now().Unix() > c.ExpiresAt {
		return Claims{}, errors.New("token kedaluwarsa")
	}
	if v.opts.Issuer != "" && c.Issuer != v.opts.Issuer {
		return Claims{}, errors.New("issuer token tidak dikenal")
	}
	return c, nil
}

func (v *Verifier) keyFor(kid string) (ed25519.PublicKey, error) {
	v.mu.RLock()
	if k, ok := v.keys[kid]; ok {
		v.mu.RUnlock()
		return k, nil
	}
	v.mu.RUnlock()
	if err := v.fetchJWKS(); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, errors.New("kunci verifikasi (kid) tidak dikenal")
}

func (v *Verifier) fetchJWKS() error {
	resp, err := v.client.Get(v.opts.JWKSURL)
	if err != nil {
		return fmt.Errorf("ambil JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ambil JWKS: status %d", resp.StatusCode)
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			Kid string `json:"kid"`
			X   string `json:"x"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}
	fresh := make(map[string]ed25519.PublicKey)
	for _, k := range set.Keys {
		if k.Kty != "OKP" || k.Crv != "Ed25519" {
			continue
		}
		raw, err := b64.DecodeString(k.X)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			continue
		}
		fresh[k.Kid] = ed25519.PublicKey(raw)
	}
	if len(fresh) == 0 {
		return errors.New("JWKS tidak berisi kunci Ed25519")
	}
	v.mu.Lock()
	v.keys = fresh
	v.mu.Unlock()
	return nil
}
