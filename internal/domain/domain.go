// Package domain holds the core types of the Konsumen (consumer portal) service
// and the auto-routing table that maps a report category to the division that
// should handle it. This is the single source of truth both the API and the UI
// derive from (the UI fetches GET /api/categories).
package domain

import "time"

// Division is the internal Greenpark department a consumer report is routed to.
// The keys match the department keys used across the dashboard SSO roles.
type Division string

const (
	DivTeknik      Division = "teknik"      // construction / building QA
	DivKeuangan    Division = "keuangan"    // payments / billing
	DivLegal       Division = "legal"       // legal / permit / certificates
	DivPerencanaan Division = "perencanaan" // design / plan changes
	DivSales       Division = "sales"       // CS / general questions
)

// Category is a kind of report a consumer can file. Each maps to exactly one
// Division via the routing table — that mapping is the "auto-route berdasarkan
// jenis laporan" behaviour the product asked for.
type Category struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Icon        string   `json:"icon"` // emoji shown in the UI chip
	Division    Division `json:"division"`
	DivisiLabel string   `json:"divisiLabel"`
	Description string   `json:"description"`
}

// Categories is the ordered routing table. Add a row here and both the API and
// the consumer UI pick it up automatically.
var Categories = []Category{
	{"progres", "Progres Pembangunan", "🏗️", DivTeknik, "Teknik", "Minta update / lihat progres pembangunan unit Anda."},
	{"komplain_bangunan", "Komplain & Perbaikan", "🔧", DivTeknik, "Teknik", "Laporkan kerusakan / kekurangan bangunan untuk diperbaiki."},
	{"pembayaran", "Bukti Pembayaran", "🧾", DivKeuangan, "Keuangan", "Unggah bukti transfer / cicilan Anda."},
	{"tagihan", "Pertanyaan Tagihan", "💳", DivKeuangan, "Keuangan", "Tanya soal tagihan, cicilan, atau kwitansi."},
	{"dokumen_legal", "Dokumen Legal", "📜", DivLegal, "Legal & Perizinan", "Sertifikat, AJB, IMB/PBG, dan dokumen legal lain."},
	{"akad", "Jadwal Akad / KPR", "🏦", DivLegal, "Legal & Perizinan", "Koordinasi jadwal akad kredit / KPR."},
	{"perubahan_desain", "Perubahan Desain", "📐", DivPerencanaan, "Perencanaan", "Ajukan permintaan perubahan denah / spesifikasi."},
	{"pertanyaan_umum", "Pertanyaan Umum", "💬", DivSales, "Sales / CS", "Pertanyaan lain atau butuh bantuan tim kami."},
}

// CategoryByKey returns the routing row for a category key, ok=false if unknown.
func CategoryByKey(key string) (Category, bool) {
	for _, c := range Categories {
		if c.Key == key {
			return c, true
		}
	}
	return Category{}, false
}

// PostStatus is the lifecycle of a routed report as the owning division works it.
type PostStatus string

const (
	StatusTerkirim  PostStatus = "terkirim"  // received, not yet picked up
	StatusDiproses  PostStatus = "diproses"  // a division staffer is handling it
	StatusSelesai   PostStatus = "selesai"   // resolved
	StatusDitolak   PostStatus = "ditolak"   // rejected / not actionable
)

// Pemohon holds the applicant/deal fields shared by an Invite and an activated
// Consumer. Core fields relate to the Sales "Konsumen Screening" (ScreeningID +
// Eligibility carried over on promote); Profile is a freeform bag for the extra
// master-DB columns (NIK address, pekerjaan, dll) so we don't rigidly duplicate
// the whole DATABASE KONSUMEN schema.
type Pemohon struct {
	Blok        string            `json:"blok,omitempty"`
	Type        string            `json:"type,omitempty"` // house type (EMERALD, JADE, …)
	NIK         string            `json:"nik,omitempty"`
	Email       string            `json:"email,omitempty"`
	CaraBayar   string            `json:"caraBayar,omitempty"` // cash | kpr | cash_bertahap
	Harga       int64             `json:"harga,omitempty"`     // harga jual (Rp)
	PlafonKPR   int64             `json:"plafonKpr,omitempty"`
	NamaSales   string            `json:"namaSales,omitempty"`
	ScreeningID string            `json:"screeningId,omitempty"` // link to Sales screening submission
	Eligibility string            `json:"eligibility,omitempty"` // screening verdict snapshot
	Profile     map[string]string `json:"profile,omitempty"`     // extra master-DB fields
}

// Invite is a one-time code Sales issues so a specific buyer can activate an
// account already bound to their unit/deal.
type Invite struct {
	Code       string     `json:"code"`
	Name       string     `json:"name"`
	Phone      string     `json:"phone"`
	Unit       string     `json:"unit"` // kavling / unit label
	Project    string     `json:"project"`
	CreatedBy  string     `json:"createdBy"` // sales username (SSO subject/username)
	CreatedAt  time.Time  `json:"createdAt"`
	UsedAt     *time.Time `json:"usedAt,omitempty"`
	ConsumerID string     `json:"consumerId,omitempty"`
	Pemohon
}

// Consumer is an activated buyer account.
type Consumer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Unit      string    `json:"unit"`
	Project   string    `json:"project"`
	Avatar    string    `json:"avatar"` // attachment id or empty
	CreatedAt time.Time `json:"createdAt"`
	Pemohon
}

// Attachment is one uploaded file bound to a post (or a consumer avatar).
type Attachment struct {
	ID          string    `json:"id"`
	PostID      string    `json:"postId,omitempty"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	Kind        string    `json:"kind"` // image | video | file
	CreatedAt   time.Time `json:"createdAt"`
}

// Post is a consumer report/update in the feed. It carries its category, the
// division it auto-routed to, its attachments, and the division's response.
type Post struct {
	ID          string       `json:"id"`
	ConsumerID  string       `json:"consumerId"`
	Consumer    *Consumer    `json:"consumer,omitempty"` // hydrated for feed views
	Category    string       `json:"category"`
	Division    Division     `json:"division"`
	Caption     string       `json:"caption"`
	Status      PostStatus   `json:"status"`
	Attachments []Attachment `json:"attachments"`
	Likes       int          `json:"likes"`
	Liked       bool         `json:"liked"`

	// Division response.
	HandledBy   string     `json:"handledBy,omitempty"`
	Response    string     `json:"response,omitempty"`
	RespondedAt *time.Time `json:"respondedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}
