package dom

import (
	"strings"

	"golang.org/x/net/html"
)

// FixComments restores comment data verbatim. x/net/html's tokenizer
// HTML-unescapes comment text ("<!-- &amp; -->" becomes " & "), which the
// WHATWG tokenizer (and html5ever) do not; re-serializing would silently
// rewrite such comments. We re-tokenize src, derive each comment's raw data
// from Tokenizer.Raw, and patch the tree's comment nodes in document order,
// only when the node's current data equals the unescaped raw data (so a
// mismatch between token order and tree order can never corrupt a comment).
func FixComments(root *html.Node, src string) {
	var raws []string
	z := html.NewTokenizer(strings.NewReader(src))
	for tt := z.Next(); tt != html.ErrorToken; tt = z.Next() {
		if tt == html.CommentToken {
			raws = append(raws, rawCommentData(string(z.Raw())))
		}
	}
	i := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.CommentNode && i < len(raws) {
			if html.UnescapeString(raws[i]) == n.Data {
				n.Data = raws[i]
			}
			i++
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

func rawCommentData(raw string) string {
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	raw = strings.ReplaceAll(raw, "\x00", "�")
	switch {
	case strings.HasPrefix(raw, "<!--"):
		d := raw[4:]
		for _, end := range []string{"--!>", "-->"} {
			if strings.HasSuffix(d, end) {
				return strings.TrimSuffix(d, end)
			}
		}
		if d == ">" || d == "->" { // <!--> and <!--->
			return ""
		}
		return strings.TrimSuffix(strings.TrimSuffix(d, "--"), "-")
	case strings.HasPrefix(raw, "<?"):
		return strings.TrimSuffix(raw[1:], ">")
	case strings.HasPrefix(raw, "<!"), strings.HasPrefix(raw, "</"):
		return strings.TrimSuffix(raw[2:], ">")
	}
	return raw
}
