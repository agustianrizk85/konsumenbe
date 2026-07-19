package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"greenpark/konsumen/internal/db"
	"greenpark/konsumen/internal/domain"
)

// consumer resolves the caller's session token to a consumer, or writes 401.
func (h *Handler) consumer(w http.ResponseWriter, r *http.Request) (domain.Consumer, bool) {
	tok := bearer(r)
	if tok == "" {
		writeErr(w, http.StatusUnauthorized, "sesi tidak ditemukan, silakan masuk")
		return domain.Consumer{}, false
	}
	c, err := h.store.ConsumerBySession(tok)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "sesi berakhir, silakan masuk lagi")
		return domain.Consumer{}, false
	}
	return c, true
}

// categories exposes the routing table so the UI shows the same jenis-laporan
// chips and destination divisions the backend routes by.
func (h *Handler) categories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"categories": domain.Categories})
}

// checkInvite previews an invite code before the buyer sets a password.
func (h *Handler) checkInvite(w http.ResponseWriter, r *http.Request) {
	var body struct{ Code string `json:"code"` }
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "permintaan tidak valid")
		return
	}
	inv, err := h.store.GetInvite(body.Code)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "kode undangan tidak ditemukan")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memeriksa kode")
		return
	}
	if inv.UsedAt != nil {
		writeErr(w, http.StatusConflict, "kode undangan sudah dipakai")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": inv.Name, "unit": inv.Unit, "project": inv.Project, "phone": inv.Phone,
	})
}

// activate turns an invite into an account + session.
func (h *Handler) activate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "permintaan tidak valid")
		return
	}
	if len(strings.TrimSpace(body.Password)) < 6 {
		writeErr(w, http.StatusBadRequest, "kata sandi minimal 6 karakter")
		return
	}
	c, token, err := h.store.Activate(body.Code, body.Password, h.sessionTTL)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "kode undangan tidak ditemukan")
		return
	}
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "consumer": c})
}

// login authenticates an existing consumer by phone + password.
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone    string `json:"phone"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "permintaan tidak valid")
		return
	}
	c, token, err := h.store.Login(body.Phone, body.Password, h.sessionTTL)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "nomor HP atau kata sandi salah")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "consumer": c})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if tok := bearer(r); tok != "" {
		_ = h.store.Logout(tok)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	c, ok := h.consumer(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"consumer": c})
}

func (h *Handler) feed(w http.ResponseWriter, r *http.Request) {
	c, ok := h.consumer(w, r)
	if !ok {
		return
	}
	posts, err := h.store.FeedForConsumer(c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat feed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

// createPost accepts a multipart form: category, caption, and files[]. The
// division is derived from the category (auto-route) — the client never picks it.
func (h *Handler) createPost(w http.ResponseWriter, r *http.Request) {
	c, ok := h.consumer(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(h.maxUpload); err != nil {
		writeErr(w, http.StatusBadRequest, "gagal membaca unggahan (file terlalu besar?)")
		return
	}
	category := strings.TrimSpace(r.FormValue("category"))
	caption := strings.TrimSpace(r.FormValue("caption"))
	cat, ok := domain.CategoryByKey(category)
	if !ok {
		writeErr(w, http.StatusBadRequest, "jenis laporan tidak dikenal")
		return
	}
	files := r.MultipartForm.File["files"]
	if caption == "" && len(files) == 0 {
		writeErr(w, http.StatusBadRequest, "isi keterangan atau lampirkan minimal satu file")
		return
	}

	post, err := h.store.CreatePost(c.ID, cat.Key, cat.Division, caption)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan laporan")
		return
	}

	for _, fh := range files {
		if fh.Size > h.maxUpload {
			continue
		}
		src, err := fh.Open()
		if err != nil {
			continue
		}
		ct := contentTypeFor(fh.Header.Get("Content-Type"), fh.Filename)
		att, err := h.store.AddAttachment(post.ID, fh.Filename, ct, kindFor(ct), fh.Size)
		if err != nil {
			src.Close()
			continue
		}
		if _, err := h.saveFile(att.ID, src); err != nil {
			src.Close()
			continue
		}
		src.Close()
	}

	full, err := h.store.Post(post.ID, c.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"post": post})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": full})
}

func (h *Handler) getPost(w http.ResponseWriter, r *http.Request) {
	c, ok := h.consumer(w, r)
	if !ok {
		return
	}
	post, err := h.store.Post(r.PathValue("id"), c.ID)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "laporan tidak ditemukan")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat laporan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": post})
}

func (h *Handler) likePost(w http.ResponseWriter, r *http.Request) {
	c, ok := h.consumer(w, r)
	if !ok {
		return
	}
	liked, count, err := h.store.ToggleLike(r.PathValue("id"), c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memperbarui suka")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"liked": liked, "likes": count})
}

// setAvatar uploads a single image and points the consumer's avatar at it.
func (h *Handler) setAvatar(w http.ResponseWriter, r *http.Request) {
	c, ok := h.consumer(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(h.maxUpload); err != nil {
		writeErr(w, http.StatusBadRequest, "gagal membaca unggahan")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file avatar tidak ditemukan")
		return
	}
	defer file.Close()
	ct := contentTypeFor(hdr.Header.Get("Content-Type"), hdr.Filename)
	att, err := h.store.AddStandaloneAttachment(hdr.Filename, ct, kindFor(ct), hdr.Size)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan avatar")
		return
	}
	if _, err := h.saveFile(att.ID, file); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan avatar")
		return
	}
	if err := h.store.SetAvatar(c.ID, att.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memperbarui avatar")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"avatar": att.ID})
}

// serveFile streams an attachment. Any authenticated caller (consumer session
// or staff SSO) may view it, so divisions can open what buyers attached.
func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request) {
	tok := bearer(r)
	authorised := false
	if tok != "" {
		if _, err := h.store.ConsumerBySession(tok); err == nil {
			authorised = true
		} else if h.sso != nil {
			if _, err := h.sso.Verify(tok); err == nil {
				authorised = true
			}
		}
	}
	if !authorised {
		writeErr(w, http.StatusUnauthorized, "tidak diizinkan")
		return
	}
	att, err := h.store.Attachment(r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat file")
		return
	}
	f, err := os.Open(filepath.Join(h.uploadDir, att.ID))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", att.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Disposition", "inline; filename=\""+sanitizeFilename(att.Filename)+"\"")
	http.ServeContent(w, r, att.Filename, att.CreatedAt, f)
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\r", "")
	return name
}

/* -------------------------------------------------------- berkas (documents) */

// storeDocument is the shared multipart upload for a berkas (docType + file),
// used by both the consumer self-upload and the Sales on-behalf upload.
func (h *Handler) storeDocument(w http.ResponseWriter, r *http.Request, consumerID, uploadedBy string) {
	if err := r.ParseMultipartForm(h.maxUpload); err != nil {
		writeErr(w, http.StatusBadRequest, "gagal membaca unggahan (file terlalu besar?)")
		return
	}
	docType := strings.TrimSpace(r.FormValue("docType"))
	if _, ok := domain.DocumentTypeByKey(docType); !ok {
		writeErr(w, http.StatusBadRequest, "jenis berkas tidak dikenal")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file berkas tidak ditemukan")
		return
	}
	defer file.Close()
	ct := contentTypeFor(hdr.Header.Get("Content-Type"), hdr.Filename)
	doc, err := h.store.AddDocument(consumerID, docType, hdr.Filename, ct, hdr.Size, uploadedBy)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan berkas")
		return
	}
	if _, err := h.saveFile(doc.ID, file); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan berkas")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"document": doc})
}

// myDocuments returns the caller's berkas + checklist + completeness.
func (h *Handler) myDocuments(w http.ResponseWriter, r *http.Request) {
	c, ok := h.consumer(w, r)
	if !ok {
		return
	}
	docs, err := h.store.ListDocuments(c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat berkas")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"documents": docs, "checklist": domain.DocumentTypes, "berkas": berkasFor(c.CaraBayar, docs),
	})
}

// uploadMyDocument lets the pemohon upload one of their own berkas.
func (h *Handler) uploadMyDocument(w http.ResponseWriter, r *http.Request) {
	c, ok := h.consumer(w, r)
	if !ok {
		return
	}
	h.storeDocument(w, r, c.ID, "konsumen")
}

// serveDocument streams a berkas file. A pemohon may view only their own; staff
// (Sales / Legal / super) may view any — these are sensitive scans (KTP, rekening
// koran), so access is stricter than post attachments.
func (h *Handler) serveDocument(w http.ResponseWriter, r *http.Request) {
	tok := bearer(r)
	var consumerID string
	staff := false
	if tok != "" {
		if cons, err := h.store.ConsumerBySession(tok); err == nil {
			consumerID = cons.ID
		} else if h.sso != nil {
			if claims, err := h.sso.Verify(tok); err == nil {
				staff = claims.Super || canDivision(claims, domain.DivSales) || canDivision(claims, domain.DivLegal)
			}
		}
	}
	if consumerID == "" && !staff {
		writeErr(w, http.StatusUnauthorized, "tidak diizinkan")
		return
	}
	doc, err := h.store.Document(r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal memuat berkas")
		return
	}
	if !staff && doc.ConsumerID != consumerID {
		writeErr(w, http.StatusForbidden, "bukan berkas Anda")
		return
	}
	f, err := os.Open(filepath.Join(h.uploadDir, doc.ID))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Disposition", "inline; filename=\""+sanitizeFilename(doc.Filename)+"\"")
	http.ServeContent(w, r, doc.Filename, doc.CreatedAt, f)
}
