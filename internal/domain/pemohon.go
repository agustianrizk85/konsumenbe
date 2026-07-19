package domain

import "time"

// This file adds the "pemohon" (applicant) layer on top of the base consumer:
// the berkas (document) checklist a buyer must complete for CASH/KPR, and the
// stored Document records. It complements the Sales "Konsumen Screening"
// relation (backend/sales): a screened prospect is promoted to a pemohon account
// here, carrying over name/phone/unit/price/eligibility, then collects berkas.

// DocStatus is the verification state of one uploaded berkas.
type DocStatus string

const (
	DocPending  DocStatus = "pending"  // uploaded, awaiting Sales/Legal verification
	DocVerified DocStatus = "verified" // checked & accepted
	DocRejected DocStatus = "rejected" // needs re-upload (see Note)
)

// DocumentType is one required/optional berkas in the KPR/CASH checklist. The
// UI renders the same list (GET /api/document-types); required-ness depends on
// the pemohon's cara bayar.
type DocumentType struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Icon         string `json:"icon"`
	Hint         string `json:"hint,omitempty"`
	RequiredCash bool   `json:"requiredCash"`
	RequiredKPR  bool   `json:"requiredKpr"`
}

// DocumentTypes is the standard Greenpark buyer document checklist. "rekening
// koran", "slip gaji", KTP, KK, NPWP mirror the Sales screening's document
// question (scq-docs).
var DocumentTypes = []DocumentType{
	{"ktp", "KTP", "🪪", "KTP pemohon (dan pasangan bila menikah)", true, true},
	{"kk", "Kartu Keluarga", "👪", "KK terbaru", true, true},
	{"foto", "Pas Foto", "📷", "Pas foto pemohon", true, true},
	{"bukti_bf", "Bukti Booking Fee", "🧾", "Bukti bayar booking fee", true, true},
	{"surat_nikah", "Surat Nikah / Cerai", "💍", "Bila sudah menikah / pernah cerai", false, false},
	{"npwp", "NPWP", "🔖", "Nomor Pokok Wajib Pajak", false, true},
	{"slip_gaji", "Slip Gaji / Ket. Penghasilan", "💵", "3 bulan terakhir / surat keterangan penghasilan", false, true},
	{"rekening_koran", "Rekening Koran", "🏦", "Mutasi rekening 3 bulan terakhir", false, true},
	{"sk_kerja", "Surat Keterangan Kerja", "📄", "SK pengangkatan / surat keterangan kerja", false, true},
	{"sppr", "SPPR", "📝", "Surat Pemesanan / Persetujuan Pembelian Rumah", false, false},
	{"sp3", "SP3 Bank", "✅", "Surat Persetujuan Prinsip dari bank", false, false},
	{"lainnya", "Dokumen Lain", "📎", "Berkas pendukung lainnya", false, false},
}

// DocumentTypeByKey looks up a checklist entry.
func DocumentTypeByKey(key string) (DocumentType, bool) {
	for _, d := range DocumentTypes {
		if d.Key == key {
			return d, true
		}
	}
	return DocumentType{}, false
}

// Document is one uploaded berkas bound to a pemohon (consumer).
type Document struct {
	ID          string     `json:"id"`
	ConsumerID  string     `json:"consumerId"`
	DocType     string     `json:"docType"`
	Label       string     `json:"label"` // hydrated from the checklist for display
	Filename    string     `json:"filename"`
	ContentType string     `json:"contentType"`
	Size        int64      `json:"size"`
	Status      DocStatus  `json:"status"`
	Note        string     `json:"note,omitempty"`
	UploadedBy  string     `json:"uploadedBy"` // "konsumen" or a sales username
	VerifiedBy  string     `json:"verifiedBy,omitempty"`
	VerifiedAt  *time.Time `json:"verifiedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// BerkasStatus summarises checklist completeness for a pemohon.
type BerkasStatus string

const (
	BerkasBelum    BerkasStatus = "belum"    // no required doc yet
	BerkasSebagian BerkasStatus = "sebagian" // some required present
	BerkasLengkap  BerkasStatus = "lengkap"  // all required present (not rejected)
)

// RequiredDocTypes returns the checklist keys required for a given cara bayar
// ("kpr" vs anything else = cash). KPR needs the full financial set.
func RequiredDocTypes(caraBayar string) []string {
	kpr := caraBayar == "kpr" || caraBayar == "KPR"
	var out []string
	for _, d := range DocumentTypes {
		if (kpr && d.RequiredKPR) || (!kpr && d.RequiredCash) {
			out = append(out, d.Key)
		}
	}
	return out
}

// ComputeBerkas returns (satisfied, requiredTotal, overall status) given the
// pemohon's cara bayar and the set of doc types that have a non-rejected upload.
func ComputeBerkas(caraBayar string, present map[string]bool) (int, int, BerkasStatus) {
	req := RequiredDocTypes(caraBayar)
	got := 0
	for _, k := range req {
		if present[k] {
			got++
		}
	}
	total := len(req)
	switch {
	case total == 0 || got == 0:
		if got > 0 {
			return got, total, BerkasLengkap
		}
		return got, total, BerkasBelum
	case got >= total:
		return got, total, BerkasLengkap
	default:
		return got, total, BerkasSebagian
	}
}
