# Greenpark Konsumen

Portal konsumen bergaya **Instagram / TikTok** untuk pembeli Greenpark. Konsumen
mengunggah laporan (progres, pembayaran, dokumen, komplain) beserta foto/video/
dokumen, lalu laporan **otomatis diarahkan ke divisi yang tepat** (auto-route
berdasarkan jenis laporan). Divisi menanggapi dari dashboard internal, dan
konsumen melihat status + balasan langsung di feed-nya.

```
konsumen/
├── backend/          Go API (:8092) — Postgres, staff SSO, upload file
├── web/              Vite + React + TypeScript (:5175) — feed IG/TikTok
├── run-konsumen.ps1  jalankan keduanya sekaligus (dev)
└── README.md
```

## Arsitektur singkat

- **Frontend** (`web`): React + Vite, mobile-first (frame HP), tema gelap aksen
  hijau. Halaman: Welcome (aktivasi/masuk), Feed (kartu ala Instagram), Compose
  (buat laporan + lampiran), Profil (grid ala Instagram), Detail laporan.
- **Backend** (`backend`): Go, PostgreSQL (DB `greenpark` bersama di `:5434`).
  Tabel `konsumen_*` (invites, consumers, sessions, posts, attachments, likes).
  File tersimpan di `backend/uploads/`, metadata di DB.
- **Dua jenis identitas**:
  - **Konsumen** — akun pembeli, diaktivasi lewat **kode undangan dari Sales**,
    login pakai No HP + kata sandi (token sesi opaque, 30 hari).
  - **Staf** — Sales & divisi, pakai **token SSO Greenpark** (Ed25519 via JWKS
    `:8090`) — satu login sama seperti dashboard, tanpa bridge.

## Auto-routing (jenis laporan → divisi)

| Jenis laporan            | Divisi tujuan       |
|--------------------------|---------------------|
| Progres Pembangunan      | Teknik              |
| Komplain & Perbaikan     | Teknik              |
| Bukti Pembayaran         | Keuangan            |
| Pertanyaan Tagihan       | Keuangan            |
| Dokumen Legal            | Legal & Perizinan   |
| Jadwal Akad / KPR        | Legal & Perizinan   |
| Perubahan Desain         | Perencanaan         |
| Pertanyaan Umum          | Sales / CS          |

Tabel ini ada di `backend/internal/domain/domain.go` (`Categories`) dan otomatis
ikut ke UI lewat `GET /api/categories` — tambah satu baris, FE & BE langsung
ikut.

## Menjalankan (dev)

Prasyarat (lihat catatan stack lokal Greenpark):
- Postgres jalan (Docker `greenpark-db`, host `:5434`).
- Auth SSO jalan di `:8090` (untuk endpoint Sales & divisi).

Sekaligus:
```powershell
powershell -ExecutionPolicy Bypass -File .\run-konsumen.ps1
```

Manual:
```powershell
# Backend
cd backend
$env:KONSUMEN_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:5434/greenpark?sslmode=disable"
$env:AUTH_JWKS_URL="http://127.0.0.1:8090/.well-known/jwks.json"
go run ./cmd/server            # http://localhost:8092

# Frontend (terminal lain)
cd web
npm install
npm run dev                    # http://localhost:5175
```

> Backend **wajib PowerShell** (bukan Bash tool) supaya `go` ada di PATH.

## Alur uji cepat

1. Login staf (mis. superadmin) → **buat undangan** di `POST /api/sales/invites`
   (nama, no HP, unit, proyek) → dapat **kode** `GP-XXXXXX`.
2. Konsumen buka web → **Aktivasi akun** → masukkan kode → buat kata sandi →
   masuk.
3. Konsumen **buat laporan** (pilih jenis → lampirkan file → kirim). Laporan
   otomatis ter-route ke divisi.
4. Staf divisi buka **inbox** (`GET /api/divisi/inbox?division=teknik`) →
   **tanggapi** (`POST /api/divisi/posts/{id}/respond`, status + balasan).
5. Konsumen lihat status + balasan langsung di feed.

## Konfigurasi (env backend)

| Env                     | Default                                                        |
|-------------------------|----------------------------------------------------------------|
| `KONSUMEN_PORT`         | `8092`                                                         |
| `KONSUMEN_DATABASE_URL` | `postgres://postgres:postgres@127.0.0.1:5434/greenpark?...`    |
| `KONSUMEN_UPLOAD_DIR`   | `uploads`                                                     |
| `AUTH_JWKS_URL`         | `http://127.0.0.1:8090/.well-known/jwks.json`                 |
| `KONSUMEN_ALLOW_ORIGIN` | `*`                                                           |

## Endpoint utama

Publik: `GET /api/health`, `GET /api/categories`

Konsumen (Bearer token sesi):
`POST /api/consumer/invite/check`, `/activate`, `/login`, `/logout`,
`GET /api/consumer/me`, `/feed`, `POST /api/consumer/posts` (multipart),
`GET /api/consumer/posts/{id}`, `POST /api/consumer/posts/{id}/like`,
`POST /api/consumer/avatar`, `GET /api/files/{id}?token=…`

Staf (Bearer token SSO):
`POST /api/sales/invites`, `GET /api/sales/invites`,
`GET /api/divisi/me`, `/inbox?division=…`, `/summary?division=…`,
`POST /api/divisi/posts/{id}/respond`

## Catatan produksi

- Kata sandi konsumen di-hash SHA-256 + salt (cukup untuk MVP internal). Sebelum
  dibuka ke publik skala besar, ganti ke bcrypt/argon2.
- Di prod, sajikan `web` di belakang satu origin dan proxy `/be/konsumen/api/*`
  → `127.0.0.1:8092/api/*` (lihat `.env.production`).
