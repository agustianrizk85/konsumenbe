package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"greenpark/konsumen/internal/db"
	"greenpark/konsumen/internal/domain"
)

// berkasSummary describes checklist completeness for one pemohon.
type berkasSummary struct {
	CaraBayar string                `json:"caraBayar"`
	Satisfied int                   `json:"satisfied"`
	Required  int                   `json:"required"`
	Status    domain.BerkasStatus   `json:"status"`
	Missing   []domain.DocumentType `json:"missing"`
}

func berkasFor(caraBayar string, docs []domain.Document) berkasSummary {
	present := map[string]bool{}
	for _, d := range docs {
		if d.Status != domain.DocRejected {
			present[d.DocType] = true
		}
	}
	got, total, status := domain.ComputeBerkas(caraBayar, present)
	missing := []domain.DocumentType{}
	for _, k := range domain.RequiredDocTypes(caraBayar) {
		if !present[k] {
			if dt, ok := domain.DocumentTypeByKey(k); ok {
				missing = append(missing, dt)
			}
		}
	}
	return berkasSummary{CaraBayar: caraBayar, Satisfied: got, Required: total, Status: status, Missing: missing}
}

// documentTypes exposes the berkas checklist (public — the UI needs it before login).
func (h *Handler) documentTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"documentTypes": domain.DocumentTypes})
}

// createPemohon lets Sales create an applicant account, optionally carried over
// from a Sales "Konsumen Screening" (screeningId + eligibility). Returns the
// invite code the buyer uses to activate.
func (h *Handler) createPemohon(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	if !canDivision(c, domain.DivSales) {
		writeErr(w, http.StatusForbidden, "hanya tim Sales yang dapat membuat pemohon")
		return
	}
	var body struct {
		Name        string            `json:"name"`
		Phone       string            `json:"phone"`
		Unit        string            `json:"unit"`
		Project     string            `json:"project"`
		Blok        string            `json:"blok"`
		Type        string            `json:"type"`
		NIK         string            `json:"nik"`
		Email       string            `json:"email"`
		CaraBayar   string            `json:"caraBayar"`
		Harga       int64             `json:"harga"`
		PlafonKPR   int64             `json:"plafonKpr"`
		NamaSales   string            `json:"namaSales"`
		ScreeningID string            `json:"screeningId"`
		Eligibility string            `json:"eligibility"`
		Profile     map[string]string `json:"profile"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "permintaan tidak valid")
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Phone) == "" {
		writeErr(w, http.StatusBadRequest, "nama dan nomor HP wajib diisi")
		return
	}
	namaSales := strings.TrimSpace(body.NamaSales)
	if namaSales == "" {
		namaSales = c.Name
	}
	inv := domain.Invite{
		Name: strings.TrimSpace(body.Name), Phone: strings.TrimSpace(body.Phone),
		Unit: strings.TrimSpace(body.Unit), Project: strings.TrimSpace(body.Project),
		CreatedBy: staffUsername(c),
		Pemohon: domain.Pemohon{
			Blok: strings.TrimSpace(body.Blok), Type: strings.TrimSpace(body.Type),
			NIK: strings.TrimSpace(body.NIK), Email: strings.TrimSpace(body.Email),
			CaraBayar: strings.ToLower(strings.TrimSpace(body.CaraBayar)), Harga: body.Harga,
			PlafonKPR: body.PlafonKPR, NamaSales: namaSales,
			ScreeningID: strings.TrimSpace(body.ScreeningID), Eligibility: strings.TrimSpace(body.Eligibility),
			Profile: body.Profile,
		},
	}
	created, err := h.store.CreateInvite(inv)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membuat pemohon")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invite": created})
}

// pemohonRow is one entry in the Sales roster.
type pemohonRow struct {
	domain.Consumer
	Berkas berkasSummary `json:"berkas"`
}

// listPemohon returns activated pemohon (with berkas completeness) plus still-
// pending invites (created, not yet activated).
func (h *Handler) listPemohon(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	if !canDivision(c, domain.DivSales) {
		writeErr(w, http.StatusForbidden, "hanya tim Sales yang dapat melihat pemohon")
		return
	}
	consumers, err := h.store.ListConsumers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat pemohon")
		return
	}
	presence, err := h.store.DocPresenceByConsumer()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat berkas")
		return
	}
	rows := make([]pemohonRow, 0, len(consumers))
	for _, cons := range consumers {
		present := presence[cons.ID]
		got, total, status := domain.ComputeBerkas(cons.CaraBayar, present)
		rows = append(rows, pemohonRow{Consumer: cons, Berkas: berkasSummary{
			CaraBayar: cons.CaraBayar, Satisfied: got, Required: total, Status: status, Missing: []domain.DocumentType{},
		}})
	}

	createdBy := staffUsername(c)
	if c.Super {
		createdBy = ""
	}
	invites, err := h.store.ListInvitesBy(createdBy)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat undangan")
		return
	}
	pending := make([]domain.Invite, 0)
	for _, inv := range invites {
		if inv.UsedAt == nil {
			pending = append(pending, inv)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pemohon": rows, "pending": pending})
}

// getPemohon returns one pemohon's profile + berkas checklist.
func (h *Handler) getPemohon(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	if !canDivision(c, domain.DivSales) {
		writeErr(w, http.StatusForbidden, "tidak diizinkan")
		return
	}
	cons, err := h.store.ConsumerByID(r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "pemohon tidak ditemukan")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat pemohon")
		return
	}
	docs, err := h.store.ListDocuments(cons.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat berkas")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"consumer": cons, "documents": docs, "berkas": berkasFor(cons.CaraBayar, docs),
		"checklist": domain.DocumentTypes,
	})
}

// uploadPemohonDocument lets Sales upload a berkas on the pemohon's behalf.
func (h *Handler) uploadPemohonDocument(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	if !canDivision(c, domain.DivSales) {
		writeErr(w, http.StatusForbidden, "tidak diizinkan")
		return
	}
	cons, err := h.store.ConsumerByID(r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "pemohon tidak ditemukan")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat pemohon")
		return
	}
	h.storeDocument(w, r, cons.ID, "sales:"+staffUsername(c))
}

// verifyDocument lets Sales verify or reject an uploaded berkas.
func (h *Handler) verifyDocument(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	if !canDivision(c, domain.DivSales) && !canDivision(c, domain.DivLegal) {
		writeErr(w, http.StatusForbidden, "tidak diizinkan")
		return
	}
	var body struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "permintaan tidak valid")
		return
	}
	status := domain.DocStatus(strings.TrimSpace(body.Status))
	switch status {
	case domain.DocPending, domain.DocVerified, domain.DocRejected:
	default:
		writeErr(w, http.StatusBadRequest, "status berkas tidak valid")
		return
	}
	doc, err := h.store.UpdateDocumentStatus(r.PathValue("id"), status, strings.TrimSpace(body.Note), staffUsername(c))
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "berkas tidak ditemukan")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memperbarui berkas")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"document": doc})
}

// salesScreenings proxies the Sales division's screened prospects so the panel
// can promote one to a pemohon (reuse the relation, don't re-type). Degrades
// gracefully to available:false if the Sales backend is down.
func (h *Handler) salesScreenings(w http.ResponseWriter, r *http.Request) {
	c, ok := h.staffClaims(w, r)
	if !ok {
		return
	}
	if !canDivision(c, domain.DivSales) {
		writeErr(w, http.StatusForbidden, "tidak diizinkan")
		return
	}
	base := strings.TrimRight(h.salesAPIBase, "/")
	req, _ := http.NewRequest(http.MethodGet, base+"/api/screening/submissions", nil)
	req.Header.Set("Authorization", "Bearer "+bearer(r))
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "submissions": []any{}, "note": "Sales backend tidak aktif"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "submissions": []any{}, "note": "Sales menolak permintaan (status " + resp.Status + ")"})
		return
	}
	var subs json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&subs); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "submissions": []any{}})
		return
	}
	// The Sales backend serialises an empty result as JSON null; normalise to an
	// empty array so the client always receives a list.
	if len(subs) == 0 || string(subs) == "null" {
		writeJSON(w, http.StatusOK, map[string]any{"available": true, "submissions": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "submissions": subs})
}
