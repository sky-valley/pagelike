"use strict";
// pagelike server DOM (internal/jsrt): the node classes of
// docs/spec/javascript.md A10 over the __pl_dom host function. Loaded as
// separate bytecode the first time a binding touches the DOM; the core
// prelude hands it its kernel. /*IFACES*/ is replaced with ifaces.go data.
__pagelike.kernel.setDOMBuilder((K) => {
  const { DOM, D1, D2, D3, D4, DN, OP, KEY, DOMException, TypeErrorFn, InternalErrorFn, ObjDefine, ObjCreate, StringFn, NumberFn, NumberIsFinite, MathTrunc, MathMax, MathMin, arrayPush, strLower, wmGet, wmSet, call } = K;
  const IFACES = /*IFACES*/[];
  const cache = [];
  let hOf; // node → handle, -1 for anything else
  const elementClasses = []; // interface index → class (created on demand)
  const otherClasses = [];  // -kind → class

  const wrap = (h) => {
    if (h === null || h === undefined || h < 0) return null;
    const w = cache[h];
    if (w !== undefined) return w;
    const k = D1(OP.kind, h);
    const C = k >= 0 ? iface(k) : otherClasses[-k];
    const n = new C(KEY, h);
    cache[h] = n;
    return n;
  };
  const need = (n, what) => {
    const h = hOf(n);
    if (h < 0) throw new TypeErrorFn("Failed to execute '" + what + "': parameter is not of type 'Node'.");
    return h;
  };
  const nsv = (ns) => (ns === null || ns === undefined ? null : StringFn(ns));
  const strOrEmpty = (v) => (v === null || v === undefined ? "" : StringFn(v));

  class Node {
    #h;
    constructor(key, h) {
      if (key !== KEY) throw new TypeErrorFn("Illegal constructor");
      this.#h = h;
    }
    static {
      hOf = (n) => (n !== null && typeof n === "object" && #h in n ? n.#h : -1);
    }
    get nodeType() { return D1(OP.nodeType, this.#h); }
    get nodeName() { return D1(OP.nodeName, this.#h); }
    get parentNode() { return wrap(D1(OP.parent, this.#h)); }
    get parentElement() { return wrap(D1(OP.parentEl, this.#h)); }
    get firstChild() { return wrap(D1(OP.first, this.#h)); }
    get lastChild() { return wrap(D1(OP.last, this.#h)); }
    get previousSibling() { return wrap(D1(OP.prev, this.#h)); }
    get nextSibling() { return wrap(D1(OP.next, this.#h)); }
    get childNodes() { return makeNodeList(D1(OP.childNodes, this.#h)); }
    get ownerDocument() { return wrap(D1(OP.ownerDoc, this.#h)); }
    hasChildNodes() { return D1(OP.hasChildren, this.#h); }
    get textContent() { return D1(OP.text, this.#h); }
    set textContent(v) { D2(OP.setText, this.#h, strOrEmpty(v)); }
    appendChild(c) { D2(OP.append, this.#h, need(c, "appendChild")); return c; }
    insertBefore(n, ref) {
      const hn = need(n, "insertBefore");
      D3(OP.insertBefore, this.#h, hn, ref === null || ref === undefined ? null : need(ref, "insertBefore"));
      return n;
    }
    removeChild(c) { D2(OP.removeChild, this.#h, need(c, "removeChild")); return c; }
    replaceChild(n, o) { D3(OP.replaceChild, this.#h, need(n, "replaceChild"), need(o, "replaceChild")); return o; }
    cloneNode(deep = false) { return wrap(D2(OP.clone, this.#h, !!deep)); }
    contains(o) {
      if (o === null || o === undefined) return false;
      return D2(OP.contains, this.#h, need(o, "contains"));
    }
  }
  for (const [k, v] of [["ELEMENT_NODE", 1], ["TEXT_NODE", 3], ["COMMENT_NODE", 8], ["DOCUMENT_NODE", 9], ["DOCUMENT_TYPE_NODE", 10], ["DOCUMENT_FRAGMENT_NODE", 11]]) {
    ObjDefine(Node, k, { value: v, enumerable: true });
  }

  class Document extends Node {
    querySelector(s) { return wrap(D2(OP.qs, hOf(this), StringFn(s))); }
    querySelectorAll(s) { return makeNodeList(D2(OP.qsa, hOf(this), StringFn(s))); }
    getElementById(id) { return wrap(D2(OP.byId, hOf(this), StringFn(id))); }
    getElementsByTagNameNS(ns, local) { return makeNodeList(D3(OP.byTagNS, hOf(this), ns === "*" ? "*" : nsv(ns), StringFn(local))); }
    createElement(tag) { return wrap(D2(OP.createElement, hOf(this), StringFn(tag))); }
    createElementNS(ns, qname) { return wrap(D3(OP.createElementNS, hOf(this), nsv(ns), StringFn(qname))); }
    createTextNode(data) { return wrap(D2(OP.createText, hOf(this), StringFn(data))); }
    createComment(data) { return wrap(D2(OP.createComment, hOf(this), StringFn(data))); }
    createDocumentFragment() { return wrap(D1(OP.createFragment, hOf(this))); }
    get documentElement() { return wrap(D1(OP.docElement, hOf(this))); }
    get head() { return wrap(D1(OP.head, hOf(this))); }
    get body() { return wrap(D1(OP.body, hOf(this))); }
  }
  class DocumentFragment extends Node {}
  class DocumentType extends Node {}
  class ProcessingInstruction extends Node {}
  class CharacterData extends Node {
    get data() { return D1(OP.data, hOf(this)); }
    set data(v) { D2(OP.setData, hOf(this), strOrEmpty(v)); }
    get length() { return D1(OP.length, hOf(this)); }
  }
  class Text extends CharacterData {}
  class Comment extends CharacterData {}

  const toHandles = (self, args) => {
    const out = [];
    let doc = -1;
    for (let i = 0; i < args.length; i++) {
      const x = args[i];
      const h = hOf(x);
      if (h >= 0) { arrayPush(out, h); continue; }
      if (doc < 0) doc = D1(OP.ownerDoc, hOf(self));
      arrayPush(out, D2(OP.createText, doc, StringFn(x)));
    }
    return out;
  };
  const classLists = new WeakMap();

  class Element extends Node {
    get tagName() { return D1(OP.tag, hOf(this)); }
    get localName() { return D1(OP.localName, hOf(this)); }
    get prefix() { return D1(OP.prefix, hOf(this)); }
    get namespaceURI() { return D1(OP.nsURI, hOf(this)); }
    get id() { return D2(OP.getAttr, hOf(this), "id") ?? ""; }
    get className() { return D2(OP.getAttr, hOf(this), "class") ?? ""; }
    getAttribute(n) { return D2(OP.getAttr, hOf(this), StringFn(n)); }
    hasAttribute(n) { return D2(OP.hasAttr, hOf(this), StringFn(n)); }
    setAttribute(n, v) { D3(OP.setAttr, hOf(this), StringFn(n), StringFn(v)); }
    removeAttribute(n) { D2(OP.removeAttr, hOf(this), StringFn(n)); }
    getAttributeNS(ns, local) { return D3(OP.getAttrNS, hOf(this), nsv(ns), StringFn(local)); }
    setAttributeNS(ns, qname, v) { D4(OP.setAttrNS, hOf(this), nsv(ns), StringFn(qname), StringFn(v)); }
    removeAttributeNS(ns, local) { D3(OP.removeAttrNS, hOf(this), nsv(ns), StringFn(local)); }
    get innerHTML() { return D1(OP.innerHTML, hOf(this)); }
    set innerHTML(v) { D2(OP.setInnerHTML, hOf(this), strOrEmpty(v)); }
    get outerHTML() { return D1(OP.outerHTML, hOf(this)); }
    set outerHTML(v) { D2(OP.setOuterHTML, hOf(this), strOrEmpty(v)); }
    insertAdjacentHTML(pos, html) { D3(OP.insertAdjacent, hOf(this), StringFn(pos), StringFn(html)); }
    get classList() {
      let l = wmGet(classLists, this);
      if (!l) { l = new DOMTokenList(KEY, this); wmSet(classLists, this, l); }
      return l;
    }
    get children() { return makeNodeList(D1(OP.children, hOf(this))); }
    get firstElementChild() { return wrap(D1(OP.firstEl, hOf(this))); }
    get lastElementChild() { return wrap(D1(OP.lastEl, hOf(this))); }
    get nextElementSibling() { return wrap(D1(OP.nextEl, hOf(this))); }
    get previousElementSibling() { return wrap(D1(OP.prevEl, hOf(this))); }
    get childElementCount() { return D1(OP.elCount, hOf(this)); }
    querySelector(s) { return wrap(D2(OP.qs, hOf(this), StringFn(s))); }
    querySelectorAll(s) { return makeNodeList(D2(OP.qsa, hOf(this), StringFn(s))); }
    getElementsByTagNameNS(ns, local) { return makeNodeList(D3(OP.byTagNS, hOf(this), ns === "*" ? "*" : nsv(ns), StringFn(local))); }
    getElementById(id) { return wrap(D2(OP.byId, hOf(this), StringFn(id))); }
    closest(s) { return wrap(D2(OP.closest, hOf(this), StringFn(s))); }
    matches(s) { return D2(OP.matches, hOf(this), StringFn(s)); }
    append(...nodes) { DN(OP.insertNodes, hOf(this), "append", toHandles(this, nodes)); }
    prepend(...nodes) { DN(OP.insertNodes, hOf(this), "prepend", toHandles(this, nodes)); }
    before(...nodes) { if (D1(OP.parent, hOf(this)) < 0) return; DN(OP.insertNodes, hOf(this), "before", toHandles(this, nodes)); }
    after(...nodes) { if (D1(OP.parent, hOf(this)) < 0) return; DN(OP.insertNodes, hOf(this), "after", toHandles(this, nodes)); }
    replaceWith(...nodes) { if (D1(OP.parent, hOf(this)) < 0) return; DN(OP.insertNodes, hOf(this), "replaceWith", toHandles(this, nodes)); }
    remove() { D1(OP.remove, hOf(this)); }
  }

  // ---------------------------------------------------------------- reflection (R-JS-78/79)
  const WS = " \t\n\f\r";
  const parseIntW = (s) => {
    if (s === null) return null;
    let i = 0;
    while (i < s.length && WS.includes(s[i])) i++;
    let sign = 1;
    if (s[i] === "-") { sign = -1; i++; } else if (s[i] === "+") i++;
    const start = i;
    while (i < s.length && s[i] >= "0" && s[i] <= "9") i++;
    if (i === start) return null;
    return sign * NumberFn(s.slice(start, i));
  };
  const parseNonNeg = (s) => { const v = parseIntW(s); return v === null || v < 0 ? null : v; };
  const FLOAT = /^[\t\n\f\r ]*(-?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)/;
  const parseFloatW = (s) => {
    if (s === null) return null;
    const m = FLOAT.exec(s);
    if (!m) return null;
    const v = NumberFn(m[1]);
    return NumberIsFinite(v) ? v : null;
  };
  const getA = (el, a) => D2(OP.getAttr, hOf(el), a);
  const setA = (el, a, v) => D3(OP.setAttr, hOf(el), a, v);
  const remA = (el, a) => D2(OP.removeAttr, hOf(el), a);
  const clamp = (v, lo, hi) => MathMin(MathMax(v, lo), hi);

  const reflectAccessors = (p) => {
    const attr = p.attr || strLower(p.prop);
    const def = p.def || 0;
    const setString = function (v) { setA(this, attr, StringFn(v)); };
    const setNumber = function (v) { setA(this, attr, StringFn(NumberFn(v))); };
    switch (p.kind) {
      case "string":
        return [function () { return getA(this, attr) ?? ""; }, setString];
      case "bool":
        return [function () { return D2(OP.hasAttr, hOf(this), attr); },
          function (v) { if (v === false || v === null || v === undefined) remA(this, attr); else setA(this, attr, ""); }];
      case "long":
        return [function () { const v = parseIntW(getA(this, attr)); return v === null ? def : v; }, setNumber];
      case "ulong":
        return [function () { const v = parseNonNeg(getA(this, attr)); return v === null ? def : v; }, setNumber];
      case "clamped":
        return [function () { const v = parseNonNeg(getA(this, attr)); return v === null ? def : clamp(v, p.min || 0, p.max); }, setNumber];
      case "limited":
        return [function () { const v = parseNonNeg(getA(this, attr)); return v === null || v <= 0 ? def : v; }, setNumber];
      case "enum": {
        const keys = p.keys || [];
        return [function () {
          const raw = getA(this, attr);
          if (raw === null) return p.missing ?? "";
          const l = strLower(raw);
          if (keys.includes(l)) return (p.alias && p.alias[l]) ?? l;
          return p.invalid ?? "";
        }, setString];
      }
      case "nullenum":
        return [function () {
          const raw = getA(this, attr);
          if (raw === null) return null;
          return strLower(raw) === "use-credentials" ? "use-credentials" : "anonymous";
        }, function (v) { if (v === null) remA(this, attr); else setA(this, attr, StringFn(v)); }];
    }
    return bespoke(p);
  };

  const editState = (el) => {
    const raw = getA(el, "contenteditable");
    if (raw === null) return "inherit";
    const l = strLower(raw);
    if (l === "" || l === "true") return "true";
    if (l === "false" || l === "plaintext-only") return l;
    return "inherit";
  };
  const meter = (el) => {
    const min = parseFloatW(getA(el, "min")) ?? 0;
    let max = parseFloatW(getA(el, "max")) ?? 1;
    max = MathMax(max, min);
    const value = clamp(parseFloatW(getA(el, "value")) ?? 0, min, max);
    const low = clamp(parseFloatW(getA(el, "low")) ?? min, min, max);
    const high = clamp(parseFloatW(getA(el, "high")) ?? max, low, max);
    const optimum = clamp(parseFloatW(getA(el, "optimum")) ?? (min + max) / 2, min, max);
    return { min, max, value, low, high, optimum };
  };
  const progress = (el) => {
    let max = parseFloatW(getA(el, "max"));
    if (max === null || max <= 0) max = 1;
    let value = parseFloatW(getA(el, "value"));
    if (value === null || value < 0) value = 0;
    return { max, value: MathMin(value, max) };
  };
  const bespoke = (p) => {
    const setNumber = function (v) { setA(this, strLower(p.prop), StringFn(NumberFn(v))); };
    switch (p.prop) {
      case "contentEditable":
        return [function () { return editState(this); }, function (v) {
          const l = strLower(StringFn(v));
          if (l === "inherit") remA(this, "contenteditable");
          else if (l === "true" || l === "false" || l === "plaintext-only") setA(this, "contenteditable", l);
          else throw new DOMException("'" + StringFn(v) + "' is not a valid contentEditable value", "SyntaxError");
        }];
      case "isContentEditable":
        return [function () {
          for (let e = this; e !== null; e = e.parentElement) {
            if (!(e instanceof HTMLElement)) continue;
            const s = editState(e);
            if (s === "true" || s === "plaintext-only") return true;
            if (s === "false") return false;
          }
          return false;
        }, undefined];
      case "min": case "max": case "value": case "low": case "high": case "optimum":
        return [function () { return this instanceof HTMLProgressElementRef.C ? progress(this)[p.prop] : meter(this)[p.prop]; }, setNumber];
    }
    return [undefined, undefined];
  };

  class HTMLElement extends Element {}

  const byName = ObjCreate(null);
  byName.Node = Node;
  byName.Element = Element;
  byName.HTMLElement = HTMLElement;
  const ifaceIndex = ObjCreate(null);
  for (let i = 0; i < IFACES.length; i++) ifaceIndex[IFACES[i].name] = i;
  // iface creates interface i (and its parents) with its reflected
  // properties the first time it is needed.
  const iface = (i) => {
    let C = elementClasses[i];
    if (C !== undefined) return C;
    const def = IFACES[i];
    C = byName[def.name];
    if (!C) {
      const Parent = byName[def.parent] || iface(ifaceIndex[def.parent]);
      C = ({ [def.name]: class extends Parent {} })[def.name];
      byName[def.name] = C;
    }
    for (const p of def.props || []) {
      const [get, set] = reflectAccessors(p);
      ObjDefine(C.prototype, p.prop, { get, set: p.ro ? undefined : set, configurable: true, enumerable: true });
    }
    elementClasses[i] = C;
    return C;
  };
  const classNamed = (name) => byName[name] || iface(ifaceIndex[name]);
  const HTMLProgressElementRef = { get C() { return classNamed("HTMLProgressElement"); } };
  otherClasses[3] = Text;
  otherClasses[8] = Comment;
  otherClasses[9] = Document;
  otherClasses[10] = DocumentType;
  otherClasses[11] = DocumentFragment;
  otherClasses[7] = ProcessingInstruction;

  // ---------------------------------------------------------------- NodeList (R-JS-72)
  let nlHandles;
  class NodeList {
    #hs;
    constructor(key, hs) {
      if (key !== KEY) throw new TypeErrorFn("Illegal constructor");
      this.#hs = hs;
      for (let i = 0; i < hs.length; i++) {
        ObjDefine(this, i, { get: () => wrap(hs[i]), enumerable: true });
      }
    }
    static { nlHandles = (v) => (v !== null && typeof v === "object" && #hs in v ? v.#hs : null); }
    get length() { return this.#hs.length; }
    item(i) {
      const n = MathTrunc(NumberFn(i));
      if (!(n >= 0 && n < this.#hs.length)) return null;
      return wrap(this.#hs[n]);
    }
    forEach(cb, thisArg) {
      const hs = this.#hs;
      for (let i = 0; i < hs.length; i++) call.call(cb, thisArg, wrap(hs[i]), i, this);
    }
    [Symbol.iterator]() {
      const hs = this.#hs;
      let i = 0;
      return { next: () => (i < hs.length ? { value: wrap(hs[i++]), done: false } : { value: undefined, done: true }), [Symbol.iterator]() { return this; } };
    }
  }
  const makeNodeList = (hs) => new NodeList(KEY, hs);

  // ---------------------------------------------------------------- DOMTokenList (R-JS-70)
  // Live 2026-09-29: PageLove's token list is the class attribute split on
  // ASCII whitespace, duplicates kept (length, item and iteration count
  // them); tokens are not validated; item() out of range is undefined and
  // an index that is not a number is a TypeError.
  const splitTokens = (v) => {
    const out = [];
    if (v === null) return out;
    let cur = "";
    for (let i = 0; i <= v.length; i++) {
      const c = i < v.length ? v[i] : " ";
      if (WS.includes(c)) {
        if (cur !== "") arrayPush(out, cur);
        cur = "";
      } else cur += c;
    }
    return out;
  };
  class DOMTokenList {
    #el;
    constructor(key, el) {
      if (key !== KEY) throw new TypeErrorFn("Illegal constructor");
      this.#el = el;
    }
    #tokens() { return splitTokens(getA(this.#el, "class")); }
    #write(ts) { if (ts.length === 0) remA(this.#el, "class"); else setA(this.#el, "class", ts.join(" ")); }
    get length() { return this.#tokens().length; }
    get value() { return getA(this.#el, "class") ?? ""; }
    set value(v) { setA(this.#el, "class", StringFn(v)); }
    item(i) {
      if (typeof i !== "number") throw new TypeErrorFn("DOMTokenList.item: the index must be a number");
      const ts = this.#tokens();
      const n = MathTrunc(i);
      return n >= 0 && n < ts.length ? ts[n] : undefined;
    }
    contains(t) { return this.#tokens().includes(StringFn(t)); }
    add(...ts) {
      const cur = this.#tokens();
      for (const t of ts.map(StringFn)) if (!cur.includes(t)) arrayPush(cur, t);
      this.#write(cur);
    }
    // remove drops every occurrence of each token.
    remove(...ts) {
      const rm = ts.map(StringFn);
      this.#write(this.#tokens().filter((t) => !rm.includes(t)));
    }
    toggle(t, force) {
      t = StringFn(t);
      const f = force === undefined ? undefined : !!force;
      const cur = this.#tokens();
      const has = cur.includes(t);
      if (has && f !== true) { this.#write(cur.filter((x) => x !== t)); return false; }
      if (!has && f !== false) { arrayPush(cur, t); this.#write(cur); return true; }
      return has;
    }
    // replace puts n at the first occurrence of o or n and drops the other
    // occurrences of n; other occurrences of o stay (live 2026-09-29:
    // "a b a".replace("a", "b") → "b a").
    replace(o, n) {
      o = StringFn(o);
      n = StringFn(n);
      const cur = this.#tokens();
      if (!cur.includes(o)) return false;
      let i = 0;
      while (cur[i] !== o && cur[i] !== n) i++;
      cur[i] = n;
      this.#write(cur.filter((x, j) => x !== n || j === i));
      return true;
    }
    toString() { return this.value; }
    [Symbol.iterator]() { return this.#tokens()[Symbol.iterator](); }
  }

  // ---------------------------------------------------------------- DOMParser, XMLSerializer (R-JS-74/75)
  class DOMParser {
    parseFromString(markup, type) { return wrap(D1(OP.parse, StringFn(markup))); }
  }
  class XMLSerializer {
    serializeToString(node) { return D1(OP.serialize, need(node, "serializeToString")); }
  }
  for (const [k, v] of [["Document", Document], ["DocumentFragment", DocumentFragment], ["CharacterData", CharacterData],
    ["Text", Text], ["Comment", Comment], ["NodeList", NodeList], ["DOMTokenList", DOMTokenList], ["DOMParser", DOMParser],
    ["XMLSerializer", XMLSerializer]]) byName[k] = v;
  return { wrap, hOf, nlHandles, classNamed, Element, Text, Comment, Document, DocumentFragment };
});
