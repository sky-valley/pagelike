package jsrt

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"errors"
	"math/big"
)

// WebCrypto public keys and signature checks (R-JS-39c; docs snapshot
// 2026-11-23, "Checking signatures with WebCrypto"). The prelude reads and
// checks the arguments; these functions parse the key and verify. A key is
// kept by the script's CryptoKey as the bytes it was imported from, so the
// functions are stateless.

// wcError is a failure the prelude turns into a DOMException named name.
func wcError(name, msg string) error { return errors.New(name + "|" + msg) }

func dataError(msg string) error { return wcError("DataError", msg) }

type wcPublicKey struct {
	ec  *ecdsa.PublicKey
	rsa *rsa.PublicKey
	ed  ed25519.PublicKey
}

// parsePublicKey reads a public key of algorithm alg (ECDSA,
// RSASSA-PKCS1-v1_5, RSA-PSS or Ed25519) in format spki or raw; curve is
// the ECDSA namedCurve.
func parsePublicKey(alg, curve, format string, data []byte) (*wcPublicKey, error) {
	switch alg {
	case "ECDSA":
		var c elliptic.Curve
		switch curve {
		case "P-256":
			c = elliptic.P256()
		case "P-384":
			c = elliptic.P384()
		default:
			return nil, wcError("NotSupportedError", "only the curves P-256 and P-384 are supported")
		}
		if format == "raw" {
			n := (c.Params().BitSize + 7) / 8
			if len(data) != 1+2*n || data[0] != 0x04 {
				return nil, dataError("a raw ECDSA key is an uncompressed point")
			}
			x, y := new(big.Int).SetBytes(data[1:1+n]), new(big.Int).SetBytes(data[1+n:])
			if !c.IsOnCurve(x, y) { //nolint:staticcheck // the point check WebCrypto asks for
				return nil, dataError("the point is not on the curve")
			}
			return &wcPublicKey{ec: &ecdsa.PublicKey{Curve: c, X: x, Y: y}}, nil
		}
		k, err := x509.ParsePKIXPublicKey(data)
		if err != nil {
			return nil, dataError("the key is not a valid SubjectPublicKeyInfo")
		}
		pk, ok := k.(*ecdsa.PublicKey)
		if !ok || pk.Curve != c {
			return nil, dataError("the key is for another algorithm or curve")
		}
		return &wcPublicKey{ec: pk}, nil
	case "RSASSA-PKCS1-v1_5", "RSA-PSS":
		if format != "spki" {
			return nil, wcError("NotSupportedError", "an RSA public key is imported as spki")
		}
		k, err := x509.ParsePKIXPublicKey(data)
		if err != nil {
			return nil, dataError("the key is not a valid SubjectPublicKeyInfo")
		}
		pk, ok := k.(*rsa.PublicKey)
		if !ok {
			return nil, dataError("the key is for another algorithm")
		}
		if bits := pk.N.BitLen(); bits < 2048 || bits > 8192 {
			return nil, dataError("the RSA modulus must be 2048 to 8192 bits")
		}
		if pk.N.Bit(0) == 0 || pk.E < 3 || pk.E%2 == 0 {
			return nil, dataError("the RSA modulus or exponent is not valid")
		}
		return &wcPublicKey{rsa: pk}, nil
	case "Ed25519":
		raw := data
		if format != "raw" {
			k, err := x509.ParsePKIXPublicKey(data)
			if err != nil {
				return nil, dataError("the key is not a valid SubjectPublicKeyInfo")
			}
			pk, ok := k.(ed25519.PublicKey)
			if !ok {
				return nil, dataError("the key is for another algorithm")
			}
			raw = pk
		}
		if len(raw) != ed25519.PublicKeySize || !ed25519Point(raw) {
			return nil, dataError("the key is not a valid Ed25519 point")
		}
		return &wcPublicKey{ed: ed25519.PublicKey(append([]byte(nil), raw...))}, nil
	}
	return nil, wcError("NotSupportedError", "Unrecognized algorithm name")
}

var (
	ed25519P = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	ed25519D = func() *big.Int {
		// d = -121665/121666 mod p
		d := new(big.Int).ModInverse(big.NewInt(121666), ed25519P)
		d.Mul(d, big.NewInt(-121665))
		return d.Mod(d, ed25519P)
	}()
)

// ed25519Point reports whether the 32 bytes encode a point of the curve:
// y below the field prime, and (y²-1)/(dy²+1) a square.
func ed25519Point(b []byte) bool {
	le := make([]byte, 32)
	for i := range b {
		le[31-i] = b[i]
	}
	le[0] &= 0x7f
	y := new(big.Int).SetBytes(le)
	if y.Cmp(ed25519P) >= 0 {
		return false
	}
	y2 := new(big.Int).Mul(y, y)
	u := new(big.Int).Sub(y2, big.NewInt(1))
	u.Mod(u, ed25519P)
	v := new(big.Int).Mul(ed25519D, y2)
	v.Add(v, big.NewInt(1)).Mod(v, ed25519P)
	x2 := new(big.Int).Mul(u, new(big.Int).ModInverse(v, ed25519P))
	x2.Mod(x2, ed25519P)
	if x2.Sign() == 0 {
		return b[31]&0x80 == 0 // x = 0 has no negative
	}
	return new(big.Int).ModSqrt(x2, ed25519P) != nil
}

func wcHash(name string) (crypto.Hash, bool) {
	switch name {
	case "SHA-256":
		return crypto.SHA256, true
	case "SHA-384":
		return crypto.SHA384, true
	case "SHA-512":
		return crypto.SHA512, true
	}
	return 0, false
}

func wcDigest(h crypto.Hash, data []byte) []byte {
	switch h {
	case crypto.SHA384:
		s := sha512.Sum384(data)
		return s[:]
	case crypto.SHA512:
		s := sha512.Sum512(data)
		return s[:]
	}
	s := sha256.Sum256(data)
	return s[:]
}

// verifySignature checks sig over data. A signature that does not match,
// or is in the wrong form (an ASN.1 DER ECDSA signature, a wrong length),
// is false, not an error.
func verifySignature(alg, curve, hash, format string, key, sig, data []byte) (bool, error) {
	pk, err := parsePublicKey(alg, curve, format, key)
	if err != nil {
		return false, err
	}
	switch {
	case pk.ec != nil:
		h := crypto.SHA256
		if curve == "P-384" {
			h = crypto.SHA384
		}
		n := (pk.ec.Curve.Params().BitSize + 7) / 8
		if len(sig) != 2*n {
			return false, nil
		}
		r, s := new(big.Int).SetBytes(sig[:n]), new(big.Int).SetBytes(sig[n:])
		return ecdsa.Verify(pk.ec, wcDigest(h, data), r, s), nil
	case pk.rsa != nil:
		h, ok := wcHash(hash)
		if !ok {
			return false, wcError("NotSupportedError", "RSA signatures use SHA-256, SHA-384 or SHA-512")
		}
		d := wcDigest(h, data)
		if alg == "RSA-PSS" {
			return rsa.VerifyPSS(pk.rsa, h, d, sig, &rsa.PSSOptions{SaltLength: h.Size(), Hash: h}) == nil, nil
		}
		return rsa.VerifyPKCS1v15(pk.rsa, h, d, sig) == nil, nil
	case pk.ed != nil:
		return len(sig) == ed25519.SignatureSize && ed25519.Verify(pk.ed, data, sig), nil
	}
	return false, nil
}
