package pwhash

import (
	"bytes"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// A hash made here checks with golang.org/x/crypto/bcrypt, and one made
// there checks here.
func TestBcryptAgreesWithXCrypto(t *testing.T) {
	salt := []byte("0123456789abcdef")
	for _, pw := range []string{"", "secret", string(bytes.Repeat([]byte("x"), 72))} {
		h, err := Bcrypt([]byte(pw), salt, 4)
		if err != nil || len(h) != 60 {
			t.Fatalf("%q: %v %d", pw, err, len(h))
		}
		if err := bcrypt.CompareHashAndPassword(h, []byte(pw)); err != nil {
			t.Errorf("%q: x/crypto refuses %s: %v", pw, h, err)
		}
		theirs, _ := bcrypt.GenerateFromPassword([]byte(pw), 4)
		if ok, err := BcryptCheck([]byte(pw), theirs); !ok || err != nil {
			t.Errorf("%q: %v %v", pw, ok, err)
		}
		if ok, _ := BcryptCheck([]byte(pw+"x"), theirs); ok && len(pw) < 72 {
			t.Errorf("%q: another password matched", pw)
		}
	}
}

func TestBcryptLongPasswordsAndVersions(t *testing.T) {
	salt := []byte("0123456789abcdef")
	long := bytes.Repeat([]byte("y"), 100)
	a, _ := Bcrypt(long, salt, 4)
	b, _ := Bcrypt(long[:72], salt, 4)
	if !bytes.Equal(a, b) {
		t.Error("bytes past the first 72 counted")
	}
	for v, want := range map[byte]bool{'a': true, 'b': true, 'y': true, 'x': false} {
		s := append([]byte(nil), a...)
		s[2] = v
		ok, err := BcryptCheck(long, s)
		if want && (!ok || err != nil) || !want && err != ErrMalformed {
			t.Errorf("$2%c$: %v %v", v, ok, err)
		}
	}
	for _, s := range []string{"", "$2b$12$short", string(a[:59]), "$2b$03$" + string(a[7:]), "$2b$32$" + string(a[7:])} {
		if _, err := BcryptCheck(long, []byte(s)); err != ErrMalformed {
			t.Errorf("%q: %v", s, err)
		}
	}
}
