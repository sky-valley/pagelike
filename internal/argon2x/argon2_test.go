package argon2x

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// The test vectors of RFC 9106 section 5: 32 KiB, 3 passes, 4 lanes, a
// secret value and associated data.
func TestRFC9106Vectors(t *testing.T) {
	password := bytes.Repeat([]byte{1}, 32)
	salt := bytes.Repeat([]byte{2}, 16)
	secret := bytes.Repeat([]byte{3}, 8)
	data := bytes.Repeat([]byte{4}, 12)
	for _, c := range []struct {
		mode int
		want string
	}{
		{Argon2d, "512b391b6f1162975371d30919734294f868e3be3984f3c1a13a4db9fabe4acb"},
		{Argon2i, "c814d9d1dc7f37aa13f0d77f2494bda1c8de6b016dd388d29952a4c4672b6ce8"},
		{Argon2id, "0d640df58d78766c08c037a34a8b53c9d01ef0452d75b65eb52520e96b01e659"},
	} {
		got := hex.EncodeToString(Derive(c.mode, password, salt, secret, data, 3, 32, 4, 32))
		if got != c.want {
			t.Errorf("mode %d: %s, want %s", c.mode, got, c.want)
		}
	}
}
