// Package pwhash holds the password hashes server JavaScript reaches through
// crypto.subtle (R-JS-39d): bcrypt with a caller-given salt, which
// golang.org/x/crypto/bcrypt does not offer, and checking a stored bcrypt
// string.
package pwhash

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"

	"golang.org/x/crypto/blowfish"
)

// bcrypt's own base64: its alphabet, no padding.
var bcryptEnc = base64.NewEncoding("./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789").WithPadding(base64.NoPadding)

// ErrMalformed reports a stored string that is not a bcrypt hash this
// package checks: not $2a$, $2b$ or $2y$ (the legacy $2x$ included), a cost
// outside 4 to 31, or a wrong length.
var ErrMalformed = errors.New("the stored bcrypt string is malformed")

// Bcrypt returns the 60 bytes of the $2b$ string for password with the
// given 16-byte salt and a cost from 4 to 31. Only the first 72 bytes of
// the password count.
func Bcrypt(password, salt []byte, cost int) ([]byte, error) {
	if len(salt) != 16 || cost < 4 || cost > 31 {
		return nil, errors.New("bcrypt takes a 16-byte salt and a cost from 4 to 31")
	}
	out := make([]byte, 0, 60)
	out = append(out, "$2b$"...)
	if cost < 10 {
		out = append(out, '0')
	}
	out = strconv.AppendInt(out, int64(cost), 10)
	out = append(out, '$')
	out = bcryptEnc.AppendEncode(out, salt)
	return bcryptEnc.AppendEncode(out, bcryptRaw(password, salt, cost)), nil
}

// bcryptRaw is the 23 hash bytes bcrypt keeps.
func bcryptRaw(password, salt []byte, cost int) []byte {
	if len(password) > 72 {
		password = password[:72]
	}
	key := append(append(make([]byte, 0, len(password)+1), password...), 0)
	c, err := blowfish.NewSaltedCipher(key, salt)
	if err != nil {
		panic(err) // the key is never empty
	}
	for i := uint64(0); i < 1<<uint(cost); i++ {
		blowfish.ExpandKey(key, c)
		blowfish.ExpandKey(salt, c)
	}
	data := []byte("OrpheanBeholderScryDoubt")
	for i := 0; i < 24; i += 8 {
		for j := 0; j < 64; j++ {
			c.Encrypt(data[i:i+8], data[i:i+8])
		}
	}
	return data[:23]
}

// BcryptCost reads the cost of a stored $2a$, $2b$ or $2y$ string.
func BcryptCost(stored []byte) (int, error) {
	if len(stored) != 60 || stored[0] != '$' || stored[1] != '2' || stored[3] != '$' || stored[6] != '$' {
		return 0, ErrMalformed
	}
	if v := stored[2]; v != 'a' && v != 'b' && v != 'y' {
		return 0, ErrMalformed
	}
	cost, err := strconv.Atoi(string(stored[4:6]))
	if err != nil || cost < 4 || cost > 31 {
		return 0, ErrMalformed
	}
	return cost, nil
}

// BcryptCheck reports whether password matches a stored $2a$, $2b$ or $2y$
// string, comparing in constant time.
func BcryptCheck(password, stored []byte) (bool, error) {
	cost, err := BcryptCost(stored)
	if err != nil {
		return false, err
	}
	salt, err := bcryptEnc.DecodeString(string(stored[7:29]))
	if err != nil || len(salt) != 16 {
		return false, ErrMalformed
	}
	want, err := bcryptEnc.DecodeString(string(stored[29:]))
	if err != nil || len(want) != 23 {
		return false, ErrMalformed
	}
	return subtle.ConstantTimeCompare(bcryptRaw(password, salt, cost), want) == 1, nil
}
