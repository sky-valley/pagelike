package sessel

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	mrand "math/rand/v2"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/text/unicode/norm"
)

// hashMethod implements .sha256(), .hmac_sha256(key), .bcrypt([options])
// and .argon2([options]) (R-SESSEL-118..121).
func hashMethod(r *run, s, name string, args []Value) (Value, error) {
	switch name {
	case "sha256":
		if err := argCount(name, args, 0, 0); err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:]), nil
	case "hmac_sha256":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, err
		}
		key, err := strArg(name, args, 0)
		if err != nil {
			return nil, err
		}
		m := hmac.New(sha256.New, []byte(key))
		m.Write([]byte(s))
		return hex.EncodeToString(m.Sum(nil)), nil
	case "bcrypt":
		cost := 12
		opts, err := optionsArg(name, args)
		if err != nil {
			return nil, err
		}
		for _, k := range opts.Keys() {
			if k != "cost" {
				return nil, typeErr("bcrypt(): unknown option %q", k)
			}
			n, ok := opts.Lookup(k).(int64)
			if !ok || n < 4 || n > 31 {
				return nil, typeErr("bcrypt(): cost must be an Integer from 4 to 31")
			}
			cost = int(n)
		}
		pw := []byte(s)
		if len(pw) > 72 {
			pw = pw[:72]
		}
		// bcrypt cannot be interrupted: refuse costs that cannot finish
		// within the remaining time budget (cost 10 ≈ 50 ms, doubling).
		if err := r.affordable(50 * time.Millisecond << max(cost-10, 0)); err != nil {
			return nil, err
		}
		h, err := bcrypt.GenerateFromPassword(pw, cost)
		if err != nil {
			return nil, runtimeErr("bcrypt(): %v", err)
		}
		return string(h), nil
	case "argon2":
		mem, iter, length := int64(19456), int64(2), int64(32)
		opts, err := optionsArg(name, args)
		if err != nil {
			return nil, err
		}
		for _, k := range opts.Keys() {
			n, ok := opts.Lookup(k).(int64)
			if !ok {
				return nil, typeErr("argon2(): option %q must be an Integer", k)
			}
			switch k {
			case "memory":
				if n < 8 || n > 262144 {
					return nil, typeErr("argon2(): memory must be 8..262144 KiB")
				}
				mem = n
			case "time":
				if n < 1 || n > 16 {
					return nil, typeErr("argon2(): time must be 1..16")
				}
				iter = n
			case "length":
				if n < 4 || n > 1024 {
					return nil, typeErr("argon2(): length must be 4..1024")
				}
				length = n
			default:
				return nil, typeErr("argon2(): unknown option %q", k)
			}
		}
		if err := r.alloc(int(mem) << 10); err != nil {
			return nil, err
		}
		if err := r.affordable(time.Duration(mem*iter) * time.Microsecond); err != nil {
			return nil, err
		}
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return nil, runtimeErr("argon2(): %v", err)
		}
		key := argon2.IDKey([]byte(s), salt, uint32(iter), uint32(mem), 1, uint32(length))
		enc := base64.RawStdEncoding
		return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=1$%s$%s", argon2.Version, mem, iter, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
	}
	return nil, typeErr("no method '%s' on String", name)
}

// affordable refuses uninterruptible work estimated to outlast the
// remaining wall-clock budget.
func (r *run) affordable(est time.Duration) error {
	b := r.budget
	if b.exhausted != nil {
		return b.exhausted
	}
	if !b.Deadline.IsZero() && time.Now().Add(est).After(b.Deadline) {
		return &Error{Type: RuntimeErrorType, Message: fmt.Sprintf("budget exhausted: the operation needs about %v, more than the remaining time budget", est), Reason: ReasonTimeout}
	}
	return nil
}

func optionsArg(name string, args []Value) (*Dict, error) {
	if err := argCount(name, args, 0, 1); err != nil {
		return nil, err
	}
	if len(args) == 0 || args[0] == nil {
		return NewDict(), nil
	}
	d, ok := args[0].(*Dict)
	if !ok {
		return nil, typeErr("%s(): options must be a Dictionary, not %s", name, TypeName(args[0]))
	}
	return d, nil
}

// slugify lowercases, strips diacritics and joins runs of [a-z0-9] with
// single hyphens (R-SESSEL-123).
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFKD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		r = unicode.ToLower(r)
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	return b.String()
}

// randomNumber implements Integer/Float/Number.random(min, max)
// (R-SESSEL-103..105).
func randomNumber(kind string, args []Value) (Value, error) {
	if len(args) != 2 {
		return nil, typeErr("%s.random() takes min and max", kind)
	}
	for _, a := range args {
		if _, ok := toFloat(a); !ok {
			return nil, typeErr("%s.random() takes numbers, not %s", kind, TypeName(a))
		}
	}
	lo, hi := args[0], args[1]
	li, lInt := lo.(int64)
	hi64, hInt := hi.(int64)
	integer := kind == "Integer" || (kind == "Number" && lInt && hInt)
	if integer {
		if !lInt || !hInt {
			return nil, typeErr("Integer.random() takes Integers")
		}
		if li > hi64 {
			li, hi64 = hi64, li
		}
		if li == hi64 {
			return li, nil
		}
		span := uint64(hi64 - li)
		if span == ^uint64(0) {
			return int64(mrand.Uint64()), nil
		}
		return li + int64(mrand.Uint64N(span+1)), nil
	}
	lf, _ := toFloat(lo)
	hf, _ := toFloat(hi)
	if lf > hf {
		lf, hf = hf, lf
	}
	if lf == hf {
		return lf, nil
	}
	return lf + mrand.Float64()*(hf-lf), nil
}

const (
	alphaUpper   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	alphaLower   = "abcdefghijklmnopqrstuvwxyz"
	alphaDigits  = "0123456789"
	alphaSymbols = "!#$%&*+-=?@^_~"
)

// randomString implements String.random(length[, options]) (R-SESSEL-122).
func randomString(args []Value) (Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, typeErr("String.random() takes a length and optional options")
	}
	n, ok := args[0].(int64)
	if !ok || n < 0 {
		return nil, typeErr("String.random(): length must be a non-negative Integer")
	}
	if n > 1<<20 {
		return nil, typeErr("String.random(): length is too large")
	}
	type pool struct {
		chars []rune
		min   int
	}
	var pools []pool
	if len(args) == 2 && args[1] != nil {
		opts, ok := args[1].(*Dict)
		if !ok {
			return nil, typeErr("String.random(): options must be a Dictionary")
		}
		named := map[string]string{
			"upper": alphaUpper, "lower": alphaLower, "digits": alphaDigits, "symbols": alphaSymbols,
			"alphanumeric": alphaUpper + alphaLower + alphaDigits, "url_safe": alphaUpper + alphaLower + alphaDigits + "-_",
		}
		var custom []rune
		customMin := 0
		for _, k := range opts.Keys() {
			v := opts.Lookup(k)
			switch k {
			case "chars":
				s, ok := v.(string)
				if !ok {
					return nil, typeErr("String.random(): chars must be a String")
				}
				custom = []rune(s)
			case "chars_min":
				m, ok := v.(int64)
				if !ok || m < 0 {
					return nil, typeErr("String.random(): chars_min must be a non-negative Integer")
				}
				customMin = int(m)
			default:
				alpha, known := named[k]
				if !known {
					return nil, typeErr("String.random(): unknown option %q", k)
				}
				switch x := v.(type) {
				case bool:
					if x {
						pools = append(pools, pool{chars: []rune(alpha)})
					}
				case int64:
					if x < 0 {
						return nil, typeErr("String.random(): minimum for %q must be non-negative", k)
					}
					pools = append(pools, pool{chars: []rune(alpha), min: int(x)})
				default:
					return nil, typeErr("String.random(): option %q must be a Boolean or an Integer", k)
				}
			}
		}
		if len(custom) > 0 {
			pools = append(pools, pool{chars: custom, min: customMin})
		} else if customMin > 0 {
			return nil, typeErr("String.random(): chars_min needs chars")
		}
	}
	if len(pools) == 0 {
		pools = []pool{{chars: []rune(alphaUpper + alphaLower + alphaDigits)}}
	}
	total := 0
	var union []rune
	seen := map[rune]bool{}
	for _, p := range pools {
		total += p.min
		for _, r := range p.chars {
			if !seen[r] {
				seen[r] = true
				union = append(union, r)
			}
		}
	}
	if int64(total) > n {
		return nil, typeErr("String.random(): the minimums (%d) exceed the length (%d)", total, n)
	}
	out := make([]rune, 0, n)
	for _, p := range pools {
		for i := 0; i < p.min; i++ {
			out = append(out, p.chars[cryptoInt(len(p.chars))])
		}
	}
	for int64(len(out)) < n {
		out = append(out, union[cryptoInt(len(union))])
	}
	for i := len(out) - 1; i > 0; i-- {
		j := cryptoInt(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}

// cryptoInt returns a uniform integer in [0, n) from crypto/rand, by
// rejection sampling 32-bit draws.
func cryptoInt(n int) int {
	if n <= 1 {
		return 0
	}
	if n > math.MaxUint32 {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
		if err != nil {
			panic(err)
		}
		return int(v.Int64())
	}
	limit := math.MaxUint32 - (math.MaxUint32 % uint32(n))
	var b [4]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			panic(err)
		}
		v := binary.LittleEndian.Uint32(b[:])
		if v < limit {
			return int(v % uint32(n))
		}
	}
}
