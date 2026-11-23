package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseByteRanges(t *testing.T) {
	for _, c := range []struct {
		h     string
		size  int64
		spans []byteSpan
		mode  int
	}{
		{"bytes=0-3", 10, []byteSpan{{0, 3}}, rangeParts},
		{"bytes=4-", 10, []byteSpan{{4, 9}}, rangeParts},
		{"bytes=-3", 10, []byteSpan{{7, 9}}, rangeParts},
		{"bytes=-30", 10, []byteSpan{{0, 9}}, rangeParts},
		{"bytes=5-100", 10, []byteSpan{{5, 9}}, rangeParts},
		{"bytes=10-", 10, nil, rangeUnsatisfiable},
		{"bytes=-0", 10, nil, rangeUnsatisfiable},
		{"bytes=3-1", 10, nil, rangeWhole},
		{"bytes=x-", 10, nil, rangeWhole},
		{"bytes=0-1", 0, nil, rangeWhole},                                       // empty body
		{"bytes=0-1,4-5", 10, nil, rangeWhole},                                  // costlier than the body
		{"bytes=0-1,400-401", 1000, []byteSpan{{0, 1}, {400, 401}}, rangeParts}, // two parts
		{"bytes=0-1, 2000-", 1000, []byteSpan{{0, 1}}, rangeParts},              // one dropped
		{"bytes=400-401,0-1", 1000, nil, rangeWhole},                            // not ascending
		{"bytes=0-5,3-8", 1000, nil, rangeWhole},                                // overlapping
		{"bytes=0-0,,,", 1000, []byteSpan{{0, 0}}, rangeParts},                  // empty elements
		{"foo=0-1", 10, nil, rangeWhole},
	} {
		spans, mode := parseByteRanges(c.h, c.size)
		if mode != c.mode || len(spans) != len(c.spans) {
			t.Errorf("parseByteRanges(%q, %d) = %v %d, want %v %d", c.h, c.size, spans, mode, c.spans, c.mode)
			continue
		}
		for i := range spans {
			if spans[i] != c.spans[i] {
				t.Errorf("parseByteRanges(%q, %d) = %v, want %v", c.h, c.size, spans, c.spans)
			}
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
