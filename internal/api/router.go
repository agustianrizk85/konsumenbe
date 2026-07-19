// Package api is the HTTP transport for the Konsumen service. It exposes three
// audiences on one server:
//   - public:   health + the category/routing table
//   - consumer: activated buyers, authenticated by an opaque session token
//   - staff:    Sales (issue invites) and divisions (inbox + respond),
//     authenticated by the unified Greenpark SSO token (Ed25519 via JWKS)
package api

import (
	"net/http"
	"time"

	"greenpark/konsumen/internal/authmw"
	"greenpark/konsumen/internal/db"
)

// Handler carries the dependencies shared by every route.
type Handler struct {
	store        *db.Store
	sso          *authmw.Verifier // may be nil if SSO not configured (staff routes 503)
	uploadDir    string
	maxUpload    int64 // bytes
	allowOrigin  string
	salesAPIBase string // Sales backend, for the screening relation
	sessionTTL   time.Duration
}

// NewHandler builds the API handler.
func NewHandler(store *db.Store, sso *authmw.Verifier, uploadDir string, maxUploadMB int64, allowOrigin, salesAPIBase string) *Handler {
	return &Handler{
		store:        store,
		sso:          sso,
		uploadDir:    uploadDir,
		maxUpload:    maxUploadMB << 20,
		allowOrigin:  allowOrigin,
		salesAPIBase: salesAPIBase,
		sessionTTL:   30 * 24 * time.Hour, // consumers stay logged in ~30 days
	}
}

// Router wires all routes and applies CORS.
func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()

	// --- Public ---
	mux.HandleFunc("GET /api/health", h.health)
	mux.HandleFunc("GET /api/categories", h.categories)
	mux.HandleFunc("GET /api/document-types", h.documentTypes)

	// --- Consumer auth (no session required) ---
	mux.HandleFunc("POST /api/consumer/invite/check", h.checkInvite)
	mux.HandleFunc("POST /api/consumer/activate", h.activate)
	mux.HandleFunc("POST /api/consumer/login", h.login)

	// --- Consumer (session token) ---
	mux.HandleFunc("POST /api/consumer/logout", h.logout)
	mux.HandleFunc("GET /api/consumer/me", h.me)
	mux.HandleFunc("GET /api/consumer/feed", h.feed)
	mux.HandleFunc("POST /api/consumer/posts", h.createPost)
	mux.HandleFunc("GET /api/consumer/posts/{id}", h.getPost)
	mux.HandleFunc("POST /api/consumer/posts/{id}/like", h.likePost)
	mux.HandleFunc("POST /api/consumer/avatar", h.setAvatar)
	// Berkas (documents) the pemohon uploads themselves.
	mux.HandleFunc("GET /api/consumer/documents", h.myDocuments)
	mux.HandleFunc("POST /api/consumer/documents", h.uploadMyDocument)

	// --- Files (consumer session OR staff SSO) ---
	mux.HandleFunc("GET /api/files/{id}", h.serveFile)
	mux.HandleFunc("GET /api/documents/{id}/file", h.serveDocument)

	// --- Staff: Sales issues invites ---
	mux.HandleFunc("POST /api/sales/invites", h.createInvite)
	mux.HandleFunc("GET /api/sales/invites", h.listInvites)

	// --- Staff: Sales pemohon (applicant) management + berkas ---
	mux.HandleFunc("POST /api/sales/pemohon", h.createPemohon)
	mux.HandleFunc("GET /api/sales/pemohon", h.listPemohon)
	mux.HandleFunc("GET /api/sales/pemohon/{id}", h.getPemohon)
	mux.HandleFunc("POST /api/sales/pemohon/{id}/documents", h.uploadPemohonDocument)
	mux.HandleFunc("PATCH /api/sales/documents/{id}", h.verifyDocument)
	// Relation to the Sales division: list screened prospects to promote.
	mux.HandleFunc("GET /api/sales/screenings", h.salesScreenings)

	// --- Staff: division inbox + respond ---
	mux.HandleFunc("GET /api/divisi/me", h.staffMe)
	mux.HandleFunc("GET /api/divisi/inbox", h.divisionInbox)
	mux.HandleFunc("GET /api/divisi/summary", h.divisionSummary)
	mux.HandleFunc("POST /api/divisi/posts/{id}/respond", h.respondPost)

	return h.cors(mux)
}

// cors applies permissive CORS (the dashboard proxies in prod; dev is cross-port).
func (h *Handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := h.allowOrigin
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "konsumen"})
}
