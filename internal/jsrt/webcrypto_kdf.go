package jsrt

import (
	"context"
	"crypto/hkdf"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/argon2x"
	"github.com/sky-valley/pagelike/internal/hashgate"
	"github.com/sky-valley/pagelike/internal/pwhash"
)

// Key derivation and password hashing of crypto.subtle (R-JS-39d; docs
// snapshot 2026-11-23, "Hashing passwords with Argon2 and bcrypt"). PBKDF2
// and HKDF run in the worker. Argon2 and bcrypt run in the host process,
// where the hashing gate shared with Sessel and Liquid limits how many run
// at once.

func operationError(msg string) error { return wcError("OperationError", msg) }

func kdfHash(name string) (func() hash.Hash, int, bool) {
	switch name {
	case "SHA-1":
		return sha1.New, sha1.Size, true
	case "SHA-256":
		return sha256.New, sha256.Size, true
	case "SHA-384":
		return sha512.New384, sha512.Size384, true
	case "SHA-512":
		return sha512.New, sha512.Size, true
	}
	return nil, 0, false
}

// maxDerived is the most bytes one derivation may produce (the docs' 16 MiB
// for a call without a memory budget).
const maxDerived = 16 << 20

// derive runs PBKDF2 or HKDF and returns the bytes in hex. rem is the time
// the evaluation has left: PBKDF2 cannot be stopped part-way, so work
// estimated to outlast it is refused.
func derive(alg, hashName string, key, salt, info []byte, iterations int64, length int, rem time.Duration) (string, error) {
	h, size, ok := kdfHash(hashName)
	if !ok {
		return "", wcError("NotSupportedError", alg+" uses SHA-1, SHA-256, SHA-384 or SHA-512")
	}
	if length > maxDerived {
		return "", operationError("the derived bytes do not fit the memory budget")
	}
	switch alg {
	case "PBKDF2":
		if iterations == 0 {
			return "", operationError("PBKDF2 needs at least one iteration")
		}
		blocks := int64((length + size - 1) / size)
		// About a microsecond for each iteration of each output block.
		if est := time.Duration(iterations*max(blocks, 1)) * time.Microsecond; est > rem || est < 0 {
			return "", operationError("the work would exceed the time budget; lower the iterations")
		}
		out, err := pbkdf2.Key(h, string(key), salt, int(iterations), length)
		if err != nil {
			return "", operationError(err.Error())
		}
		return hex.EncodeToString(out), nil
	case "HKDF":
		if length > 255*size {
			return "", operationError("HKDF gives at most 255 hash blocks of output")
		}
		out, err := hkdf.Key(h, key, salt, string(info), length)
		if err != nil {
			return "", operationError(err.Error())
		}
		return hex.EncodeToString(out), nil
	}
	return "", wcError("NotSupportedError", "Unrecognized algorithm name")
}

// pwRequest is one Argon2 or bcrypt job, sent from the worker to the host.
type pwRequest struct {
	Alg      string `json:"alg"` // Argon2id, Argon2i, Argon2d, bcrypt, bcrypt-verify
	Password []byte `json:"password"`
	Salt     []byte `json:"salt,omitempty"`
	Secret   []byte `json:"secret,omitempty"`
	Data     []byte `json:"data,omitempty"`
	Stored   []byte `json:"stored,omitempty"` // bcrypt-verify
	Passes   uint32 `json:"passes,omitempty"`
	Memory   uint32 `json:"memory,omitempty"` // KiB
	Lanes    uint8  `json:"lanes,omitempty"`
	Length   uint32 `json:"length,omitempty"` // bytes
	Cost     int    `json:"cost,omitempty"`
	// Deadline is when the evaluation's time runs out (Unix nanoseconds).
	Deadline int64 `json:"deadline,omitempty"`
}

// estimate is how long the job takes; it also checks a stored bcrypt string.
func (r *pwRequest) estimate() (time.Duration, error) {
	switch r.Alg {
	case "bcrypt":
		return hashgate.BcryptEstimate(r.Cost), nil
	case "bcrypt-verify":
		cost, err := pwhash.BcryptCost(r.Stored)
		if err != nil {
			return 0, operationError("the stored bcrypt string is malformed or uses the legacy $2x$ form")
		}
		return hashgate.BcryptEstimate(cost), nil
	}
	return hashgate.Argon2Estimate(int64(r.Memory), int64(r.Passes)), nil
}

// MemoryCharger is implemented by a Host that counts memory an evaluation
// uses outside the JavaScript heap (Argon2's) against the request.
type MemoryCharger interface {
	// ChargeMemory records n bytes; an error means they do not fit.
	ChargeMemory(ctx context.Context, n int64) error
}

// HashBusySuffix ends the message of the OperationError a busy hashing
// refusal throws; a host answers 503 when it escapes the script (R-JS-39d).
const HashBusySuffix = "; try again shortly"

// servePwhash runs one job on the host: it takes a slot of the hashing
// gate, which refuses when the server or the site is at its share.
func servePwhash(ctx context.Context, host Host, payload string) (string, error) {
	var r pwRequest
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		return "", errors.New("bad hash request")
	}
	est, err := r.estimate()
	if err != nil {
		return "", err
	}
	var mem int64
	if strings.HasPrefix(r.Alg, "Argon2") {
		mem = int64(r.Memory)
		if mc, ok := host.(MemoryCharger); ok {
			if err := mc.ChargeMemory(ctx, mem<<10); err != nil {
				return "", operationError("Argon2's memory does not fit the request's memory budget")
			}
		}
	}
	var deadline time.Time
	if r.Deadline != 0 {
		deadline = time.Unix(0, r.Deadline)
	}
	release, err := hashgate.Default().Acquire(ctx, mem, est, deadline)
	if err != nil {
		if ed := hashgate.Refused(err); ed != nil {
			return "", operationError(strings.TrimSuffix(ed.Message, "; try again") + HashBusySuffix)
		}
		return "", operationError(err.Error())
	}
	defer release()
	switch r.Alg {
	case "bcrypt":
		out, err := pwhash.Bcrypt(r.Password, r.Salt, r.Cost)
		if err != nil {
			return "", operationError(err.Error())
		}
		return hex.EncodeToString(out), nil
	case "bcrypt-verify":
		ok, err := pwhash.BcryptCheck(r.Password, r.Stored)
		if err != nil {
			return "", operationError("the stored bcrypt string is malformed or uses the legacy $2x$ form")
		}
		if ok {
			return "true", nil
		}
		return "false", nil
	case "Argon2id", "Argon2i", "Argon2d":
		mode := map[string]int{"Argon2d": argon2x.Argon2d, "Argon2i": argon2x.Argon2i, "Argon2id": argon2x.Argon2id}[r.Alg]
		return hex.EncodeToString(argon2x.Derive(mode, r.Password, r.Salt, r.Secret, r.Data, r.Passes, r.Memory, r.Lanes, r.Length)), nil
	}
	return "", errors.New("bad hash request")
}

// pwhash is the worker side: it refuses work that cannot finish in the
// time the evaluation has left, then asks the host to run it.
func (st *evalState) pwhash(payload string) (any, error) {
	var r pwRequest
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		return nil, errors.New("bad hash request")
	}
	est, err := r.estimate()
	if err != nil {
		return nil, err
	}
	if !st.deadline.IsZero() {
		if time.Now().Add(est).After(st.deadline) {
			return nil, operationError("the work would exceed the time budget; lower the cost, memory or passes")
		}
		r.Deadline = st.deadline.UnixNano()
	}
	b, _ := json.Marshal(&r)
	reply, err := st.callback(&cbMsg{Op: "pwhash", Value: string(b)})
	if err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New(reply.Error)
	}
	switch reply.Value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return reply.Value, nil
}
