// Package config loads runtime configuration from environment variables with
// sensible defaults, so the Konsumen service runs out of the box for local dev.
package config

import "os"

// Config holds the server runtime configuration.
type Config struct {
	Port        string // HTTP port to listen on (default 8092)
	AllowOrigin string // CORS allowed origin (default *)
	DatabaseURL string // PostgreSQL DSN (shared greenpark DB on :5434)
	UploadDir   string // where consumer file attachments are stored
	MaxUploadMB int64  // per-file upload ceiling (MB)

	// Staff SSO: the master auth service JWKS so this backend can accept the
	// unified dashboard login token for division-facing endpoints. Empty ->
	// SSO acceptance stays off (staff endpoints refuse until configured).
	JWKSURL string
	Issuer  string

	// SalesAPIBase is the Sales division backend, used to relate a pemohon to an
	// existing "Konsumen Screening" submission (reuse, don't re-type the data).
	SalesAPIBase string
}

// Load reads configuration from the environment, applying defaults.
func Load() Config {
	return Config{
		Port:        getenv("KONSUMEN_PORT", "8092"),
		AllowOrigin: getenv("KONSUMEN_ALLOW_ORIGIN", "*"),
		DatabaseURL: getenv("KONSUMEN_DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5434/greenpark?sslmode=disable"),
		UploadDir:   getenv("KONSUMEN_UPLOAD_DIR", "uploads"),
		MaxUploadMB: 25,
		JWKSURL:     getenv("AUTH_JWKS_URL", "http://127.0.0.1:8090/.well-known/jwks.json"),
		Issuer:      os.Getenv("AUTH_ISSUER"),
		SalesAPIBase: getenv("SALES_API_BASE", "http://127.0.0.1:8085"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
