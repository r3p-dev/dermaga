package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"runtime/debug"
	"strings"

	"golang.org/x/crypto/argon2"
)

// OWASP's argon2id profile m=19 MiB, t=2, p=1: the daemon's idle RSS budget is
// 30 MB, and login backoff limits online guessing. Parameters are stored in
// the hash, so they can be raised later without breaking existing hashes.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
)

var b64 = base64.RawStdEncoding

// releaseArgonMemory hands argon2's 19 MiB work area back to the OS at once.
// Without it the idle daemon stayed at 57 MB RSS after a login (measured in M1).
func releaseArgonMemory() { debug.FreeOSMemory() }

// HashPassword returns a PHC-format argon2id hash.
func HashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	releaseArgonMemory()
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

func VerifyPassword(encoded, pw string) bool {
	p := strings.Split(encoded, "$")
	if len(p) != 6 || p[1] != "argon2id" || p[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return false
	}
	var m, t uint32
	var threads uint8
	if _, err := fmt.Sscanf(p[3], "m=%d,t=%d,p=%d", &m, &t, &threads); err != nil {
		return false
	}
	salt, err1 := b64.DecodeString(p[4])
	want, err2 := b64.DecodeString(p[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, threads, uint32(len(want)))
	releaseArgonMemory()
	return subtle.ConstantTimeCompare(got, want) == 1
}
