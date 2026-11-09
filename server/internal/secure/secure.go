// Package secure: password hashing (argon2id), random tokens and per-organisation encryption.
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
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
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/hkdf"
)

// Token returns n random bytes as hex.
func Token(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func SHA256(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func Equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// ---- passwords: argon2id (OWASP: m=19MiB, t=2, p=1)
const (
	aTime    = 2
	aMem     = 19 * 1024
	aThreads = 1
	aLen     = 32
)

func HashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	h := argon2.IDKey([]byte(pw), salt, aTime, aMem, aThreads, aLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", aMem, aTime, aThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(h))
}

// NeedsRehash reports a legacy (v1 bcrypt) hash that should be upgraded after a successful login.
func NeedsRehash(hash string) bool { return strings.HasPrefix(hash, "$2") }

func CheckPassword(hash, pw string) bool {
	if NeedsRehash(hash) {
		return bcrypt.CompareHashAndPassword([]byte(strings.Replace(hash, "$2y$", "$2a$", 1)), []byte(pw)) == nil
	}
	p := strings.Split(hash, "$")
	if len(p) != 6 || p[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var th uint8
	if _, err := fmt.Sscanf(p[3], "m=%d,t=%d,p=%d", &m, &t, &th); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(p[4])
	want, err2 := base64.RawStdEncoding.DecodeString(p[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, th, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---- encryption of stored secrets with a key derived per organisation (HKDF from the master key)
type Box struct{ master []byte }

func NewBox(master []byte) *Box { return &Box{master: master} }

func (b *Box) key(orgID int64) []byte {
	k := make([]byte, 32)
	r := hkdf.New(sha256.New, b.master, nil, []byte("setu-org-"+strconv.FormatInt(orgID, 10)))
	r.Read(k)
	return k
}

const encPrefix = "enc2:"

func (b *Box) Encrypt(orgID int64, plain string) string {
	if plain == "" {
		return ""
	}
	blk, _ := aes.NewCipher(b.key(orgID))
	g, _ := cipher.NewGCM(blk)
	nonce := make([]byte, g.NonceSize())
	rand.Read(nonce)
	return encPrefix + base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), nil))
}

func (b *Box) Decrypt(orgID int64, v string) (string, error) {
	if !strings.HasPrefix(v, encPrefix) {
		return v, nil
	}
	raw, err := base64.StdEncoding.DecodeString(v[len(encPrefix):])
	if err != nil {
		return "", err
	}
	blk, _ := aes.NewCipher(b.key(orgID))
	g, _ := cipher.NewGCM(blk)
	if len(raw) < g.NonceSize() {
		return "", errors.New("secure: short ciphertext")
	}
	out, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
	return string(out), err
}

// Sign returns an HMAC for values that must not be forged (e.g. signed URLs).
func (b *Box) Sign(msg string) string {
	m := hmac.New(sha256.New, b.master)
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))[:32]
}
