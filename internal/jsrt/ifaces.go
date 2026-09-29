package jsrt

import (
	"encoding/json"
	"strings"
)

// The server DOM's element interfaces and their reflected properties
// (docs/spec/javascript.md R-JS-78/79) as one data table. The Go side uses
// it to pick an element's class; the JavaScript prelude receives it as JSON
// and builds every accessor from it.

// reflectKind is how a reflected property converts (R-JS-78).
type reflectKind string

const (
	rkString   reflectKind = "string"   // verbatim, "" when absent
	rkBool     reflectKind = "bool"     // presence
	rkLong     reflectKind = "long"     // WHATWG integer, default when absent/unparsable
	rkULong    reflectKind = "ulong"    // non-negative integer, default otherwise
	rkClamped  reflectKind = "clamped"  // non-negative integer clamped to [min, max]
	rkLimited  reflectKind = "limited"  // integer > 0, default otherwise
	rkEnum     reflectKind = "enum"     // canonical keyword with missing/invalid defaults
	rkNullEnum reflectKind = "nullenum" // crossOrigin
	rkBespoke  reflectKind = "bespoke"  // implemented by name in the prelude
)

type reflectProp struct {
	Prop    string            `json:"prop"`
	Attr    string            `json:"attr,omitempty"` // content attribute when not lower(prop)
	Kind    reflectKind       `json:"kind"`
	Def     float64           `json:"def,omitempty"`
	Min     float64           `json:"min,omitempty"`
	Max     float64           `json:"max,omitempty"`
	Keys    []string          `json:"keys,omitempty"`
	Alias   map[string]string `json:"alias,omitempty"`
	Missing *string           `json:"missing,omitempty"`
	Invalid *string           `json:"invalid,omitempty"`
	RO      bool              `json:"ro,omitempty"`
}

type ifaceDef struct {
	Name   string        `json:"name"`
	Parent string        `json:"parent"`
	Tags   []string      `json:"tags,omitempty"`
	Props  []reflectProp `json:"props,omitempty"`
}

func sp(s string) *string { return &s }

func str(names ...string) []reflectProp {
	out := make([]reflectProp, len(names))
	for i, n := range names {
		prop, attr, _ := strings.Cut(n, "=")
		out[i] = reflectProp{Prop: prop, Attr: attr, Kind: rkString}
	}
	return out
}

func boolp(names ...string) []reflectProp {
	out := make([]reflectProp, len(names))
	for i, n := range names {
		prop, attr, _ := strings.Cut(n, "=")
		out[i] = reflectProp{Prop: prop, Attr: attr, Kind: rkBool}
	}
	return out
}

func enum(prop, attr string, keys []string, missing, invalid string) reflectProp {
	return reflectProp{Prop: prop, Attr: attr, Kind: rkEnum, Keys: keys, Missing: sp(missing), Invalid: sp(invalid)}
}

func cat(parts ...[]reflectProp) []reflectProp {
	var out []reflectProp
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func one(p reflectProp) []reflectProp { return []reflectProp{p} }

var referrerPolicy = enum("referrerPolicy", "referrerpolicy", []string{"", "no-referrer", "no-referrer-when-downgrade", "same-origin", "origin", "strict-origin", "origin-when-cross-origin", "strict-origin-when-cross-origin", "unsafe-url"}, "", "")

var crossOrigin = reflectProp{Prop: "crossOrigin", Attr: "crossorigin", Kind: rkNullEnum}

var loading = enum("loading", "", []string{"lazy", "eager"}, "eager", "eager")

var inputTypes = []string{"text", "search", "tel", "url", "email", "password", "date", "month", "week", "time", "datetime-local", "number", "range", "color", "checkbox", "radio", "file", "submit", "image", "reset", "button", "hidden"}

// ifaceTable lists the interfaces from the root down; index 0 is Element
// (namespaced elements), 1 HTMLElement (HTML elements without a dedicated
// interface).
var ifaceTable = []ifaceDef{
	{Name: "Element", Parent: "Node"},
	{Name: "HTMLElement", Parent: "Element", Props: []reflectProp{
		enum("dir", "", []string{"ltr", "rtl", "auto"}, "", ""),
		enum("inputMode", "inputmode", []string{"none", "text", "tel", "url", "email", "numeric", "decimal", "search"}, "", ""),
		enum("enterKeyHint", "enterkeyhint", []string{"enter", "done", "go", "next", "previous", "search", "send"}, "", ""),
		{Prop: "autocapitalize", Kind: rkEnum, Keys: []string{"", "off", "none", "on", "sentences", "words", "characters"}, Alias: map[string]string{"off": "none", "on": "sentences"}, Missing: sp(""), Invalid: sp("sentences")},
		{Prop: "contentEditable", Attr: "contenteditable", Kind: rkBespoke},
		{Prop: "isContentEditable", Attr: "contenteditable", Kind: rkBespoke, RO: true},
	}},
	{Name: "HTMLMediaElement", Parent: "HTMLElement", Props: cat(str("src"), one(crossOrigin), str("preload"), boolp("autoplay", "loop", "controls"))},
	{Name: "HTMLAnchorElement", Parent: "HTMLElement", Tags: []string{"a"}, Props: cat(str("href", "target", "rel", "download", "hreflang", "type"), one(referrerPolicy))},
	{Name: "HTMLAreaElement", Parent: "HTMLElement", Tags: []string{"area"}, Props: cat(str("alt", "href", "target", "download", "rel", "hreflang", "type", "shape", "coords"), one(referrerPolicy))},
	{Name: "HTMLAudioElement", Parent: "HTMLMediaElement", Tags: []string{"audio"}},
	{Name: "HTMLBRElement", Parent: "HTMLElement", Tags: []string{"br"}},
	{Name: "HTMLBaseElement", Parent: "HTMLElement", Tags: []string{"base"}, Props: str("href", "target")},
	{Name: "HTMLBodyElement", Parent: "HTMLElement", Tags: []string{"body"}},
	{Name: "HTMLButtonElement", Parent: "HTMLElement", Tags: []string{"button"}, Props: cat(one(enum("type", "", []string{"submit", "reset", "button"}, "submit", "submit")), str("name", "value"), boolp("disabled"))},
	{Name: "HTMLCanvasElement", Parent: "HTMLElement", Tags: []string{"canvas"}, Props: []reflectProp{{Prop: "width", Kind: rkULong, Def: 300}, {Prop: "height", Kind: rkULong, Def: 150}}},
	{Name: "HTMLDListElement", Parent: "HTMLElement", Tags: []string{"dl"}},
	{Name: "HTMLDataElement", Parent: "HTMLElement", Tags: []string{"data"}, Props: str("value")},
	{Name: "HTMLDataListElement", Parent: "HTMLElement", Tags: []string{"datalist"}},
	{Name: "HTMLDetailsElement", Parent: "HTMLElement", Tags: []string{"details"}, Props: boolp("open")},
	{Name: "HTMLDialogElement", Parent: "HTMLElement", Tags: []string{"dialog"}, Props: boolp("open")},
	{Name: "HTMLDirectoryElement", Parent: "HTMLElement", Tags: []string{"dir"}, Props: boolp("compact")},
	{Name: "HTMLDivElement", Parent: "HTMLElement", Tags: []string{"div"}},
	{Name: "HTMLEmbedElement", Parent: "HTMLElement", Tags: []string{"embed"}, Props: str("src", "type", "width", "height")},
	{Name: "HTMLFieldSetElement", Parent: "HTMLElement", Tags: []string{"fieldset"}, Props: cat(boolp("disabled"), str("name"))},
	{Name: "HTMLFontElement", Parent: "HTMLElement", Tags: []string{"font"}, Props: str("color", "face", "size")},
	{Name: "HTMLFormElement", Parent: "HTMLElement", Tags: []string{"form"}, Props: cat(str("action"),
		one(enum("method", "", []string{"get", "post", "dialog"}, "get", "get")), str("name", "target"),
		one(enum("enctype", "", []string{"application/x-www-form-urlencoded", "multipart/form-data", "text/plain"}, "application/x-www-form-urlencoded", "application/x-www-form-urlencoded")),
		str("acceptCharset=accept-charset"))},
	{Name: "HTMLFrameElement", Parent: "HTMLElement", Tags: []string{"frame"}, Props: cat(str("name", "scrolling", "src", "frameBorder=frameborder", "longDesc=longdesc"), boolp("noResize=noresize"), str("marginHeight=marginheight", "marginWidth=marginwidth"))},
	{Name: "HTMLFrameSetElement", Parent: "HTMLElement", Tags: []string{"frameset"}, Props: str("cols", "rows")},
	{Name: "HTMLHRElement", Parent: "HTMLElement", Tags: []string{"hr"}},
	{Name: "HTMLHeadElement", Parent: "HTMLElement", Tags: []string{"head"}},
	{Name: "HTMLHeadingElement", Parent: "HTMLElement", Tags: []string{"h1", "h2", "h3", "h4", "h5", "h6"}},
	{Name: "HTMLHtmlElement", Parent: "HTMLElement", Tags: []string{"html"}},
	{Name: "HTMLIFrameElement", Parent: "HTMLElement", Tags: []string{"iframe"}, Props: cat(str("src", "srcdoc", "name", "width", "height", "allow"), one(loading), one(referrerPolicy))},
	{Name: "HTMLImageElement", Parent: "HTMLElement", Tags: []string{"img"}, Props: cat(str("src", "alt"),
		[]reflectProp{{Prop: "width", Kind: rkULong}, {Prop: "height", Kind: rkULong}}, str("srcset", "sizes"), one(loading),
		one(enum("decoding", "", []string{"sync", "async", "auto"}, "auto", "auto")), one(crossOrigin), one(referrerPolicy))},
	{Name: "HTMLInputElement", Parent: "HTMLElement", Tags: []string{"input"}, Props: cat(one(enum("type", "", inputTypes, "text", "text")),
		str("name", "value", "placeholder"), boolp("required", "disabled", "readOnly=readonly", "checked"), str("min", "max", "step", "pattern", "autocomplete"))},
	{Name: "HTMLLIElement", Parent: "HTMLElement", Tags: []string{"li"}, Props: []reflectProp{{Prop: "value", Kind: rkLong}}},
	{Name: "HTMLLabelElement", Parent: "HTMLElement", Tags: []string{"label"}, Props: str("htmlFor=for")},
	{Name: "HTMLLegendElement", Parent: "HTMLElement", Tags: []string{"legend"}},
	{Name: "HTMLLinkElement", Parent: "HTMLElement", Tags: []string{"link"}, Props: cat(str("href", "rel", "type", "media", "as"), one(crossOrigin), one(referrerPolicy))},
	{Name: "HTMLMapElement", Parent: "HTMLElement", Tags: []string{"map"}, Props: str("name")},
	{Name: "HTMLMarqueeElement", Parent: "HTMLElement", Tags: []string{"marquee"}, Props: cat(str("behavior", "bgColor=bgcolor", "direction", "height", "hspace", "loop", "scrollAmount=scrollamount", "scrollDelay=scrolldelay", "vspace", "width"), boolp("trueSpeed=truespeed"))},
	{Name: "HTMLMenuElement", Parent: "HTMLElement", Tags: []string{"menu"}},
	{Name: "HTMLMetaElement", Parent: "HTMLElement", Tags: []string{"meta"}, Props: str("name", "content", "httpEquiv=http-equiv", "charset")},
	{Name: "HTMLMeterElement", Parent: "HTMLElement", Tags: []string{"meter"}, Props: []reflectProp{{Prop: "min", Kind: rkBespoke}, {Prop: "max", Kind: rkBespoke}, {Prop: "value", Kind: rkBespoke}, {Prop: "low", Kind: rkBespoke}, {Prop: "high", Kind: rkBespoke}, {Prop: "optimum", Kind: rkBespoke}}},
	{Name: "HTMLModElement", Parent: "HTMLElement", Tags: []string{"ins", "del"}, Props: str("cite", "dateTime=datetime")},
	{Name: "HTMLOListElement", Parent: "HTMLElement", Tags: []string{"ol"}, Props: cat(boolp("reversed"), []reflectProp{{Prop: "start", Kind: rkLong, Def: 1}}, str("type"))},
	{Name: "HTMLObjectElement", Parent: "HTMLElement", Tags: []string{"object"}, Props: str("data", "type", "name", "width", "height")},
	{Name: "HTMLOptGroupElement", Parent: "HTMLElement", Tags: []string{"optgroup"}, Props: cat(boolp("disabled"), str("label"))},
	{Name: "HTMLOptionElement", Parent: "HTMLElement", Tags: []string{"option"}, Props: cat(str("value", "label"), boolp("selected", "disabled"))},
	{Name: "HTMLOutputElement", Parent: "HTMLElement", Tags: []string{"output"}, Props: str("name")},
	{Name: "HTMLParagraphElement", Parent: "HTMLElement", Tags: []string{"p"}},
	{Name: "HTMLParamElement", Parent: "HTMLElement", Tags: []string{"param"}, Props: str("name", "value")},
	{Name: "HTMLPictureElement", Parent: "HTMLElement", Tags: []string{"picture"}},
	{Name: "HTMLPreElement", Parent: "HTMLElement", Tags: []string{"pre"}},
	{Name: "HTMLProgressElement", Parent: "HTMLElement", Tags: []string{"progress"}, Props: []reflectProp{{Prop: "max", Kind: rkBespoke}, {Prop: "value", Kind: rkBespoke}}},
	{Name: "HTMLQuoteElement", Parent: "HTMLElement", Tags: []string{"blockquote", "q"}, Props: str("cite")},
	{Name: "HTMLScriptElement", Parent: "HTMLElement", Tags: []string{"script"}, Props: cat(str("src", "type"), boolp("defer", "async", "noModule=nomodule"), one(crossOrigin), one(referrerPolicy))},
	{Name: "HTMLSelectElement", Parent: "HTMLElement", Tags: []string{"select"}, Props: cat(str("name"), boolp("required", "disabled", "multiple"))},
	{Name: "HTMLSlotElement", Parent: "HTMLElement", Tags: []string{"slot"}, Props: str("name")},
	{Name: "HTMLSourceElement", Parent: "HTMLElement", Tags: []string{"source"}, Props: str("src", "type", "srcset", "sizes", "media")},
	{Name: "HTMLSpanElement", Parent: "HTMLElement", Tags: []string{"span"}},
	{Name: "HTMLStyleElement", Parent: "HTMLElement", Tags: []string{"style"}, Props: str("media", "type")},
	{Name: "HTMLTableCaptionElement", Parent: "HTMLElement", Tags: []string{"caption"}},
	{Name: "HTMLTableCellElement", Parent: "HTMLElement", Tags: []string{"td", "th"}, Props: cat([]reflectProp{
		{Prop: "colSpan", Attr: "colspan", Kind: rkClamped, Def: 1, Min: 1, Max: 1000},
		{Prop: "rowSpan", Attr: "rowspan", Kind: rkClamped, Def: 1, Min: 0, Max: 65534}},
		str("headers"), one(enum("scope", "", []string{"row", "col", "rowgroup", "colgroup"}, "", "")), str("abbr"))},
	{Name: "HTMLTableColElement", Parent: "HTMLElement", Tags: []string{"col", "colgroup"}, Props: []reflectProp{{Prop: "span", Kind: rkClamped, Def: 1, Min: 1, Max: 1000}}},
	{Name: "HTMLTableElement", Parent: "HTMLElement", Tags: []string{"table"}},
	{Name: "HTMLTableRowElement", Parent: "HTMLElement", Tags: []string{"tr"}},
	{Name: "HTMLTableSectionElement", Parent: "HTMLElement", Tags: []string{"thead", "tbody", "tfoot"}},
	{Name: "HTMLTemplateElement", Parent: "HTMLElement", Tags: []string{"template"}},
	{Name: "HTMLTextAreaElement", Parent: "HTMLElement", Tags: []string{"textarea"}, Props: cat(str("name", "placeholder"), boolp("required", "disabled", "readOnly=readonly"),
		[]reflectProp{{Prop: "rows", Kind: rkLimited, Def: 2}, {Prop: "cols", Kind: rkLimited, Def: 20}},
		one(enum("wrap", "", []string{"soft", "hard"}, "soft", "soft")), str("value"))},
	{Name: "HTMLTimeElement", Parent: "HTMLElement", Tags: []string{"time"}, Props: str("dateTime=datetime")},
	{Name: "HTMLTitleElement", Parent: "HTMLElement", Tags: []string{"title"}},
	{Name: "HTMLTrackElement", Parent: "HTMLElement", Tags: []string{"track"}, Props: cat(str("src"),
		one(enum("kind", "", []string{"subtitles", "captions", "descriptions", "chapters", "metadata"}, "subtitles", "metadata")), str("srclang", "label"), boolp("default"))},
	{Name: "HTMLUListElement", Parent: "HTMLElement", Tags: []string{"ul"}},
	{Name: "HTMLVideoElement", Parent: "HTMLMediaElement", Tags: []string{"video"}, Props: cat([]reflectProp{{Prop: "width", Kind: rkULong}, {Prop: "height", Kind: rkULong}}, str("poster"), boolp("playsInline=playsinline"))},
}

// ifaceByTag maps a lower-case tag to its interface index in ifaceTable.
var ifaceByTag = func() map[string]int {
	m := map[string]int{}
	for i, d := range ifaceTable {
		for _, t := range d.Tags {
			m[t] = i
		}
	}
	return m
}()

// ifaceJSON is the table as the prelude reads it.
var ifaceJSON = func() string {
	b, err := json.Marshal(ifaceTable)
	if err != nil {
		panic(err)
	}
	return string(b)
}()

// domExceptionCodes are the legacy codes of R-JS-76.
var domExceptionCodes = []string{"IndexSizeError", "DOMStringSizeError", "HierarchyRequestError", "WrongDocumentError",
	"InvalidCharacterError", "NoDataAllowedError", "NoModificationAllowedError", "NotFoundError", "NotSupportedError",
	"InUseAttributeError", "InvalidStateError", "SyntaxError", "InvalidModificationError", "NamespaceError",
	"InvalidAccessError", "ValidationError", "TypeMismatchError", "SecurityError", "NetworkError", "AbortError",
	"URLMismatchError", "QuotaExceededError", "TimeoutError", "InvalidNodeTypeError", "DataCloneError"}
