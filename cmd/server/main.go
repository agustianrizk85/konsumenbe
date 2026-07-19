// Command server starts the Konsumen (consumer portal) API — an Instagram/
// TikTok-style feed where Greenpark buyers post updates and attach files that
// auto-route to the right internal division (Teknik / Keuangan / Legal /
// Perencanaan / Sales).
//
// It wires db -> api and runs an HTTP server with graceful shutdown.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"greenpark/konsumen/internal/api"
	"greenpark/konsumen/internal/authmw"
	"greenpark/konsumen/internal/config"
	"greenpark/konsumen/internal/db"
)

func main() {
	cfg := config.Load()

	store, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("konsumen: PostgreSQL required (no in-memory fallback): %v", err)
	}
	defer func() { _ = store.Close() }()
	log.Println("konsumen: using PostgreSQL store")

	// Staff SSO: accept the unified Greenpark dashboard token so Sales and the
	// divisions can use the internal endpoints with one login (no token bridge).
	sso := authmw.New(authmw.Options{JWKSURL: cfg.JWKSURL, Issuer: cfg.Issuer})
	if sso != nil {
		log.Printf("konsumen: staff SSO acceptance enabled (jwks=%s)", cfg.JWKSURL)
	} else {
		log.Println("konsumen: staff SSO OFF (set AUTH_JWKS_URL) — division endpoints will 503")
	}

	handler := api.NewHandler(store, sso, cfg.UploadDir, cfg.MaxUploadMB, cfg.AllowOrigin, cfg.SalesAPIBase)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("konsumen API listening on http://localhost:%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("konsumen: server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("konsumen: shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("konsumen: graceful shutdown failed: %v", err)
	}
	log.Println("konsumen: stopped")
}
