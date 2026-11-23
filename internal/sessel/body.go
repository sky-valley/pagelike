package sessel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"strings"
)

// ParseBody is Context.request.body (R-SESSEL-291, docs snapshot
// 2026-11-23): the body parsed by its content type. A form or multipart
// post is a map of its fields (the last value of a repeated field), JSON is
// the parsed value, and any other type the raw string. An empty or missing
// body is an empty map. A body that does not parse as its type stays raw.
func ParseBody(contentType string, body []byte) Value {
	if len(body) == 0 {
		return NewDict()
	}
	mt, params, _ := mime.ParseMediaType(contentType)
	switch mt {
	case "application/x-www-form-urlencoded":
		d := NewDict()
		raw := strings.TrimRight(string(body), " \t\r\n")
		for raw != "" {
			var part string
			part, raw, _ = strings.Cut(raw, "&")
			if part == "" {
				continue
			}
			name, value, _ := strings.Cut(part, "=")
			d.Set(formUnescape(name), formUnescape(value))
		}
		return d
	case "application/json":
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		if v, err := jsonBodyNext(dec); err == nil {
			if _, err := dec.Token(); err == io.EOF {
				return v
			}
		}
	case "multipart/form-data":
		if d, ok := multipartBody(body, params["boundary"]); ok {
			return d
		}
	}
	return string(body)
}

func formUnescape(s string) string {
	if v, err := url.QueryUnescape(s); err == nil {
		return v
	}
	return s
}

func jsonBodyNext(dec *json.Decoder) (Value, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch x := t.(type) {
	case json.Delim:
		switch x {
		case '{':
			d := NewDict()
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := jsonBodyNext(dec)
				if err != nil {
					return nil, err
				}
				d.Set(k.(string), v)
			}
			_, err := dec.Token()
			return d, err
		case '[':
			l := List{}
			for dec.More() {
				v, err := jsonBodyNext(dec)
				if err != nil {
					return nil, err
				}
				l = append(l, v)
			}
			_, err := dec.Token()
			return l, err
		}
		return nil, fmt.Errorf("unexpected %v", x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		return x.Float64()
	case string:
		return x, nil
	case bool:
		return x, nil
	}
	return nil, nil // null
}

// multipartBody returns the text fields of a multipart/form-data body;
// file parts are skipped.
func multipartBody(body []byte, boundary string) (*Dict, bool) {
	if boundary == "" {
		return nil, false
	}
	d := NewDict()
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return d, true
		}
		if err != nil {
			return nil, false
		}
		if p.FileName() != "" || p.FormName() == "" {
			continue
		}
		v, err := io.ReadAll(p)
		if err != nil {
			return nil, false
		}
		d.Set(p.FormName(), string(v))
	}
}
