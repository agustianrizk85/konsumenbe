// Package db is the PostgreSQL persistence layer for the Konsumen service. It
// uses proper relational tables (invites, consumers, sessions, posts,
// attachments, likes) on the shared greenpark database. File bytes live on disk
// (see the api package); only their metadata is stored here.
package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"greenpark/konsumen/internal/domain"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver ("pgx")
)

// marshalProfile serialises the freeform master-DB profile bag for JSONB storage.
func marshalProfile(p map[string]string) []byte {
	if len(p) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// scanProfile deserialises a JSONB profile bag (nil-safe).
func scanProfile(raw []byte) map[string]string {
	m := map[string]string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("tidak ditemukan")

// Store is the Postgres-backed data store.
type Store struct{ db *sql.DB }

// Open connects, pings, and runs migrations. A failure is fatal to the caller —
// there is no in-memory fallback, so consumer data is always persistent.
func Open(dsn string) (*Store, error) {
	sqldb, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	sqldb.SetMaxOpenConns(10)
	sqldb.SetMaxIdleConns(5)
	sqldb.SetConnMaxLifetime(time.Hour)
	if err := sqldb.Ping(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	s := &Store{db: sqldb}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close releases the connection pool.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS konsumen_invites (
			code        TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			phone       TEXT NOT NULL,
			unit        TEXT NOT NULL DEFAULT '',
			project     TEXT NOT NULL DEFAULT '',
			created_by  TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			used_at     TIMESTAMPTZ,
			consumer_id TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS konsumen_consumers (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			phone      TEXT NOT NULL UNIQUE,
			unit       TEXT NOT NULL DEFAULT '',
			project    TEXT NOT NULL DEFAULT '',
			avatar     TEXT NOT NULL DEFAULT '',
			pw_salt    TEXT NOT NULL,
			pw_hash    TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS konsumen_sessions (
			token       TEXT PRIMARY KEY,
			consumer_id TEXT NOT NULL REFERENCES konsumen_consumers(id) ON DELETE CASCADE,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			expires_at  TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS konsumen_posts (
			id           TEXT PRIMARY KEY,
			consumer_id  TEXT NOT NULL REFERENCES konsumen_consumers(id) ON DELETE CASCADE,
			category     TEXT NOT NULL,
			division     TEXT NOT NULL,
			caption      TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL DEFAULT 'terkirim',
			handled_by   TEXT NOT NULL DEFAULT '',
			response     TEXT NOT NULL DEFAULT '',
			responded_at TIMESTAMPTZ,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS konsumen_attachments (
			id           TEXT PRIMARY KEY,
			post_id      TEXT REFERENCES konsumen_posts(id) ON DELETE CASCADE,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size         BIGINT NOT NULL DEFAULT 0,
			kind         TEXT NOT NULL DEFAULT 'file',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS konsumen_likes (
			post_id     TEXT NOT NULL REFERENCES konsumen_posts(id) ON DELETE CASCADE,
			consumer_id TEXT NOT NULL REFERENCES konsumen_consumers(id) ON DELETE CASCADE,
			PRIMARY KEY (post_id, consumer_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_konsumen_posts_consumer ON konsumen_posts(consumer_id)`,
		`CREATE INDEX IF NOT EXISTS idx_konsumen_posts_division ON konsumen_posts(division)`,

		// Pemohon (applicant) fields on invites + consumers. Added via ALTER so
		// existing installs upgrade in place. Profile JSONB holds the extra
		// master DATABASE KONSUMEN columns (address/pekerjaan/etc).
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS blok TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS nik TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS cara_bayar TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS harga BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS plafon_kpr BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS nama_sales TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS screening_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS eligibility TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_invites   ADD COLUMN IF NOT EXISTS profile JSONB NOT NULL DEFAULT '{}'`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS blok TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS nik TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS cara_bayar TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS harga BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS plafon_kpr BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS nama_sales TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS screening_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS eligibility TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE konsumen_consumers ADD COLUMN IF NOT EXISTS profile JSONB NOT NULL DEFAULT '{}'`,

		// Berkas (documents) per pemohon — the KTP/rekening-koran/slip-gaji set.
		`CREATE TABLE IF NOT EXISTS konsumen_documents (
			id           TEXT PRIMARY KEY,
			consumer_id  TEXT NOT NULL REFERENCES konsumen_consumers(id) ON DELETE CASCADE,
			doc_type     TEXT NOT NULL,
			filename     TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size         BIGINT NOT NULL DEFAULT 0,
			status       TEXT NOT NULL DEFAULT 'pending',
			note         TEXT NOT NULL DEFAULT '',
			uploaded_by  TEXT NOT NULL DEFAULT '',
			verified_by  TEXT NOT NULL DEFAULT '',
			verified_at  TIMESTAMPTZ,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_konsumen_documents_consumer ON konsumen_documents(consumer_id)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

/* ------------------------------------------------------------------ invites */

// CreateInvite issues a fresh, unused invite code for a buyer. The caller fills
// inv's identity + pemohon fields; Code/CreatedAt are assigned here.
func (s *Store) CreateInvite(inv domain.Invite) (domain.Invite, error) {
	inv.Code = newInviteCode()
	inv.CreatedAt = time.Now()
	_, err := s.db.Exec(`INSERT INTO konsumen_invites
		(code,name,phone,unit,project,created_by,created_at,
		 blok,type,nik,email,cara_bayar,harga,plafon_kpr,nama_sales,screening_id,eligibility,profile)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		inv.Code, inv.Name, inv.Phone, inv.Unit, inv.Project, inv.CreatedBy, inv.CreatedAt,
		inv.Blok, inv.Type, inv.NIK, inv.Email, inv.CaraBayar, inv.Harga, inv.PlafonKPR,
		inv.NamaSales, inv.ScreeningID, inv.Eligibility, marshalProfile(inv.Profile))
	if err != nil {
		return domain.Invite{}, err
	}
	return inv, nil
}

// inviteCols is the shared SELECT column list for invites (base + pemohon).
const inviteCols = `code,name,phone,unit,project,created_by,created_at,used_at,consumer_id,
	blok,type,nik,email,cara_bayar,harga,plafon_kpr,nama_sales,screening_id,eligibility,profile`

// scanInvite reads one invite row (columns in inviteCols order).
func scanInvite(sc interface{ Scan(...any) error }) (domain.Invite, error) {
	var inv domain.Invite
	var usedAt sql.NullTime
	var consumerID sql.NullString
	var profile []byte
	err := sc.Scan(&inv.Code, &inv.Name, &inv.Phone, &inv.Unit, &inv.Project, &inv.CreatedBy,
		&inv.CreatedAt, &usedAt, &consumerID,
		&inv.Blok, &inv.Type, &inv.NIK, &inv.Email, &inv.CaraBayar, &inv.Harga, &inv.PlafonKPR,
		&inv.NamaSales, &inv.ScreeningID, &inv.Eligibility, &profile)
	if err != nil {
		return domain.Invite{}, err
	}
	if usedAt.Valid {
		inv.UsedAt = &usedAt.Time
	}
	if consumerID.Valid {
		inv.ConsumerID = consumerID.String
	}
	inv.Profile = scanProfile(profile)
	return inv, nil
}

// GetInvite fetches an invite by code.
func (s *Store) GetInvite(code string) (domain.Invite, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	inv, err := scanInvite(s.db.QueryRow(`SELECT `+inviteCols+` FROM konsumen_invites WHERE code=$1`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Invite{}, ErrNotFound
	}
	return inv, err
}

// ListInvitesBy returns invites created by a given staff user (newest first).
// If createdBy is empty (super admin) it returns all invites.
func (s *Store) ListInvitesBy(createdBy string) ([]domain.Invite, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if createdBy == "" {
		rows, err = s.db.Query(`SELECT ` + inviteCols + ` FROM konsumen_invites ORDER BY created_at DESC`)
	} else {
		rows, err = s.db.Query(`SELECT `+inviteCols+` FROM konsumen_invites WHERE created_by=$1 ORDER BY created_at DESC`, createdBy)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Invite
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

/* --------------------------------------------------------------- consumers */

// Activate consumes an invite, creates the consumer account with a password,
// and returns the new consumer plus a fresh session token. It is transactional:
// a used or missing code fails without creating anything.
func (s *Store) Activate(code, password string, ttl time.Duration) (domain.Consumer, string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	tx, err := s.db.Begin()
	if err != nil {
		return domain.Consumer{}, "", err
	}
	defer func() { _ = tx.Rollback() }()

	var inv domain.Invite
	var usedAt sql.NullTime
	var profile []byte
	err = tx.QueryRow(`SELECT code,name,phone,unit,project,used_at,
			blok,type,nik,email,cara_bayar,harga,plafon_kpr,nama_sales,screening_id,eligibility,profile
		FROM konsumen_invites WHERE code=$1 FOR UPDATE`, code).
		Scan(&inv.Code, &inv.Name, &inv.Phone, &inv.Unit, &inv.Project, &usedAt,
			&inv.Blok, &inv.Type, &inv.NIK, &inv.Email, &inv.CaraBayar, &inv.Harga, &inv.PlafonKPR,
			&inv.NamaSales, &inv.ScreeningID, &inv.Eligibility, &profile)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Consumer{}, "", ErrNotFound
	}
	if err != nil {
		return domain.Consumer{}, "", err
	}
	if usedAt.Valid {
		return domain.Consumer{}, "", errors.New("kode undangan sudah dipakai")
	}
	inv.Profile = scanProfile(profile)

	c := domain.Consumer{
		ID: newID(), Name: inv.Name, Phone: inv.Phone, Unit: inv.Unit,
		Project: inv.Project, CreatedAt: time.Now(), Pemohon: inv.Pemohon,
	}
	salt := newToken()
	if _, err := tx.Exec(`INSERT INTO konsumen_consumers
		(id,name,phone,unit,project,pw_salt,pw_hash,created_at,
		 blok,type,nik,email,cara_bayar,harga,plafon_kpr,nama_sales,screening_id,eligibility,profile)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		c.ID, c.Name, c.Phone, c.Unit, c.Project, salt, hashPassword(password, salt), c.CreatedAt,
		c.Blok, c.Type, c.NIK, c.Email, c.CaraBayar, c.Harga, c.PlafonKPR, c.NamaSales,
		c.ScreeningID, c.Eligibility, marshalProfile(c.Profile)); err != nil {
		if isUniqueViolation(err) {
			return domain.Consumer{}, "", errors.New("nomor HP ini sudah punya akun, silakan masuk")
		}
		return domain.Consumer{}, "", err
	}
	if _, err := tx.Exec(`UPDATE konsumen_invites SET used_at=now(), consumer_id=$1 WHERE code=$2`, c.ID, code); err != nil {
		return domain.Consumer{}, "", err
	}
	token := newToken()
	if _, err := tx.Exec(`INSERT INTO konsumen_sessions (token,consumer_id,expires_at) VALUES ($1,$2,$3)`,
		token, c.ID, time.Now().Add(ttl)); err != nil {
		return domain.Consumer{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return domain.Consumer{}, "", err
	}
	return c, token, nil
}

// Login verifies phone + password and returns the consumer with a new session.
func (s *Store) Login(phone, password string, ttl time.Duration) (domain.Consumer, string, error) {
	phone = strings.TrimSpace(phone)
	var c domain.Consumer
	var salt, hash string
	var profile []byte
	err := s.db.QueryRow(`SELECT id,name,phone,unit,project,avatar,pw_salt,pw_hash,created_at,
			blok,type,nik,email,cara_bayar,harga,plafon_kpr,nama_sales,screening_id,eligibility,profile
		FROM konsumen_consumers WHERE phone=$1`, phone).
		Scan(&c.ID, &c.Name, &c.Phone, &c.Unit, &c.Project, &c.Avatar, &salt, &hash, &c.CreatedAt,
			&c.Blok, &c.Type, &c.NIK, &c.Email, &c.CaraBayar, &c.Harga, &c.PlafonKPR, &c.NamaSales,
			&c.ScreeningID, &c.Eligibility, &profile)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Consumer{}, "", ErrNotFound
	}
	c.Profile = scanProfile(profile)
	if err != nil {
		return domain.Consumer{}, "", err
	}
	if !samePassword(password, salt, hash) {
		return domain.Consumer{}, "", errors.New("nomor HP atau kata sandi salah")
	}
	token := newToken()
	if _, err := s.db.Exec(`INSERT INTO konsumen_sessions (token,consumer_id,expires_at) VALUES ($1,$2,$3)`,
		token, c.ID, time.Now().Add(ttl)); err != nil {
		return domain.Consumer{}, "", err
	}
	return c, token, nil
}

// ConsumerBySession resolves a live (non-expired) session token to its consumer.
func (s *Store) ConsumerBySession(token string) (domain.Consumer, error) {
	var c domain.Consumer
	var profile []byte
	err := s.db.QueryRow(`SELECT c.id,c.name,c.phone,c.unit,c.project,c.avatar,c.created_at,
			c.blok,c.type,c.nik,c.email,c.cara_bayar,c.harga,c.plafon_kpr,c.nama_sales,c.screening_id,c.eligibility,c.profile
		FROM konsumen_sessions s JOIN konsumen_consumers c ON c.id=s.consumer_id
		WHERE s.token=$1 AND s.expires_at > now()`, token).
		Scan(&c.ID, &c.Name, &c.Phone, &c.Unit, &c.Project, &c.Avatar, &c.CreatedAt,
			&c.Blok, &c.Type, &c.NIK, &c.Email, &c.CaraBayar, &c.Harga, &c.PlafonKPR, &c.NamaSales,
			&c.ScreeningID, &c.Eligibility, &profile)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Consumer{}, ErrNotFound
	}
	c.Profile = scanProfile(profile)
	return c, err
}

// ConsumerByID fetches an activated consumer by id (staff/pemohon views).
func (s *Store) ConsumerByID(id string) (domain.Consumer, error) {
	var c domain.Consumer
	var profile []byte
	err := s.db.QueryRow(`SELECT id,name,phone,unit,project,avatar,created_at,
			blok,type,nik,email,cara_bayar,harga,plafon_kpr,nama_sales,screening_id,eligibility,profile
		FROM konsumen_consumers WHERE id=$1`, id).
		Scan(&c.ID, &c.Name, &c.Phone, &c.Unit, &c.Project, &c.Avatar, &c.CreatedAt,
			&c.Blok, &c.Type, &c.NIK, &c.Email, &c.CaraBayar, &c.Harga, &c.PlafonKPR, &c.NamaSales,
			&c.ScreeningID, &c.Eligibility, &profile)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Consumer{}, ErrNotFound
	}
	c.Profile = scanProfile(profile)
	return c, err
}

// Logout deletes a session token.
func (s *Store) Logout(token string) error {
	_, err := s.db.Exec(`DELETE FROM konsumen_sessions WHERE token=$1`, token)
	return err
}

// SetAvatar points a consumer's avatar at an uploaded attachment id.
func (s *Store) SetAvatar(consumerID, attachmentID string) error {
	_, err := s.db.Exec(`UPDATE konsumen_consumers SET avatar=$1 WHERE id=$2`, attachmentID, consumerID)
	return err
}

/* -------------------------------------------------------------------- posts */

// CreatePost inserts a routed report. Division is derived from the category by
// the caller (service layer) so the routing rule stays in one place.
func (s *Store) CreatePost(consumerID, category string, division domain.Division, caption string) (domain.Post, error) {
	p := domain.Post{
		ID: newID(), ConsumerID: consumerID, Category: category, Division: division,
		Caption: caption, Status: domain.StatusTerkirim, CreatedAt: time.Now(),
	}
	_, err := s.db.Exec(`INSERT INTO konsumen_posts (id,consumer_id,category,division,caption,status,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.ID, p.ConsumerID, p.Category, string(p.Division), p.Caption, string(p.Status), p.CreatedAt)
	if err != nil {
		return domain.Post{}, err
	}
	return p, nil
}

// AddAttachment records file metadata bound to a post.
func (s *Store) AddAttachment(postID, filename, contentType, kind string, size int64) (domain.Attachment, error) {
	a := domain.Attachment{
		ID: newID(), PostID: postID, Filename: filename, ContentType: contentType,
		Size: size, Kind: kind, CreatedAt: time.Now(),
	}
	_, err := s.db.Exec(`INSERT INTO konsumen_attachments (id,post_id,filename,content_type,size,kind,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		a.ID, a.PostID, a.Filename, a.ContentType, a.Size, a.Kind, a.CreatedAt)
	if err != nil {
		return domain.Attachment{}, err
	}
	return a, nil
}

// Attachment fetches one attachment's metadata (used to serve/authorise files).
func (s *Store) Attachment(id string) (domain.Attachment, error) {
	var a domain.Attachment
	var postID sql.NullString
	err := s.db.QueryRow(`SELECT id,post_id,filename,content_type,size,kind,created_at
		FROM konsumen_attachments WHERE id=$1`, id).
		Scan(&a.ID, &postID, &a.Filename, &a.ContentType, &a.Size, &a.Kind, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Attachment{}, ErrNotFound
	}
	if postID.Valid {
		a.PostID = postID.String
	}
	return a, err
}

// AddStandaloneAttachment records a file not (yet) bound to a post — e.g. an
// avatar image uploaded before/without a post.
func (s *Store) AddStandaloneAttachment(filename, contentType, kind string, size int64) (domain.Attachment, error) {
	a := domain.Attachment{
		ID: newID(), Filename: filename, ContentType: contentType,
		Size: size, Kind: kind, CreatedAt: time.Now(),
	}
	_, err := s.db.Exec(`INSERT INTO konsumen_attachments (id,post_id,filename,content_type,size,kind,created_at)
		VALUES ($1,NULL,$2,$3,$4,$5,$6)`,
		a.ID, a.Filename, a.ContentType, a.Size, a.Kind, a.CreatedAt)
	if err != nil {
		return domain.Attachment{}, err
	}
	return a, nil
}

// postFilter is an internal query builder shared by the feed queries.
type postFilter struct {
	consumerID string
	division   string
	viewerID   string // consumer id whose "liked" flag to compute (may be empty)
}

func (s *Store) queryPosts(f postFilter) ([]domain.Post, error) {
	where := []string{"1=1"}
	args := []any{}
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.consumerID != "" {
		add("p.consumer_id=$%d", f.consumerID)
	}
	if f.division != "" {
		add("p.division=$%d", f.division)
	}
	viewer := f.viewerID
	args = append(args, viewer)
	viewerIdx := len(args)

	q := fmt.Sprintf(`SELECT p.id,p.consumer_id,p.category,p.division,p.caption,p.status,
			p.handled_by,p.response,p.responded_at,p.created_at,
			c.name,c.unit,c.project,c.avatar,
			(SELECT count(*) FROM konsumen_likes l WHERE l.post_id=p.id) AS likes,
			EXISTS(SELECT 1 FROM konsumen_likes l WHERE l.post_id=p.id AND l.consumer_id=$%d) AS liked
		FROM konsumen_posts p JOIN konsumen_consumers c ON c.id=p.consumer_id
		WHERE %s ORDER BY p.created_at DESC`, viewerIdx, strings.Join(where, " AND "))

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []domain.Post
	index := map[string]int{}
	for rows.Next() {
		var p domain.Post
		var respondedAt sql.NullTime
		var cons domain.Consumer
		if err := rows.Scan(&p.ID, &p.ConsumerID, &p.Category, &p.Division, &p.Caption, &p.Status,
			&p.HandledBy, &p.Response, &respondedAt, &p.CreatedAt,
			&cons.Name, &cons.Unit, &cons.Project, &cons.Avatar,
			&p.Likes, &p.Liked); err != nil {
			return nil, err
		}
		if respondedAt.Valid {
			p.RespondedAt = &respondedAt.Time
		}
		cons.ID = p.ConsumerID
		p.Consumer = &cons
		p.Attachments = []domain.Attachment{}
		index[p.ID] = len(posts)
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(posts) == 0 {
		return posts, nil
	}
	// Hydrate attachments in one pass.
	arows, err := s.db.Query(`SELECT id,post_id,filename,content_type,size,kind,created_at
		FROM konsumen_attachments WHERE post_id = ANY($1) ORDER BY created_at ASC`, postIDs(posts))
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	for arows.Next() {
		var a domain.Attachment
		var postID sql.NullString
		if err := arows.Scan(&a.ID, &postID, &a.Filename, &a.ContentType, &a.Size, &a.Kind, &a.CreatedAt); err != nil {
			return nil, err
		}
		if postID.Valid {
			a.PostID = postID.String
			if i, ok := index[a.PostID]; ok {
				posts[i].Attachments = append(posts[i].Attachments, a)
			}
		}
	}
	return posts, arows.Err()
}

func postIDs(posts []domain.Post) []string {
	ids := make([]string, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	return ids
}

// FeedForConsumer returns a consumer's own posts (their private timeline).
func (s *Store) FeedForConsumer(consumerID string) ([]domain.Post, error) {
	return s.queryPosts(postFilter{consumerID: consumerID, viewerID: consumerID})
}

// InboxForDivision returns posts auto-routed to a division (staff view).
func (s *Store) InboxForDivision(division string) ([]domain.Post, error) {
	return s.queryPosts(postFilter{division: division})
}

// Post fetches a single post (with viewer's like flag) or ErrNotFound.
func (s *Store) Post(id, viewerID string) (domain.Post, error) {
	posts, err := s.queryPostsByID(id, viewerID)
	if err != nil {
		return domain.Post{}, err
	}
	if len(posts) == 0 {
		return domain.Post{}, ErrNotFound
	}
	return posts[0], nil
}

func (s *Store) queryPostsByID(id, viewerID string) ([]domain.Post, error) {
	rows, err := s.db.Query(`SELECT p.id,p.consumer_id,p.category,p.division,p.caption,p.status,
			p.handled_by,p.response,p.responded_at,p.created_at,
			c.name,c.unit,c.project,c.avatar,
			(SELECT count(*) FROM konsumen_likes l WHERE l.post_id=p.id) AS likes,
			EXISTS(SELECT 1 FROM konsumen_likes l WHERE l.post_id=p.id AND l.consumer_id=$2) AS liked
		FROM konsumen_posts p JOIN konsumen_consumers c ON c.id=p.consumer_id
		WHERE p.id=$1`, id, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []domain.Post
	for rows.Next() {
		var p domain.Post
		var respondedAt sql.NullTime
		var cons domain.Consumer
		if err := rows.Scan(&p.ID, &p.ConsumerID, &p.Category, &p.Division, &p.Caption, &p.Status,
			&p.HandledBy, &p.Response, &respondedAt, &p.CreatedAt,
			&cons.Name, &cons.Unit, &cons.Project, &cons.Avatar, &p.Likes, &p.Liked); err != nil {
			return nil, err
		}
		if respondedAt.Valid {
			p.RespondedAt = &respondedAt.Time
		}
		cons.ID = p.ConsumerID
		p.Consumer = &cons
		p.Attachments = []domain.Attachment{}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(posts) == 1 {
		arows, err := s.db.Query(`SELECT id,post_id,filename,content_type,size,kind,created_at
			FROM konsumen_attachments WHERE post_id=$1 ORDER BY created_at ASC`, id)
		if err != nil {
			return nil, err
		}
		defer arows.Close()
		for arows.Next() {
			var a domain.Attachment
			var postID sql.NullString
			if err := arows.Scan(&a.ID, &postID, &a.Filename, &a.ContentType, &a.Size, &a.Kind, &a.CreatedAt); err != nil {
				return nil, err
			}
			if postID.Valid {
				a.PostID = postID.String
			}
			posts[0].Attachments = append(posts[0].Attachments, a)
		}
		if err := arows.Err(); err != nil {
			return nil, err
		}
	}
	return posts, nil
}

// Respond lets a division staffer set status + a response note on a post.
func (s *Store) Respond(postID, division, handledBy, response string, status domain.PostStatus) (domain.Post, error) {
	res, err := s.db.Exec(`UPDATE konsumen_posts
		SET status=$1, handled_by=$2, response=$3, responded_at=now()
		WHERE id=$4 AND division=$5`,
		string(status), handledBy, response, postID, division)
	if err != nil {
		return domain.Post{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.Post{}, ErrNotFound
	}
	return s.Post(postID, "")
}

// ToggleLike flips a consumer's like on a post and returns the new like count.
func (s *Store) ToggleLike(postID, consumerID string) (liked bool, count int, err error) {
	var exists bool
	if err = s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM konsumen_likes WHERE post_id=$1 AND consumer_id=$2)`,
		postID, consumerID).Scan(&exists); err != nil {
		return false, 0, err
	}
	if exists {
		_, err = s.db.Exec(`DELETE FROM konsumen_likes WHERE post_id=$1 AND consumer_id=$2`, postID, consumerID)
		liked = false
	} else {
		_, err = s.db.Exec(`INSERT INTO konsumen_likes (post_id,consumer_id) VALUES ($1,$2)
			ON CONFLICT DO NOTHING`, postID, consumerID)
		liked = true
	}
	if err != nil {
		return false, 0, err
	}
	if err = s.db.QueryRow(`SELECT count(*) FROM konsumen_likes WHERE post_id=$1`, postID).Scan(&count); err != nil {
		return false, 0, err
	}
	return liked, count, nil
}

/* -------------------------------------------------------------------- stats */

/* ---------------------------------------------------------------- pemohon */

// ListConsumers returns every activated pemohon (newest first) — the Sales panel
// roster.
func (s *Store) ListConsumers() ([]domain.Consumer, error) {
	rows, err := s.db.Query(`SELECT id,name,phone,unit,project,avatar,created_at,
			blok,type,nik,email,cara_bayar,harga,plafon_kpr,nama_sales,screening_id,eligibility,profile
		FROM konsumen_consumers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Consumer
	for rows.Next() {
		var c domain.Consumer
		var profile []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone, &c.Unit, &c.Project, &c.Avatar, &c.CreatedAt,
			&c.Blok, &c.Type, &c.NIK, &c.Email, &c.CaraBayar, &c.Harga, &c.PlafonKPR, &c.NamaSales,
			&c.ScreeningID, &c.Eligibility, &profile); err != nil {
			return nil, err
		}
		c.Profile = scanProfile(profile)
		out = append(out, c)
	}
	return out, rows.Err()
}

// DocPresenceByConsumer maps consumerID -> set of doc types with a non-rejected
// upload, so the Sales roster can show berkas completeness in one query.
func (s *Store) DocPresenceByConsumer() (map[string]map[string]bool, error) {
	rows, err := s.db.Query(`SELECT DISTINCT consumer_id, doc_type FROM konsumen_documents WHERE status <> 'rejected'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var cid, dt string
		if err := rows.Scan(&cid, &dt); err != nil {
			return nil, err
		}
		if out[cid] == nil {
			out[cid] = map[string]bool{}
		}
		out[cid][dt] = true
	}
	return out, rows.Err()
}

/* -------------------------------------------------------------- documents */

func scanDocument(sc interface{ Scan(...any) error }) (domain.Document, error) {
	var d domain.Document
	var verifiedAt sql.NullTime
	if err := sc.Scan(&d.ID, &d.ConsumerID, &d.DocType, &d.Filename, &d.ContentType, &d.Size,
		&d.Status, &d.Note, &d.UploadedBy, &d.VerifiedBy, &verifiedAt, &d.CreatedAt); err != nil {
		return domain.Document{}, err
	}
	if verifiedAt.Valid {
		d.VerifiedAt = &verifiedAt.Time
	}
	if dt, ok := domain.DocumentTypeByKey(d.DocType); ok {
		d.Label = dt.Label
	}
	return d, nil
}

const docCols = `id,consumer_id,doc_type,filename,content_type,size,status,note,uploaded_by,verified_by,verified_at,created_at`

// AddDocument stores a berkas record (bytes are written to disk by the caller
// using the returned id).
func (s *Store) AddDocument(consumerID, docType, filename, contentType string, size int64, uploadedBy string) (domain.Document, error) {
	d := domain.Document{
		ID: newID(), ConsumerID: consumerID, DocType: docType, Filename: filename,
		ContentType: contentType, Size: size, Status: domain.DocPending,
		UploadedBy: uploadedBy, CreatedAt: time.Now(),
	}
	_, err := s.db.Exec(`INSERT INTO konsumen_documents (id,consumer_id,doc_type,filename,content_type,size,status,uploaded_by,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		d.ID, d.ConsumerID, d.DocType, d.Filename, d.ContentType, d.Size, string(d.Status), d.UploadedBy, d.CreatedAt)
	if err != nil {
		return domain.Document{}, err
	}
	if dt, ok := domain.DocumentTypeByKey(docType); ok {
		d.Label = dt.Label
	}
	return d, nil
}

// ListDocuments returns a pemohon's uploaded berkas (newest first).
func (s *Store) ListDocuments(consumerID string) ([]domain.Document, error) {
	rows, err := s.db.Query(`SELECT `+docCols+` FROM konsumen_documents WHERE consumer_id=$1 ORDER BY created_at DESC`, consumerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Document{} // never nil — the UI iterates this without a guard
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Document fetches one berkas (to serve/authorise the file).
func (s *Store) Document(id string) (domain.Document, error) {
	d, err := scanDocument(s.db.QueryRow(`SELECT `+docCols+` FROM konsumen_documents WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Document{}, ErrNotFound
	}
	return d, err
}

// UpdateDocumentStatus lets Sales/Legal verify or reject a berkas.
func (s *Store) UpdateDocumentStatus(id string, status domain.DocStatus, note, verifiedBy string) (domain.Document, error) {
	res, err := s.db.Exec(`UPDATE konsumen_documents SET status=$1, note=$2, verified_by=$3, verified_at=now() WHERE id=$4`,
		string(status), note, verifiedBy, id)
	if err != nil {
		return domain.Document{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.Document{}, ErrNotFound
	}
	return s.Document(id)
}

// DivisionCounts returns per-status counts for a division inbox (staff badges).
func (s *Store) DivisionCounts(division string) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, count(*) FROM konsumen_posts WHERE division=$1 GROUP BY status`, division)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}
