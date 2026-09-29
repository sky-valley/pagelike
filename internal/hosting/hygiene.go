package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"mime"
	"net/url"
	"path"
	"strings"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/store"
	"golang.org/x/net/html"
)

type imageBuffer struct{ bytes.Buffer }

func (b *imageBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 10<<20 {
		return 0, errors.New("encoded image exceeds upload limit")
	}
	return b.Buffer.Write(p)
}

func unsafeContribution() error {
	return errdoc.New(422, "InvalidContribution", "Contributions must contain data, not executable code or runtime configuration.")
}

// Participant HTML is inert data even when included in an authored page.
// Authors retain full browser/server composition in their immutable bundle.
func dataHTML(root *html.Node, incoming bool) error {
	var invalid bool
	allowed := map[string]bool{}
	for _, tag := range strings.Fields("html head body title div span p br hr ul ol li dl dt dd article section main header footer h1 h2 h3 h4 h5 h6 b strong i em u s small sub sup blockquote pre code a img figure figcaption table caption thead tbody tfoot tr th td time details summary label input button select option textarea meta") {
		allowed[tag] = true
	}
	dom.Walk(root, func(n *html.Node) bool {
		if n.Type == html.TextNode && (strings.Contains(n.Data, "{{") || strings.Contains(n.Data, "{%")) {
			invalid = true
		}
		if n.Type != html.ElementNode {
			return !invalid
		}
		if n.Namespace != "" || !allowed[n.Data] {
			invalid = true
			return false
		}
		for _, a := range n.Attr {
			k := strings.ToLower(a.Key)
			if a.Namespace != "" || strings.Contains(k, ":") || strings.HasPrefix(k, "on") || k == "xmlns" || k == "style" || k == "srcdoc" || k == "http-equiv" || (incoming && strings.HasPrefix(k, "data-contribution")) {
				invalid = true
				return false
			}
			if k == "itemtype" {
				for _, typ := range strings.Fields(a.Val) {
					u, e := url.Parse(typ)
					if e != nil || u.Hostname() == "pagelove.org" || u.Hostname() == "pagelike.org" {
						invalid = true
						return false
					}
				}
			}
			if k == "href" || k == "src" || k == "action" || k == "formaction" || k == "poster" || k == "srcset" {
				u, e := url.Parse(a.Val)
				if e != nil || k == "srcset" || u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
					invalid = true
					return false
				}
			}
		}
		return !invalid
	})
	if invalid {
		return unsafeContribution()
	}
	return nil
}

func contributionBody(p, ct string, body []byte) ([]byte, error) {
	media, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, unsafeContribution()
	}
	if strings.HasPrefix(p, "/uploads/") {
		if len(body) > 10<<20 {
			return nil, errdoc.New(413, "UploadTooLarge", "Images must be at most 10 MiB.")
		}
		ext := strings.ToLower(path.Ext(p))
		if !(media == "image/jpeg" && (ext == ".jpg" || ext == ".jpeg") || media == "image/png" && ext == ".png" || media == "image/gif" && ext == ".gif") {
			return nil, unsafeContribution()
		}
		cfg, format, e := image.DecodeConfig(bytes.NewReader(body))
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
			return nil, errdoc.New(422, "InvalidImage", "Use a valid image up to 16 megapixels.")
		}
		img, actual, e := image.Decode(bytes.NewReader(body))
		if e != nil || actual != format || "image/"+format != media {
			return nil, unsafeContribution()
		}
		var out imageBuffer
		switch format {
		case "jpeg":
			e = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90})
		case "png":
			e = png.Encode(&out, img)
		case "gif":
			e = gif.Encode(&out, img, nil)
		default:
			return nil, unsafeContribution()
		}
		// Decoding and encoding pixels deliberately drops all uploaded metadata.
		if e != nil {
			return nil, unsafeContribution()
		}
		return out.Bytes(), nil
	}
	if len(body) > 512<<10 {
		return nil, errdoc.New(413, "ContributionTooLarge", "Contributions must be at most 512 KiB.")
	}
	switch {
	case media == "text/html" && path.Ext(p) == ".html":
		root, e := dom.Parse(body)
		if e != nil {
			return nil, unsafeContribution()
		}
		if e = dataHTML(root, true); e != nil {
			return nil, e
		}
		return dom.Render(root), nil
	case media == "application/json" && path.Ext(p) == ".json":
		if !json.Valid(body) {
			return nil, unsafeContribution()
		}
		return body, nil
	case media == "text/plain" && path.Ext(p) == ".txt":
		return body, nil
	default:
		return nil, unsafeContribution()
	}
}

func protectWrite(_ context.Context, w *engine.WriteCtx) error {
	if !store.LivePath(w.Op.Path) || w.Op.Destination != "" && !store.LivePath(w.Op.Destination) {
		return errdoc.New(403, "AuthoredContent", "Publish a version to change authored content.")
	}
	if w.Op.Method == "MOVE" {
		return errdoc.New(405, "ContributionMove", "Contributions cannot be moved between documents.")
	}
	if w.Op.Method == "DELETE" {
		return nil
	}
	var err error
	w.Op.Body, err = contributionBody(w.Op.Path, engine.ResolveContentType(w.Op.Path, w.Op.ContentType), w.Op.Body)
	return err
}

func guardStore(w *engine.WriteCtx, path string, doc *store.Document) error {
	if !store.LivePath(path) {
		return errdoc.New(403, "AuthoredContent", "Publish a version to change authored content.")
	}
	if doc == nil {
		return nil
	}
	if !doc.IsBlob() {
		if !strings.HasPrefix(doc.ContentType, "text/html") {
			return unsafeContribution()
		}
		root, err := dom.Parse(doc.Body)
		if err != nil {
			return err
		}
		if err := dataHTML(root, false); err != nil {
			return err
		}
	}
	var count, size int64
	if err := w.Tx.QueryRow(`SELECT COUNT(*),COALESCE(SUM(size),0) FROM documents WHERE path LIKE '/data/%' OR path LIKE '/uploads/%'`).Scan(&count, &size); err != nil {
		return err
	}
	previous := int64(0)
	old, err := w.Tx.Get(path)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if old != nil {
		previous = old.Size
	} else {
		count++
	}
	next := doc.Size
	if !doc.IsBlob() {
		next = int64(len(doc.Body))
	}
	if count > 10000 || size-previous+next > 256<<20 && next > previous {
		return errdoc.New(413, "ParticipationFull", "This post has reached its participation storage limit.")
	}
	if err := w.Tx.QueryRow(`SELECT COUNT(*) FROM hosted_contributions`).Scan(&count); err != nil {
		return err
	}
	if count > 100000 {
		return errdoc.New(413, "ParticipationFull", "This post has reached its contribution limit.")
	}
	return nil
}
