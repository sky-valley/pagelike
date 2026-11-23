package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Byte ranges (docs reference/protocol/Byte-Ranges, snapshot 2026-11-23;
// spec R-PROTO-140..143). A GET for a whole page or a stored file may ask
// for "bytes=" ranges; HEAD ignores them. Ranges are cut from the final
// bytes, after processors.

// acceptRanges is advertised on every GET and HEAD of a page or file, and on
// the 206 and 416 answers to a byte range.
const acceptRanges = "selector, bytes"

const (
	maxByteRanges  = 16  // more ranges: the whole resource
	maxRangeItems  = 64  // more list elements (empty ones count): the whole resource
	rangePartFrame = 128 // framing cost assumed per multipart part
	rangeWhole     = iota
	rangeParts
	rangeUnsatisfiable
)

type byteSpan struct{ start, end int64 } // inclusive

// parseByteRanges interprets a "bytes=" Range header over size bytes. The
// result is rangeWhole (serve the whole resource with 200: malformed, too
// many, overlapping, unordered, costlier than the whole, or an empty
// body), rangeUnsatisfiable (every range starts after the last byte, or
// asks for the last 0 bytes) or rangeParts with the spans to send.
func parseByteRanges(h string, size int64) ([]byteSpan, int) {
	unit, spec, ok := strings.Cut(h, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(unit), "bytes") || size == 0 {
		return nil, rangeWhole
	}
	items := strings.Split(spec, ",")
	if len(items) > maxRangeItems {
		return nil, rangeWhole
	}
	var spans []byteSpan
	n := 0
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		n++
		if n > maxByteRanges {
			return nil, rangeWhole
		}
		a, b, found := strings.Cut(it, "-")
		if !found {
			return nil, rangeWhole
		}
		a, b = strings.TrimSpace(a), strings.TrimSpace(b)
		if a == "" {
			k, err := strconv.ParseInt(b, 10, 64)
			if err != nil || k < 0 {
				return nil, rangeWhole
			}
			if k == 0 {
				continue // the last 0 bytes: unsatisfiable
			}
			spans = append(spans, byteSpan{max(size-k, 0), size - 1})
			continue
		}
		first, err := strconv.ParseInt(a, 10, 64)
		if err != nil || first < 0 {
			return nil, rangeWhole
		}
		last := size - 1
		if b != "" {
			if last, err = strconv.ParseInt(b, 10, 64); err != nil || last < first {
				return nil, rangeWhole
			}
			last = min(last, size-1)
		}
		if first >= size {
			continue // starts after the last byte: dropped
		}
		spans = append(spans, byteSpan{first, last})
	}
	if n == 0 {
		return nil, rangeWhole
	}
	if len(spans) == 0 {
		return nil, rangeUnsatisfiable
	}
	if len(spans) > 1 {
		var cost int64
		for i, s := range spans {
			if i > 0 && s.start <= spans[i-1].end {
				return nil, rangeWhole // overlapping or not ascending
			}
			cost += s.end - s.start + 1 + rangePartFrame
		}
		if cost >= size {
			return nil, rangeWhole
		}
	}
	return spans, rangeParts
}

// writeByteRanges sends the spans of content (size bytes, of type ct) as a
// 206: one span directly with Content-Range, several as
// multipart/byteranges. The caller has set the whole resource's validators
// and caching headers.
func writeByteRanges(w http.ResponseWriter, content io.ReaderAt, size int64, ct string, spans []byteSpan) {
	h := w.Header()
	h.Set("Accept-Ranges", acceptRanges)
	if len(spans) == 1 {
		s := spans[0]
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", s.start, s.end, size))
		h.Set("Content-Length", strconv.FormatInt(s.end-s.start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		io.Copy(w, io.NewSectionReader(content, s.start, s.end-s.start+1))
		return
	}
	var rb [16]byte
	rand.Read(rb[:])
	boundary := hex.EncodeToString(rb[:])
	var parts []string
	var total int64
	for _, s := range spans {
		head := fmt.Sprintf("--%s\r\nContent-Type: %s\r\nContent-Range: bytes %d-%d/%d\r\n\r\n", boundary, ct, s.start, s.end, size)
		parts = append(parts, head)
		total += int64(len(head)) + s.end - s.start + 1 + 2
	}
	tail := "--" + boundary + "--\r\n"
	total += int64(len(tail))
	h.Del("Content-Range")
	h.Set("Content-Type", "multipart/byteranges; boundary="+boundary)
	h.Set("Content-Length", strconv.FormatInt(total, 10))
	w.WriteHeader(http.StatusPartialContent)
	for i, s := range spans {
		io.WriteString(w, parts[i])
		io.Copy(w, io.NewSectionReader(content, s.start, s.end-s.start+1))
		io.WriteString(w, "\r\n")
	}
	io.WriteString(w, tail)
}
