package jsrt

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"strings"
	"testing"
)

func wcName(err error) string {
	if err == nil {
		return ""
	}
	n, _, _ := strings.Cut(err.Error(), "|")
	return n
}

// Malformed public keys are DataError; unsupported shapes NotSupportedError
// (R-JS-39c, docs snapshot 2026-11-23 §Error cases).
func TestParsePublicKeyRefusals(t *testing.T) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	spki, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	raw := elliptic.Marshal(elliptic.P256(), k.X, k.Y) //nolint:staticcheck
	offCurve := append([]byte(nil), raw...)
	offCurve[len(offCurve)-1] ^= 1
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	smallSPKI, _ := x509.MarshalPKIXPublicKey(&small.PublicKey)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	edSPKI, _ := x509.MarshalPKIXPublicKey(pub)
	badPoint := make([]byte, 32)
	badPoint[0] = 2 // y = 2 is not on the curve
	for _, c := range []struct {
		name, alg, curve, format string
		data                     []byte
		want                     string
	}{
		{"good spki", "ECDSA", "P-256", "spki", spki, ""},
		{"good raw", "ECDSA", "P-256", "raw", raw, ""},
		{"trailing bytes", "ECDSA", "P-256", "spki", append(append([]byte(nil), spki...), 0), "DataError"},
		{"not DER", "ECDSA", "P-256", "spki", []byte{1, 2, 3}, "DataError"},
		{"another curve", "ECDSA", "P-384", "spki", spki, "DataError"},
		{"another algorithm", "ECDSA", "P-256", "spki", edSPKI, "DataError"},
		{"compressed point", "ECDSA", "P-256", "raw", elliptic.MarshalCompressed(elliptic.P256(), k.X, k.Y), "DataError"},
		{"point off the curve", "ECDSA", "P-256", "raw", offCurve, "DataError"},
		{"unknown curve", "ECDSA", "P-521", "spki", spki, "NotSupportedError"},
		{"small RSA modulus", "RSA-PSS", "", "spki", smallSPKI, "DataError"},
		{"raw RSA", "RSASSA-PKCS1-v1_5", "", "raw", smallSPKI, "NotSupportedError"},
		{"good Ed25519", "Ed25519", "", "raw", pub, ""},
		{"Ed25519 length", "Ed25519", "", "raw", pub[:31], "DataError"},
		{"Ed25519 not a point", "Ed25519", "", "raw", badPoint, "DataError"},
	} {
		_, err := parsePublicKey(c.alg, c.curve, c.format, c.data)
		if got := wcName(err); got != c.want {
			t.Errorf("%s: %q, want %q (%v)", c.name, got, c.want, err)
		}
	}
}

// A signature that does not match or is in the wrong form verifies false.
func TestVerifySignature(t *testing.T) {
	msg := []byte("m")
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	spki, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	h := sha256.Sum256(msg)
	r, s, _ := ecdsa.Sign(rand.Reader, k, h[:])
	rs := make([]byte, 64)
	r.FillBytes(rs[:32])
	s.FillBytes(rs[32:])
	der, _ := ecdsa.SignASN1(rand.Reader, k, h[:])
	check := func(name string, sig, data []byte, want bool) {
		t.Helper()
		got, err := verifySignature("ECDSA", "P-256", "SHA-256", "spki", spki, sig, data)
		if err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", name, got, err, want)
		}
	}
	check("r and s", rs, msg, true)
	check("other data", rs, []byte("n"), false)
	check("ASN.1 DER", der, msg, false)
	check("empty", nil, msg, false)
}
