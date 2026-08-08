package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"strings"
	"time"
)

// RFC 6238 TOTP (SHA-1, 6 digits, 30s step) + RFC 4648 base32 — pure stdlib, no
// external dependency. Compatible with Google Authenticator / 1Password / etc.

const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

func base32Encode(b []byte) string {
	var sb strings.Builder
	var buf uint64
	var bits int
	for _, x := range b {
		buf = (buf << 8) | uint64(x)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(base32Alphabet[(buf>>uint(bits))&0x1f])
		}
	}
	if bits > 0 {
		sb.WriteByte(base32Alphabet[(buf<<uint(5-bits))&0x1f])
	}
	return sb.String()
}

func base32Decode(s string) []byte {
	s = strings.ToUpper(strings.TrimRight(strings.ReplaceAll(s, " ", ""), "="))
	var out []byte
	var buf uint64
	var bits int
	for _, c := range s {
		idx := strings.IndexRune(base32Alphabet, c)
		if idx < 0 {
			continue
		}
		buf = (buf << 5) | uint64(idx)
		bits += 5
		if bits >= 8 {
			bits -= 8
			out = append(out, byte((buf>>uint(bits))&0xff))
		}
	}
	return out
}

// newTOTPSecret returns a fresh 20-byte secret, base32-encoded.
func newTOTPSecret() string {
	b := make([]byte, 20)
	rand.Read(b)
	return base32Encode(b)
}

func totpAt(secret string, counter uint64) string {
	key := base32Decode(secret)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	h := hmac.New(sha1.New, key)
	h.Write(msg[:])
	sum := h.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])
	code %= 1_000_000
	out := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		out[i] = byte('0' + code%10)
		code /= 10
	}
	return string(out)
}

// verifyTOTP accepts a 6-digit code, allowing +/-1 step for clock skew.
func verifyTOTP(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 || secret == "" {
		return false
	}
	step := uint64(time.Now().Unix() / 30)
	for _, c := range []uint64{step - 1, step, step + 1} {
		if hmac.Equal([]byte(totpAt(secret, c)), []byte(code)) {
			return true
		}
	}
	return false
}

// otpauthURL builds the provisioning URI for a QR code / manual entry.
func otpauthURL(secret, account, issuer string) string {
	return "otpauth://totp/" + urlEsc(issuer) + ":" + urlEsc(account) +
		"?secret=" + secret + "&issuer=" + urlEsc(issuer) + "&digits=6&period=30"
}

func urlEsc(s string) string {
	r := strings.NewReplacer(" ", "%20", ":", "%3A", "/", "%2F", "?", "%3F", "&", "%26", "=", "%3D")
	return r.Replace(s)
}
