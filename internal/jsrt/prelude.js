"use strict";
// pagelike server JavaScript prelude (internal/jsrt). Evaluated once per
// fresh context before the binding's module. It builds the WHATWG-shaped
// server DOM (docs/spec/javascript.md A10) as classes over the __pl_dom host
// function, the platform globals (DOMException, DOMParser, XMLSerializer,
// crypto, TextEncoder/TextDecoder), the schema-class runtime (A11), and the
// driver the worker calls to load, call and marshal a binding. Everything
// internal lives in the global lexical binding __pagelike, which is not a
// property of globalThis. /*IFACES*/ and /*DOMEX*/ are replaced with data
// from ifaces.go.
const __pagelike = (() => {
  const G = globalThis;
  const DOM = G.__pl_dom;
  const SYS = G.__pl_sys;
  delete G.__pl_dom;
  delete G.__pl_sys;
  const IFACENAMES = /*IFACENAMES*/[];
  const DOMEX = /*DOMEX*/[];

  // Primordials: the driver runs after user code, which may have patched
  // built-ins.
  const ObjDefine = Object.defineProperty;
  const ObjCreate = Object.create;
  const ObjKeys = Object.keys;
  const ObjHasOwn = Object.hasOwn;
  const ArrayIsArray = Array.isArray;
  const JSONparse = JSON.parse;
  const JSONstringify = JSON.stringify;
  const StringFn = String;
  const NumberFn = Number;
  const NumberIsFinite = Number.isFinite;
  const NumberIsInteger = Number.isInteger;
  const MathTrunc = Math.trunc;
  const MathMax = Math.max;
  const MathMin = Math.min;
  const call = Function.prototype.call;
  const uncurry = (f) => call.bind(f);
  const wmGet = uncurry(WeakMap.prototype.get);
  const wmSet = uncurry(WeakMap.prototype.set);
  const wmHas = uncurry(WeakMap.prototype.has);
  const mapGet = uncurry(Map.prototype.get);
  const mapSet = uncurry(Map.prototype.set);
  const mapHas = uncurry(Map.prototype.has);
  const mapEntries = uncurry(Map.prototype.entries);
  const setValues = uncurry(Set.prototype.values);
  const arrayPush = uncurry(Array.prototype.push);
  const strLower = uncurry(String.prototype.toLowerCase);
  const ReflectApply = Reflect.apply;
  const ReflectGet = Reflect.get;
  const ReflectSet = Reflect.set;
  const ReflectHas = Reflect.has;
  const ReflectOwnKeys = Reflect.ownKeys;
  const ReflectGOPD = Reflect.getOwnPropertyDescriptor;
  const ReflectDefine = Reflect.defineProperty;
  const ReflectDelete = Reflect.deleteProperty;
  const ProxyFn = Proxy;
  const MapFn = Map;
  const SetFn = Set;
  const ErrorFn = Error;
  const TypeErrorFn = TypeError;
  const InternalErrorFn = typeof InternalError === "function" ? InternalError : Error;
  const PromiseResolve = Promise.resolve.bind(Promise);
  const PromiseReject = Promise.reject.bind(Promise);

  const ERR = "\u0000";
  const KEY = ObjCreate(null); // proves a construction comes from the prelude

  // ---------------------------------------------------------------- DOMException (R-JS-76)
  const CODES = ObjCreate(null);
  for (let i = 0; i < DOMEX.length; i++) CODES[DOMEX[i]] = i + 1;
  class DOMException extends ErrorFn {
    #name;
    constructor(message = "", name = "Error") {
      super(StringFn(message));
      this.#name = StringFn(name);
    }
    get name() { return this.#name; }
    get code() { return CODES[this.#name] ?? 0; }
    toString() {
      return this.message === "" ? "DOMException: " + this.#name : "DOMException: " + this.#name + ": " + this.message;
    }
  }

  const lastError = () => {
    const [name, msg] = DOM(0);
    if (name === "TypeError") return new TypeErrorFn(msg);
    if (name === "InternalError") return new InternalErrorFn(msg);
    return new DOMException(msg, name);
  };
  const D1 = (op, h) => { const r = DOM(op, h); if (r === ERR) throw lastError(); return r; };
  const D2 = (op, h, a) => { const r = DOM(op, h, a); if (r === ERR) throw lastError(); return r; };
  const D3 = (op, h, a, b) => { const r = DOM(op, h, a, b); if (r === ERR) throw lastError(); return r; };
  const D4 = (op, h, a, b, c) => { const r = DOM(op, h, a, b, c); if (r === ERR) throw lastError(); return r; };
  const DN = (op, h, mode, hs) => { const r = DOM(op, h, mode, ...hs); if (r === ERR) throw lastError(); return r; };

  // Operation codes (dom.go).
  const OP = {
    kind: 1, parent: 2, first: 3, last: 4, prev: 5, next: 6, childNodes: 7, children: 8, nodeName: 9,
    text: 10, setText: 11, data: 12, setData: 13, length: 14, ownerDoc: 15, append: 16, insertBefore: 17,
    removeChild: 18, replaceChild: 19, clone: 20, contains: 21, tag: 22, localName: 23, prefix: 24, nsURI: 25,
    getAttr: 26, hasAttr: 27, setAttr: 28, removeAttr: 29, getAttrNS: 30, setAttrNS: 31, removeAttrNS: 32,
    innerHTML: 33, setInnerHTML: 34, outerHTML: 35, setOuterHTML: 36, insertAdjacent: 37, qs: 38, qsa: 39,
    byId: 40, byTagNS: 41, closest: 42, matches: 43, createElement: 44, createElementNS: 45, createText: 46,
    createComment: 47, createFragment: 48, docElement: 49, head: 50, body: 51, parse: 52, serialize: 53,
    insertNodes: 54, remove: 55, firstEl: 56, lastEl: 57, nextEl: 58, prevEl: 59, elCount: 60, nodeType: 61,
    hasChildren: 62, parentEl: 63,
  };

  // ---------------------------------------------------------------- nodes (R-JS-64..73)
  // The DOM is built on first use: most bindings never touch it, and a fresh
  // context per evaluation would otherwise pay for ~90 classes every time.
  let D = null;
  const dom = () => D || (D = buildDOM());
  let domBuilder = null;
  let domLoading = false;
  // buildDOM asks the worker to evaluate the DOM chunk, which registers
  // its builder through the kernel (visible only while loading).
  const buildDOM = () => {
    domLoading = true;
    try { sys("dom"); } finally { domLoading = false; }
    if (domBuilder === null) throw new InternalErrorFn("the server DOM failed to load");
    return domBuilder({ DOM, D1, D2, D3, D4, DN, OP, KEY, DOMException, TypeErrorFn, InternalErrorFn, ObjDefine, ObjCreate, StringFn, NumberFn, NumberIsFinite, MathTrunc, MathMax, MathMin, arrayPush, strLower, wmGet, wmSet, call });
  };

  // ---------------------------------------------------------------- crypto, TextEncoder/Decoder (R-JS-39a)
  const bytesOf = (data) => {
    if (data instanceof ArrayBuffer) return new Uint8Array(data);
    if (ArrayBuffer.isView(data)) return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
    throw new TypeErrorFn("expected an ArrayBuffer or ArrayBufferView");
  };
  const latin1 = (u8) => {
    let s = "";
    for (let i = 0; i < u8.length; i += 8192) s += StringFn.fromCharCode.apply(null, u8.subarray(i, i + 8192));
    return s;
  };
  const fromLatin1 = (s) => {
    const u8 = new Uint8Array(s.length);
    for (let i = 0; i < s.length; i++) u8[i] = s.charCodeAt(i);
    return u8;
  };
  const fromHex = (hex) => {
    const u8 = new Uint8Array(hex.length / 2);
    for (let i = 0; i < u8.length; i++) u8[i] = parseInt(hex.substr(2 * i, 2), 16);
    return u8;
  };
  const sys = (...a) => {
    const r = SYS(...a);
    if (r === ERR) throw new ErrorFn(SYS("err"));
    return r;
  };
  const subtle = {
    digest(alg, data) {
      try {
        let name = typeof alg === "string" ? alg : alg && alg.name;
        name = StringFn(name).toUpperCase();
        if (name !== "SHA-1" && name !== "SHA-256" && name !== "SHA-384" && name !== "SHA-512") {
          return PromiseReject(new DOMException("Unrecognized algorithm name", "NotSupportedError"));
        }
        return PromiseResolve(fromHex(sys("digest", name, latin1(bytesOf(data)))).buffer);
      } catch (e) {
        return PromiseReject(e);
      }
    },
  };
  const crypto = {
    subtle,
    getRandomValues(ta) {
      if (!ArrayBuffer.isView(ta) || ta instanceof Float32Array || ta instanceof Float64Array || ta instanceof DataView) {
        throw new DOMException("expected an integer typed array", "TypeMismatchError");
      }
      if (ta.byteLength > 65536) throw new DOMException("at most 65536 bytes", "QuotaExceededError");
      new Uint8Array(ta.buffer, ta.byteOffset, ta.byteLength).set(fromHex(sys("random", ta.byteLength)));
      return ta;
    },
    randomUUID() { return sys("uuid"); },
  };
  class TextEncoder {
    get encoding() { return "utf-8"; }
    encode(s = "") {
      let str = StringFn(s);
      if (typeof str.toWellFormed === "function") str = str.toWellFormed();
      return fromLatin1(unescape(encodeURIComponent(str)));
    }
  }
  class TextDecoder {
    #fatal;
    constructor(label = "utf-8", options = undefined) {
      const l = strLower(StringFn(label)).trim();
      if (l !== "utf-8" && l !== "utf8" && l !== "unicode-1-1-utf-8") throw new RangeError("TextDecoder: only utf-8 is supported");
      this.#fatal = !!(options && options.fatal);
    }
    get encoding() { return "utf-8"; }
    get fatal() { return this.#fatal; }
    decode(input = new Uint8Array(0)) { return sys("utf8", latin1(bytesOf(input)), this.#fatal); }
  }

  // ---------------------------------------------------------------- globals (R-JS-38/39)
  const defineGlobal = (name, value) => ObjDefine(G, name, { value, writable: true, configurable: true, enumerable: false });
  // A lazy global is an accessor that replaces itself with a data property
  // on first use (reading, typeof, or assignment).
  const defineLazyGlobal = (name, make) => {
    ObjDefine(G, name, {
      get() { const v = make(); defineGlobal(name, v); return v; },
      set(v) { defineGlobal(name, v); },
      configurable: true, enumerable: false,
    });
  };
  const globals = { DOMException, crypto, TextEncoder, TextDecoder };
  for (const k of ObjKeys(globals)) defineGlobal(k, globals[k]);
  for (const name of ["DOMParser", "XMLSerializer", "Node", "Document", "DocumentFragment", "CharacterData", "Text",
    "Comment", "Element", "HTMLElement", "NodeList", "DOMTokenList"]) defineLazyGlobal(name, () => dom().classNamed(name));
  // The per-tag interface globals (about 70) cost a property definition each,
  // so setup defines them only when the binding could reach them.
  const defineInterfaceGlobals = () => {
    for (const name of IFACENAMES) if (name !== "Element" && name !== "HTMLElement") defineLazyGlobal(name, () => dom().classNamed(name));
  };

  // ---------------------------------------------------------------- schema classes (R-JS-85..87)
  const STORE = Symbol("pagelike.store");
  const classes = new MapFn();        // type URL → class
  const classInfo = new WeakMap();    // class → { type, props, map }
  const instanceClass = new WeakMap(); // instance (or its Map proxy) → class
  const proxyTarget = new WeakMap();  // Map proxy → target
  const mapProto = MapFn.prototype;
  const mapMethods = new SetFn([mapProto.get, mapProto.set, mapProto.has, mapProto.delete, mapProto.clear, mapProto.keys, mapProto.values, mapProto.entries, mapProto.forEach, mapProto[Symbol.iterator]]);
  const mapTraps = {
    get(t, k) {
      if (ReflectHas(t, k)) {
        const v = ReflectGet(t, k, t);
        return typeof v === "function" && mapMethods.has(v) ? v.bind(t) : v;
      }
      if (typeof k === "string" && mapHas(t, k)) return mapGet(t, k);
      return undefined;
    },
    set(t, k, v) { return ReflectSet(t, k, v, t); },
  };
  let docHidden = 0;
  const callJS = (f, name, receiver, args) => {
    if (typeof f !== "function") throw new TypeErrorFn("method " + name + " has no default export function");
    // A method whose receiver is a schema instance has no document (R-JS-17).
    const had = ObjHasOwn(G, "document");
    const saved = had ? ReflectGOPD(G, "document") : undefined;
    if (had) delete G.document;
    try {
      return ReflectApply(f, receiver, args);
    } finally {
      if (had) ObjDefine(G, "document", saved);
    }
  };
  const defineClass = (d) => {
    const pinfo = d.parent ? wmGet(classInfo, d.parent) : undefined;
    const Base = d.parent || (d.map ? MapFn : Object);
    const props = pinfo ? pinfo.props.slice() : [];
    for (const p of d.props) if (!props.includes(p)) arrayPush(props, p);
    const isMap = !!(d.map || (pinfo && pinfo.map));
    const makesProxy = d.map && !d.parent;
    let C;
    C = ({ [d.name]: class extends Base {
      constructor(init) {
        super();
        let self = this;
        if (!ObjHasOwn(self, STORE)) ObjDefine(self, STORE, { value: ObjCreate(null) });
        if (new.target === C || !wmHas(classInfo, new.target)) {
          if (init !== null && typeof init === "object") {
            for (const k of ObjKeys(init)) {
              if (props.includes(k)) self[STORE][k] = init[k];
              else ObjDefine(self, k, { value: init[k], writable: true, enumerable: true, configurable: true });
            }
          }
        }
        if (makesProxy) {
          const px = new ProxyFn(self, mapTraps);
          wmSet(proxyTarget, px, self);
          self = px;
        }
        wmSet(instanceClass, self, C);
        return self;
      }
    } })[d.name];
    for (const p of d.props) {
      ObjDefine(C.prototype, p, {
        get() { const s = this[STORE]; return s ? s[p] : undefined; },
        set(v) { const s = this[STORE]; if (s) s[p] = v; },
        configurable: true,
      });
    }
    if (makesProxy) {
      ObjDefine(C.prototype, "merge", {
        value: function merge(o) {
          const t = wmGet(proxyTarget, this) || this;
          if (o instanceof MapFn) { for (const [k, v] of mapEntries(o)) mapSet(t, k, v); }
          else if (o !== null && typeof o === "object") { for (const k of ObjKeys(o)) mapSet(t, k, o[k]); }
          return this;
        },
        writable: true, configurable: true,
      });
    }
    for (const m of d.methods) {
      const holder = m.static ? C : C.prototype;
      const fn = m.js !== null
        ? function (...a) { return callJS(m.js, m.name, this, a); }
        : function (...a) { return callHost(d.type, m.name, m.static, this, a); };
      ObjDefine(fn, "name", { value: m.name });
      ObjDefine(holder, m.name, { value: fn, writable: true, configurable: true });
    }
    wmSet(classInfo, C, { type: d.type, props, map: isMap });
    mapSet(classes, d.type, C);
    return C;
  };
  const makeInstance = (type, props, entries) => {
    const C = mapGet(classes, type);
    if (C === undefined) {
      const o = ObjCreate(null);
      for (const k of ObjKeys(props || {})) o[k] = props[k];
      return o;
    }
    const inst = new C(props || {});
    if (entries) {
      const t = wmGet(proxyTarget, inst) || inst;
      if (t instanceof MapFn) for (const k of ObjKeys(entries)) mapSet(t, k, entries[k]);
    }
    return inst;
  };

  // ---------------------------------------------------------------- values across the boundary (R-JS-40/41)
  const UNDEF = ObjCreate(null);
  let inputHandles = [];
  const defineData = (o, k, v) => ObjDefine(o, k, { value: v, writable: true, enumerable: true, configurable: true });
  const PRIVATE = ["headers", "auth"];
  let tainted = false;
  const taint = () => { if (!tainted) { tainted = true; SYS("taint"); } };
  const makeRequest = (shared, priv) => {
    const t = {};
    for (const k of ObjKeys(shared)) defineData(t, k, shared[k]);
    for (const k of ObjKeys(priv)) defineData(t, k, priv[k]);
    return new ProxyFn(t, {
      get(tt, k, r) { if (PRIVATE.includes(k)) taint(); return ReflectGet(tt, k, r); },
      has(tt, k) { if (PRIVATE.includes(k)) taint(); return ReflectHas(tt, k); },
      ownKeys(tt) { const ks = ReflectOwnKeys(tt); for (const k of ks) if (PRIVATE.includes(k)) taint(); return ks; },
      getOwnPropertyDescriptor(tt, k) { if (PRIVATE.includes(k)) taint(); return ReflectGOPD(tt, k); },
    });
  };
  const makeHeaders = (entries) => {
    const t = {};
    for (const [k, v] of entries) defineData(t, k, v);
    const low = (k) => (typeof k === "string" ? strLower(k) : k);
    return new ProxyFn(t, {
      get(tt, k, r) { const l = low(k); return ObjHasOwn(tt, l) ? tt[l] : ReflectGet(tt, k, r); },
      has(tt, k) { return ReflectHas(tt, low(k)) || ReflectHas(tt, k); },
      getOwnPropertyDescriptor(tt, k) { return ReflectGOPD(tt, low(k)); },
    });
  };
  const reviver = function (k, v) {
    if (v === null || typeof v !== "object") return v;
    if (ArrayIsArray(v)) {
      for (let i = 0; i < v.length; i++) if (v[i] === UNDEF) v[i] = undefined;
      return v;
    }
    const t = v.$type;
    if (typeof t !== "string") {
      for (const key of ObjKeys(v)) if (v[key] === UNDEF) v[key] = undefined;
      return v;
    }
    switch (t) {
      case "undefined": return UNDEF;
      case "element": return dom().wrap(inputHandles[v.$i]);
      case "instance": return makeInstance(v.$itemtype, v.$props, v.$entries);
      case "class": return mapGet(classes, v.$itemtype) ?? null;
      case "date": return new Date(v.$ms);
      case "headers": return makeHeaders(v.$entries);
      case "request": return makeRequest(v.shared, v.private);
      case "dict": {
        const o = {};
        for (const [kk, vv] of v.$entries) defineData(o, kk, vv === UNDEF ? undefined : vv);
        return o;
      }
    }
    return v;
  };
  const decodeWith = (json, handles) => {
    if (json === "" || json === undefined) return undefined;
    const saved = inputHandles;
    inputHandles = handles;
    try {
      const v = JSONparse(json, reviver);
      return v === UNDEF ? undefined : v;
    } finally {
      inputHandles = saved;
    }
  };

  class ReturnTypeError extends ErrorFn {}
  const rt = (msg) => new ReturnTypeError(msg);
  const tagDict = (o) => {
    if (!ObjHasOwn(o, "$type")) return o;
    const entries = [];
    for (const k of ObjKeys(o)) arrayPush(entries, [k, o[k]]);
    return { $type: "dict", $entries: entries };
  };
  // marshalOut converts a JavaScript value to the wire encoding, collecting
  // element handles; ReturnTypeError marks values that have no dombase form.
  const marshalOut = (value, elems) => {
    const stack = [];
    const enc = (v, level) => {
      switch (typeof v) {
        case "undefined": return null;
        case "boolean": case "string": return v;
        case "number":
          if (!NumberIsFinite(v)) throw rt("the number " + v + " has no dombase form");
          return v;
        case "bigint": throw rt("a BigInt cannot be returned");
        case "symbol": throw rt("a symbol cannot be returned");
        case "function": throw rt("a function cannot be returned");
      }
      if (v === null) return null;
      if (level > 64) throw rt("the value nests deeper than 64 levels");
      for (let i = 0; i < stack.length; i++) if (stack[i] === v) throw rt("the value is cyclic");
      const h = D === null ? -1 : D.hOf(v);
      if (h >= 0) {
        if (v instanceof D.Element) { arrayPush(elems, h); return { $type: "element", $i: elems.length - 1 }; }
        if (v instanceof D.Text) throw rt("a Text node cannot be returned; return its .textContent instead");
        if (v instanceof D.Comment) throw rt("a Comment node cannot be returned; return its .data instead");
        if (v instanceof D.Document) throw rt("a Document cannot be returned; return document.documentElement instead");
        if (v instanceof D.DocumentFragment) throw rt("a DocumentFragment cannot be returned; return its children ([...fragment.childNodes]) instead");
        throw rt("this node cannot be returned");
      }
      arrayPush(stack, v);
      try {
        const nl = D === null ? null : D.nlHandles(v);
        if (nl !== null) {
          const out = [];
          for (let i = 0; i < nl.length; i++) arrayPush(out, enc(D.wrap(nl[i]), level + 1));
          return out;
        }
        const C = wmGet(instanceClass, v);
        if (C !== undefined) {
          const info = wmGet(classInfo, C);
          const target = wmGet(proxyTarget, v) || v;
          const store = target[STORE] || {};
          const props = ObjCreate(null);
          for (const p of info.props) if (p in store) props[p] = enc(store[p], level + 1);
          const out = { $type: "instance", $itemtype: info.type, $props: tagDict(props) };
          if (target instanceof MapFn) {
            const entries = ObjCreate(null);
            for (const [k, x] of mapEntries(target)) entries[StringFn(k)] = enc(x, level + 1);
            out.$entries = tagDict(entries);
          }
          return out;
        }
        if (ArrayIsArray(v)) {
          const out = [];
          const n = v.length;
          for (let i = 0; i < n; i++) arrayPush(out, enc(v[i], level + 1));
          return out;
        }
        if (v instanceof MapFn) {
          const out = ObjCreate(null);
          for (const [k, x] of mapEntries(v)) out[StringFn(k)] = enc(x, level + 1);
          return tagDict(out);
        }
        if (v instanceof SetFn) {
          const out = [];
          for (const x of setValues(v)) arrayPush(out, enc(x, level + 1));
          return out;
        }
        const out = ObjCreate(null);
        for (const k of ObjKeys(v)) out[k] = enc(v[k], level + 1);
        return tagDict(out);
      } finally {
        stack.pop();
      }
    };
    return enc(value, 1);
  };

  // ---------------------------------------------------------------- host callbacks
  let contextTarget = null;
  let contextProxy = null;
  let buildContext = null;
  const getContext = () => {
    if (contextProxy === null && buildContext !== null) contextProxy = buildContext();
    return contextProxy;
  };
  const applyContext = (writes) => {
    getContext();
    if (!contextTarget) return;
    for (const w of writes) {
      if (w.deleted) delete contextTarget[w.name];
      else defineData(contextTarget, w.name, w.value);
    }
  };
  const callHost = (type, name, isStatic, receiver, args) => {
    const elems = [];
    let payload;
    try {
      const recv = isStatic ? { $type: "class", $itemtype: type } : marshalOut(receiver, elems);
      const list = [recv];
      for (const a of args) arrayPush(list, marshalOut(a, elems));
      payload = JSONstringify(list);
    } catch (e) {
      if (e instanceof ReturnTypeError) throw new TypeErrorFn("cannot pass this value to " + name + ": " + e.message);
      throw e;
    }
    const reply = JSONparse(sys("method", type, name, !!isStatic, payload, JSONstringify(elems)));
    const writes = [];
    for (const c of reply.context || []) arrayPush(writes, { name: c.name, deleted: !!c.deleted, value: c.deleted ? undefined : decodeWith(c.value, reply.handles) });
    applyContext(writes);
    return decodeWith(reply.value, reply.handles);
  };
  const writeBack = (name, value, deleted) => {
    let json = "";
    const elems = [];
    if (!deleted) {
      try {
        json = JSONstringify(marshalOut(value, elems));
      } catch (e) {
        if (e instanceof ReturnTypeError) throw new TypeErrorFn("Context." + name + " cannot hold this value: " + e.message);
        throw e;
      }
    }
    sys("context", name, json, JSONstringify(elems), !!deleted);
  };
  const makeContext = (init) => {
    const t = {};
    if (init !== null && typeof init === "object") for (const k of ObjKeys(init)) defineData(t, k, init[k]);
    contextTarget = t;
    return new ProxyFn(t, {
      set(tt, k, v) { if (typeof k === "string") writeBack(k, v, false); return ReflectSet(tt, k, v); },
      deleteProperty(tt, k) { if (typeof k === "string") writeBack(k, undefined, true); return ReflectDelete(tt, k); },
      defineProperty(tt, k, desc) { if (typeof k === "string" && "value" in desc) writeBack(k, desc.value, false); return ReflectDefine(tt, k, desc); },
    });
  };

  // ---------------------------------------------------------------- driver
  let cfg = null;
  let state = "idle";
  let outcome = null;
  let decodedArgs = null;
  let requestObj;
  const getArgs = () => {
    if (decodedArgs === null) decodedArgs = decodeWith(cfg.args, cfg.handles) || [];
    return decodedArgs;
  };
  const getRequest = () => {
    if (requestObj === undefined) requestObj = cfg.request === "" ? undefined : decodeWith(cfg.request, cfg.handles);
    return requestObj;
  };
  const setup = (json) => {
    cfg = JSONparse(json);
    inputHandles = cfg.handles;
    if (cfg.interfaces) defineInterfaceGlobals();
    if (cfg.doc >= 0) defineLazyGlobal("document", () => dom().wrap(cfg.doc));
    if (cfg.request !== "") defineLazyGlobal("request", getRequest);
    if (cfg.hasContext) {
      buildContext = () => {
        let init;
        if (cfg.expr) {
          init = {};
          const args = getArgs();
          const scope = cfg.scope || [];
          for (let i = 0; i < scope.length; i++) defineData(init, scope[i], args[i]);
        } else {
          init = cfg.context === "" ? {} : decodeWith(cfg.context, cfg.handles);
        }
        const r = getRequest();
        if (r !== undefined) defineData(init, "request", r);
        return makeContext(init);
      };
      defineLazyGlobal("Context", getContext);
    }
    return "ok";
  };
  const start = () => {
    state = "pending";
    let phase = "load";
    import(cfg.entry).then((ns) => {
      phase = "call";
      SYS("running");
      const main = ns.main;
      const f = main.default;
      if (typeof f !== "function") {
        outcome = { kind: "shape", message: ObjHasOwn(main, "default") || "default" in main ? "the default export is not a function" : "the module has no default export" };
        state = "done";
        return;
      }
      const t = cfg.this === "" ? undefined : decodeWith(cfg.this, cfg.handles);
      let args = getArgs();
      if (cfg.params) args = cfg.params.map((i) => args[i]);
      return ReflectApply(f, t, args);
    }).then((v) => {
      if (state === "done") return;
      outcome = { kind: "ok", value: v };
      state = "done";
    }, (e) => {
      outcome = { kind: phase === "load" ? "load" : "threw", error: e };
      state = "done";
    });
    return "started";
  };
  const isHTTPResponse = (e) => e !== null && typeof e === "object" &&
    ((ObjHasOwn(e, "schema_url") && e.schema_url === "https://pagelove.org/HTTPResponse") ||
     (ObjHasOwn(e, "itemtype") && e.itemtype === "https://pagelove.org/HTTPResponse"));
  const httpOf = (e) => {
    const status = e.status === undefined ? 500 : e.status;
    if (typeof status !== "number" || !NumberIsInteger(status) || status < 100 || status > 599) return undefined;
    const message = e.message === undefined || e.message === null ? "" : StringFn(e.message);
    const body = e.body === undefined || e.body === null ? message : StringFn(e.body);
    const headers = [];
    if (e.headers !== null && typeof e.headers === "object") {
      for (const k of ObjKeys(e.headers)) arrayPush(headers, [k, StringFn(e.headers[k])]);
    }
    return { status, message, body, headers };
  };
  const describe = (kind, e) => {
    const d = { outcome: kind, isNull: e === null };
    try {
      if (isHTTPResponse(e)) d.http = httpOf(e);
    } catch (e2) { /* an HTTPResponse whose fields throw is an ordinary failure */ }
    try {
      if (e instanceof ErrorFn || (e !== null && typeof e === "object" && typeof e.name === "string" && typeof e.message === "string")) {
        d.name = StringFn(e.name);
        d.message = StringFn(e.message);
        d.stack = typeof e.stack === "string" ? e.stack : "";
        d.text = d.name === "" ? d.message : d.message === "" ? d.name : d.name + ": " + d.message;
      } else {
        d.text = StringFn(e);
      }
    } catch (e3) {
      d.text = "[thrown value cannot be converted to a string]";
    }
    return JSONstringify(d);
  };
  const finish = () => {
    const o = outcome;
    if (o === null) return JSONstringify({ outcome: "pending" });
    if (o.kind === "shape") return JSONstringify({ outcome: "shape", message: o.message });
    if (o.kind !== "ok") return describe(o.kind, o.error);
    try {
      const elems = [];
      const value = marshalOut(o.value, elems);
      return JSONstringify({ outcome: "ok", value, elems });
    } catch (e) {
      if (e instanceof ReturnTypeError) return JSONstringify({ outcome: "return-type", message: e.message });
      return describe("threw", e);
    }
  };

  return {
    setup, start, finish,
    preloadDOM: () => { dom(); return "ok"; },
    state: () => state,
    tainted: () => tainted,
    schema: { defineClass },
    get kernel() { return domLoading ? { setDOMBuilder: (f) => { domBuilder = f; } } : undefined; },
  };
})();
