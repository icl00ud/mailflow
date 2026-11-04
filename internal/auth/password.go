// Package auth holds the primitives the panel needs to identify people:
// argon2id password hashes, opaque session/invite tokens and a login rate limiter.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters. Memory is the expensive part (64 MiB); a login costs
// ~50 ms on the server, which is what makes offline guessing expensive too.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

var ErrBadHash = errors.New("auth: unrecognised password hash")

// HashPassword returns a PHC-formatted argon2id hash:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<b64 salt>$<b64 key>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches hash. Unknown or malformed
// hashes return false, never an error, so callers cannot accidentally treat a
// corrupt row as a valid credential.
func VerifyPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, key
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory uint32
	var timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	// Guard against a hostile/corrupt hash making one request allocate gigabytes.
	if memory > 1<<20 || timeCost > 10 || threads == 0 || threads > 64 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// DummyHash is verified against when the email is unknown, so a login attempt
// for a missing user costs the same time as one for an existing user.
var DummyHash = func() string {
	h, err := HashPassword("mailflow-dummy-password-" + strconv.Itoa(argonSaltLen))
	if err != nil {
		panic(err)
	}
	return h
}()

// NewToken returns a URL-safe random token with 256 bits of entropy.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken is what gets stored: tokens themselves never touch the database.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
