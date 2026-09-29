package dom

import (
	"strings"

	"golang.org/x/net/html"
)

const markerAttr = "data-pagelike-el"

// ParseDocumentPrefixedAsElements parses src. In PageLove's model
// (BuildTree) a prefixed element (<p:include …>, <t:hello/>) always stays
// where it was written, "<p:include … />" is closed, and its content is
// ordinary markup, which is what the PageLove docs and apps rely on
// (pagelove-ats roles.html: <table><p:include … selector="#roles-body">).
func ParseDocumentPrefixedAsElements(src string) (*html.Node, Implied, error) {
	return ParseDocument(src)
}

// renamePrefixed turns the marker templates written by rewritePrefixed back
// into the prefixed elements they stand for.
func renamePrefixed(n *html.Node) {
	if n.Type == html.ElementNode && n.Data == "template" {
		for i, a := range n.Attr {
			if a.Key == markerAttr && a.Namespace == "" {
				n.Data, n.DataAtom = a.Val, 0
				n.Attr = append(n.Attr[:i:i], n.Attr[i+1:]...)
				break
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renamePrefixed(c)
	}
}

// IsPrefixed reports whether n is a prefixed element (<p:include>), which
// the parser keeps where the author placed it (see
// ParseDocumentPrefixedAsElements).
func IsPrefixed(n *html.Node) bool {
	return n != nil && n.Type == html.ElementNode && n.DataAtom == 0 && strings.Contains(n.Data, ":")
}

// rewritePrefixed rewrites prefixed start and end tags. Tags are found by
// the tokenizer, so text inside <script>, <style> and other raw-text
// elements is never touched; attributes are copied verbatim.
func rewritePrefixed(src string) string {
	if !strings.Contains(src, ":") {
		return src
	}
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(src))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return b.String()
		}
		raw := string(z.Raw())
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, _ := z.TagName()
			if !strings.Contains(string(name), ":") {
				break
			}
			if tt == html.EndTagToken {
				b.WriteString("</template>")
				continue
			}
			// raw is "<NAME attrs…>" or "<NAME attrs…/>"; keep attrs verbatim.
			attrs := raw[1+len(name):]
			attrs = strings.TrimSuffix(attrs, ">")
			selfClose := strings.HasSuffix(attrs, "/")
			attrs = strings.TrimSuffix(attrs, "/")
			b.WriteString(`<template ` + markerAttr + `="` + string(name) + `"` + attrs + ">")
			if selfClose {
				b.WriteString("</template>")
			}
			continue
		}
		b.WriteString(raw)
	}
}
