package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestByteRange(t *testing.T) {
	for _, c := range []struct {
		h                 string
		size              int64
		start, end        int64
		ok, unsatisfiable bool
	}{
		{"bytes=0-3", 10, 0, 3, true, false},
		{"bytes=4-", 10, 4, 9, true, false},
		{"bytes=-3", 10, 7, 9, true, false},
		{"bytes=-30", 10, 0, 9, true, false},
		{"bytes=5-100", 10, 5, 9, true, false},
		{"bytes=10-", 10, 0, 0, false, true},
		{"bytes=-0", 10, 0, 0, false, true},
		{"bytes=3-1", 10, 0, 0, false, false}, // invalid: ignored
		{"bytes=0-1,4-5", 10, 0, 0, false, false},
		{"bytes=x-", 10, 0, 0, false, false},
	} {
		s, e, ok, un := byteRange(c.h, c.size)
		if s != c.start || e != c.end || ok != c.ok || un != c.unsatisfiable {
			t.Errorf("byteRange(%q, %d) = %d %d %v %v", c.h, c.size, s, e, ok, un)
		}
	}
}

func TestReadPreconditions(t *testing.T) {
	mod := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		headers map[string]string
		want    int
	}{
		{nil, 0},
		{map[string]string{"If-Match": `"stale"`}, 0}, // ignored on reads (live 2026-09-28)
		{map[string]string{"If-Match": `"cur"`}, 0},
		{map[string]string{"If-None-Match": `"cur"`}, 304},
		{map[string]string{"If-None-Match": `W/"cur"`}, 304},
		{map[string]string{"If-None-Match": `"other", "cur"`}, 304},
		{map[string]string{"If-None-Match": `"other"`, "If-Modified-Since": mod.Format(time.RFC1123)}, 0}, // IMS ignored with INM
		{map[string]string{"If-Modified-Since": "Mon, 28 Sep 2026 12:00:00 GMT"}, 304},
		{map[string]string{"If-Modified-Since": "Mon, 28 Sep 2026 11:00:00 GMT"}, 0},
		{map[string]string{"If-Unmodified-Since": "Mon, 28 Sep 2026 11:00:00 GMT"}, 412},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		if got := evalPreconditions(r, `"cur"`, mod); got != c.want {
			t.Errorf("%v: %d, want %d", c.headers, got, c.want)
		}
	}
}
