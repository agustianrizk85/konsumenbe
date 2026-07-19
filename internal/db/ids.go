package db

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// newID returns a random 16-byte hex id used for consumers, posts, attachments.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newToken returns a long, opaque 32-byte session token.
func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// crockford avoids ambiguous chars (no I,L,O,U,0,1) for human-typed invite codes.
const crockford = "23456789ABCDEFGHJKMNPQRSTVWXYZ"

// newInviteCode returns a friendly code like "GP-7QF2KD".
func newInviteCode() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	var sb strings.Builder
	sb.WriteString("GP-")
	for _, c := range b {
		sb.WriteByte(crockford[int(c)%len(crockford)])
	}
	return sb.String()
}

// isUniqueViolation reports whether err is a Postgres unique-constraint error
// (SQLSTATE 23505), matched by text so we don't import the pgconn error type.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key")
}

// hashPassword returns a salted SHA-256 hash. Note: for an internal MVP portal;
// swap for bcrypt/argon2 before exposing to the public internet at scale.
func hashPassword(pw, salt string) string {
	h := sha256.Sum256([]byte(salt + "\x00" + pw))
	return hex.EncodeToString(h[:])
}

// samePassword is a constant-time compare of a candidate against a stored hash.
func samePassword(pw, salt, stored string) bool {
	got := hashPassword(pw, salt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(stored)) == 1
}
