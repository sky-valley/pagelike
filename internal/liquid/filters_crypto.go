package liquid

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Hashing and random filters (R-LIQ-180 … R-LIQ-187). Randomness comes from
// Options.Rand (the OS CSPRNG by default); costly parameters are capped so
// one filter call cannot eat a request's budget.

//go:embed wordlist.txt
var wordListText string

// defaultWordList is diceware's list: 1,939 common lower-case English words
// selected for pagelike (Apache-2.0, like the rest of the project). PageLove's
// own list is not published (R-LIQ-186).
var defaultWordList = strings.Fields(wordListText)

const (
	upperChars  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	lowerChars  = "abcdefghijklmnopqrstuvwxyz"
	digitChars  = "0123456789"
	symbolChars = "!@#$%^&*()-_=+[]{};:,.<>?" // the docs' list ends in "…"; the last seven are pagelike's
)

func cryptoFilters() map[string]filterSpec {
	return map[string]filterSpec{
		"sha256": {fn: func(c *fcall) (any, error) {
			sum := sha256.Sum256([]byte(toString(c.in)))
			return hex.EncodeToString(sum[:]), nil
		}},
		"bcrypt":   {max: 1, fn: bcryptFilter},
		"argon2":   {kwargs: []string{"format", "salt", "memory", "time", "length"}, fn: argon2Filter},
		"random":   {kwargs: []string{"upper", "lower", "digits", "symbols", "alphanumeric", "url_safe", "chars", "chars_min"}, fn: randomFilter},
		"diceware": {fn: dicewareFilter},
	}
}

// bcryptFilter is `bcrypt[: cost = 12]` (R-LIQ-181): a $2b$ modular-crypt
// hash with a fresh salt.
func bcryptFilter(c *fcall) (any, error) {
	cost, err := c.integer(0, 12)
	if err != nil {
		return nil, err
	}
	if cost < int64(bcrypt.MinCost) || cost > int64(bcrypt.MaxCost) {
		return nil, filterError("cost %d is out of range [%d, %d]", cost, bcrypt.MinCost, bcrypt.MaxCost)
	}
	if limit := int64(c.st.opts.MaxBcryptCost); cost > limit {
		return nil, filterError("cost %d exceeds this server's maximum of %d", cost, limit)
	}
	pw := []byte(toString(c.in))
	if len(pw) > 72 {
		return nil, filterError("input is longer than 72 bytes")
	}
	h, err := bcrypt.GenerateFromPassword(pw, int(cost))
	if err != nil {
		return nil, filterError("%v", err)
	}
	// Go writes the $2a$ prefix; its hashes are the $2b$ format.
	return "$2b$" + strings.TrimPrefix(string(h), "$2a$"), nil
}

// argon2Filter is argon2id (R-LIQ-182): PHC output with a random salt by
// default, or `format: "raw"` with a caller-supplied salt.
func argon2Filter(c *fcall) (any, error) {
	format := "phc"
	if v, ok := c.kw["format"]; ok {
		format = toString(v)
	}
	param := func(name string, def, lo, hi int64) (int64, error) {
		v, ok := c.kw[name]
		if !ok || v == nil {
			return def, nil
		}
		n, err := c.intValue(v, name)
		if err != nil {
			return 0, err
		}
		if n < lo || n > hi {
			return 0, filterError("%s %d is out of range [%d, %d]", name, n, lo, hi)
		}
		return n, nil
	}
	memory, err := param("memory", 19456, 8, c.st.opts.MaxArgon2Memory)
	if err != nil {
		return nil, err
	}
	time, err := param("time", 2, 1, c.st.opts.MaxArgon2Time)
	if err != nil {
		return nil, err
	}
	length, err := param("length", 32, 4, 64)
	if err != nil {
		return nil, err
	}
	var salt []byte
	switch format {
	case "raw":
		s, ok := c.kw["salt"]
		if !ok || len(toString(s)) < 8 {
			return nil, filterError(`format "raw" requires a salt of at least 8 bytes`)
		}
		salt = []byte(toString(s))
	case "phc":
		salt = make([]byte, 16)
		if _, err := io.ReadFull(c.st.opts.Rand, salt); err != nil {
			return nil, filterError("no randomness available")
		}
	default:
		return nil, filterError("unknown format %q", format)
	}
	// Argon2 work is proportional to memory × passes; charge it.
	if err := c.st.charge(memory * time / 64); err != nil {
		return nil, err
	}
	h := argon2.IDKey([]byte(toString(c.in)), salt, uint32(time), uint32(memory), 1, uint32(length))
	if format == "raw" {
		return hex.EncodeToString(h), nil
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=1$%s$%s", argon2.Version, memory, time, enc.EncodeToString(salt), enc.EncodeToString(h)), nil
}

type charClass struct {
	set []rune
	min int
}

// randomFilter is `N | random` (R-LIQ-185). With no class keyword the
// alphabet is A–Z a–z 0–9; otherwise it is the union of the enabled classes.
// A class keyword takes true, false, or a minimum count.
func randomFilter(c *fcall) (any, error) {
	n, err := c.intValue(c.in, "length")
	if err != nil {
		return nil, err
	}
	if n < 1 || n > int64(c.st.opts.MaxRandomLength) {
		return nil, filterError("length %d is out of range [1, %d]", n, c.st.opts.MaxRandomLength)
	}
	option := func(name string) (given, enabled bool, minCount int, err error) {
		v, ok := c.kw[name]
		if !ok || v == nil {
			return false, false, 0, nil
		}
		if b, isBool := v.(bool); isBool {
			return true, b, 0, nil
		}
		m, err := c.intValue(v, name)
		if err != nil || m < 0 {
			return true, false, 0, filterError("%s must be true, false or a count", name)
		}
		return true, m > 0, int(m), nil
	}
	named := []struct{ name, set string }{
		{"upper", upperChars}, {"lower", lowerChars}, {"digits", digitChars},
		{"symbols", symbolChars}, {"alphanumeric", upperChars + lowerChars + digitChars},
	}
	var classes []charClass
	explicit := false
	disabled := map[string]bool{}
	for _, cl := range named {
		given, enabled, m, err := option(cl.name)
		if err != nil {
			return nil, err
		}
		if enabled {
			classes = append(classes, charClass{[]rune(cl.set), m})
			explicit = true
		} else if given {
			disabled[cl.name] = true
		}
	}
	if s := toString(c.kw["chars"]); s != "" {
		cm, err := c.intValue(orZero(c.kw["chars_min"]), "chars_min")
		if err != nil {
			return nil, err
		}
		classes = append(classes, charClass{dedupeRunes(s), int(cm)})
		explicit = true
	}
	alphabet := map[rune]bool{}
	var all []rune
	addAll := func(set []rune) {
		for _, r := range set {
			if !alphabet[r] {
				alphabet[r] = true
				all = append(all, r)
			}
		}
	}
	switch {
	case truthy(c.kw["url_safe"]):
		addAll([]rune(upperChars + lowerChars + digitChars + "-_"))
	case explicit:
		for _, cl := range classes {
			addAll(cl.set)
		}
	default:
		for _, cl := range named[:3] {
			if !disabled[cl.name] {
				addAll([]rune(cl.set))
			}
		}
	}
	if len(all) == 0 {
		return nil, filterError("no characters to choose from")
	}
	total := 0
	for _, cl := range classes {
		total += cl.min
	}
	if int64(total) > n {
		return nil, filterError("minimum counts (%d) exceed the length %d", total, n)
	}
	rnd := c.st.opts.Rand
	out := make([]rune, 0, n)
	for _, cl := range classes {
		for range cl.min {
			i, err := randInt(rnd, len(cl.set))
			if err != nil {
				return nil, err
			}
			out = append(out, cl.set[i])
		}
	}
	for int64(len(out)) < n {
		i, err := randInt(rnd, len(all))
		if err != nil {
			return nil, err
		}
		out = append(out, all[i])
	}
	for i := len(out) - 1; i > 0; i-- { // Fisher–Yates, after the minima are placed
		j, err := randInt(rnd, i+1)
		if err != nil {
			return nil, err
		}
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}

func orZero(v any) any {
	if v == nil {
		return 0
	}
	return v
}

func dedupeRunes(s string) []rune {
	seen := map[rune]bool{}
	var out []rune
	for _, r := range s {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// dicewareFilter is `N | diceware` (R-LIQ-186): N words (default 3,
// clamped to 1–10) joined with hyphens.
func dicewareFilter(c *fcall) (any, error) {
	n := int64(3)
	if i, f, isInt, ok := number(c.in); ok {
		n = i
		if !isInt {
			n = satTrunc(f)
		}
	}
	n = max(1, min(10, n))
	list := c.st.opts.WordList
	words := make([]string, n)
	for i := range words {
		j, err := randInt(c.st.opts.Rand, len(list))
		if err != nil {
			return nil, err
		}
		words[i] = list[j]
	}
	return strings.Join(words, "-"), nil
}

// randInt returns a uniform int in [0, n) without modulo bias.
func randInt(r io.Reader, n int) (int, error) {
	var b [8]byte
	limit := ^uint64(0) - (^uint64(0) % uint64(n))
	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, filterError("no randomness available")
		}
		if v := binary.BigEndian.Uint64(b[:]); v < limit {
			return int(v % uint64(n)), nil
		}
	}
}
