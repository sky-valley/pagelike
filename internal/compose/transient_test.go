package compose_test

import (
	"testing"
)

const cartPage = `<!DOCTYPE html>
<html xmlns:p="https://pagelove.org/1.0">
<body><ul id="cart" p:transient><li id="item1">Apples</li><li id="item2">Milk</li></ul></body>
</html>
`

// A write to a transient element (or a descendant) changes only the
// session copy; the stored document keeps the default; the page is private.
func TestTransientSessionCopy(t *testing.T) {
	p := newPlane(t)
	p.allow("GET", "PUT", "POST", "DELETE")
	p.author("/cart.html", cartPage)

	r := p.do("GET", "/cart.html", "", nil)
	r.must(t, 200, `<ul id="cart">`, "Apples")
	r.mustNot(t, "p:transient", "xmlns:p")
	if r.header.Get("Cache-Control") != "private" {
		t.Errorf("Cache-Control %q", r.header.Get("Cache-Control"))
	}

	p.do("PUT", "/cart.html", `<li id="item1">Pears</li>`, map[string]string{"Range": "selector=#item1", "Content-Type": "text/html"}).must(t, 206)
	p.do("GET", "/cart.html", "", nil).must(t, 200, "Pears", "Milk")
	if s := p.stored("/cart.html"); s != cartPage {
		t.Errorf("stored document changed:\n%s", s)
	}

	p.do("POST", "/cart.html", `<li>Bread</li>`, map[string]string{"Range": "selector=#cart", "Content-Type": "text/html"}).must(t, 206)
	p.do("GET", "/cart.html", "", nil).must(t, 200, "Pears", "Bread")

	// Another session sees the default.
	other := *p
	other.cookies = map[string]string{}
	r = other.do("GET", "/cart.html", "", nil)
	r.must(t, 200, "Apples")
	r.mustNot(t, "Pears", "Bread")

	// Identity must be kept; the tag may change.
	p.do("PUT", "/cart.html", `<ul id="other"></ul>`, map[string]string{"Range": "selector=#cart", "Content-Type": "text/html"}).must(t, 422)
	p.do("PUT", "/cart.html", `<div id="cart"><p>Div cart</p></div>`, map[string]string{"Range": "selector=#cart", "Content-Type": "text/html"}).must(t, 206)
	p.do("GET", "/cart.html", "", nil).must(t, 200, `<div id="cart"><p>Div cart</p></div>`)

	// DELETE of the transient element drops the copy.
	p.do("DELETE", "/cart.html", "", map[string]string{"Range": "selector=#cart"}).must(t, 204)
	p.do("GET", "/cart.html", "", nil).must(t, 200, "Apples")
}
