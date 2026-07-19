package api

import (
	"errors"
	"net/http"
	"strings"

	"greenpark/konsumen/internal/authmw"
	"greenpark/konsumen/internal/db"
	"greenpark/konsumen/internal/domain"
)

// divisionRoles maps a Konsumen routing division to the SSO role keys that grant
// access to its inbox. Super admins bypass this entirely.
var divisionRoles = map[domain.Division][]string{
	domain.DivTeknik:      {"teknik"},
	domain.DivKeuangan:    {"keuangan"},
	domain.DivPerencanaan: {"perencanaan"},
	domain.DivSales:       {"sales", "cso"},
	domain.DivLegal:       {"permit", "legal"},
}

// staffClaims validates the SSO token on a staff request.
func (h *Handler) staffClaims(w http.ResponseWriter, r *http.Request) (authmw.Claims, bool) {
	if h.sso == nil {
		writeErr(w, http.StatusServiceUnavailable, "SSO belum dikonfigurasi (set AUTH_JWKS_URL)")
		return authmw.Claims{}, false
	}
	tok := bearer(r)
	if tok == "" {
		writeErr(w, http.StatusUnauthorized, "token staf tidak ditemukan")
		return authmw.Claims{}, false
	}
	claims, err := h.sso.Verify(tok)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "token staf tidak valid: "+err.Error())
		return authmw.Claims{}, false
	}
	return claims, true
}

// canDivision reports whether a staff member may work a division's inbox.
func canDivision(c authmw.Claims, div domain.Division) bool {
	if c.Super {
		return true
	}
	for _, role := range divisionRoles[div] {
		if _, ok := c.Roles[role]; ok {
			return true
		}
	}
	return false
}

// allowedDivisions lists the divisions a staff member may access.
func allowedDivisions(c authmw.Claims) []domain.Division {
	var out []domain.Division
	for _, cat := range domain.Categories {
		if canDivision(c, cat.Division) && !containsDiv(out, cat.Division) {
			out = append(out, cat.Division)
		}
	}
	return out
}

func containsDiv(list []domain.Division, d domain.Division) bool {
	for _, x := range list {
		if x == d {
			return true
		}
	}
	return false
}

func staffUsername(c authmw.Claims) string {
	if c.Username != "" {
		return c.Username
	}
	return c.Subject
}

// staffMe tells the UI which division inboxes this staffer may open.
func (h *Handler) staffMe(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username":   staffUsername(c),
		"name":       c.Name,
		"super":      c.Super,
		"divisions":  allowedDivisions(c),
	})
}

// createInvite lets Sales (or super) issue an activation code for a buyer.
func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	if !canDivision(c, domain.DivSales) {
		writeErr(w, http.StatusForbidden, "hanya tim Sales yang dapat membuat undangan")
		return
	}
	var body struct {
		Name    string `json:"name"`
		Phone   string `json:"phone"`
		Unit    string `json:"unit"`
		Project string `json:"project"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "permintaan tidak valid")
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Phone) == "" {
		writeErr(w, http.StatusBadRequest, "nama dan nomor HP wajib diisi")
		return
	}
	inv, err := h.store.CreateInvite(domain.Invite{
		Name: strings.TrimSpace(body.Name), Phone: strings.TrimSpace(body.Phone),
		Unit: strings.TrimSpace(body.Unit), Project: strings.TrimSpace(body.Project),
		CreatedBy: staffUsername(c),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membuat undangan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invite": inv})
}

// listInvites returns invites (own for sales, all for super).
func (h *Handler) listInvites(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	if !canDivision(c, domain.DivSales) {
		writeErr(w, http.StatusForbidden, "hanya tim Sales yang dapat melihat undangan")
		return
	}
	createdBy := staffUsername(c)
	if c.Super {
		createdBy = "" // all
	}
	invites, err := h.store.ListInvitesBy(createdBy)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat undangan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
}

// divisionInbox returns the reports auto-routed to a division.
func (h *Handler) divisionInbox(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	div := domain.Division(strings.TrimSpace(r.URL.Query().Get("division")))
	if div == "" {
		// Default to the caller's first accessible division for convenience.
		if ds := allowedDivisions(c); len(ds) > 0 {
			div = ds[0]
		}
	}
	if div == "" || !canDivision(c, div) {
		writeErr(w, http.StatusForbidden, "Anda tidak memiliki akses ke inbox divisi ini")
		return
	}
	posts, err := h.store.InboxForDivision(string(div))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat inbox")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"division": div, "posts": posts})
}

// divisionSummary returns per-status counts for a division (badges).
func (h *Handler) divisionSummary(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	div := domain.Division(strings.TrimSpace(r.URL.Query().Get("division")))
	if div == "" || !canDivision(c, div) {
		writeErr(w, http.StatusForbidden, "Anda tidak memiliki akses ke divisi ini")
		return
	}
	counts, err := h.store.DivisionCounts(string(div))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat ringkasan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"division": div, "counts": counts})
}

// respondPost lets a division staffer update a report's status + reply.
func (h *Handler) respondPost(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	var body struct {
		Status   string `json:"status"`
		Response string `json:"response"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "permintaan tidak valid")
		return
	}
	status := domain.PostStatus(strings.TrimSpace(body.Status))
	switch status {
	case domain.StatusTerkirim, domain.StatusDiproses, domain.StatusSelesai, domain.StatusDitolak:
	default:
		writeErr(w, http.StatusBadRequest, "status tidak valid")
		return
	}
	// Load the post to check the caller owns its division.
	post, err := h.store.Post(r.PathValue("id"), "")
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "laporan tidak ditemukan")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat laporan")
		return
	}
	if !canDivision(c, post.Division) {
		writeErr(w, http.StatusForbidden, "laporan ini bukan untuk divisi Anda")
		return
	}
	updated, err := h.store.Respond(post.ID, string(post.Division), staffUsername(c), strings.TrimSpace(body.Response), status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan tanggapan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": updated})
}
