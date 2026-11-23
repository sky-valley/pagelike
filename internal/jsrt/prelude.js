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
    hasChildren: 62, parentEl: 63, microdata: 64,
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
    return domBuilder({ JSONparse, DOM, D1, D2, D3, D4, DN, OP, KEY, DOMException, TypeErrorFn, InternalErrorFn, ObjDefine, ObjCreate, StringFn, NumberFn, NumberIsFinite, MathTrunc, MathMax, MathMin, arrayPush, strLower, wmGet, wmSet, call });
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
  // WebCrypto public keys and signature checks (R-JS-39c; docs snapshot
  // 2026-11-23). A failure is a DOMException named for its cause; a wrong
  // argument type is a TypeError, as in browsers.
  const csys = (...a) => {
    try {
      return sys(...a);
    } catch (e) {
      const m = StringFn(e && e.message);
      const i = m.indexOf("|");
      if (i > 0) throw new DOMException(m.slice(i + 1), m.slice(0, i));
      throw e;
    }
  };
  const KEY_USAGES = ["encrypt", "decrypt", "sign", "verify", "deriveKey", "deriveBits", "wrapKey", "unwrapKey"];
  const ALG_NAMES = ["ECDSA", "RSASSA-PKCS1-v1_5", "RSA-PSS", "Ed25519", "Argon2id", "Argon2i", "Argon2d", "bcrypt", "PBKDF2", "HKDF"];
  // Secret-key algorithms (R-JS-39d) and the usages their keys may have.
  const ARGON = ["Argon2id", "Argon2i", "Argon2d"];
  const secretUsages = (name) => (name === "bcrypt" ? ["deriveBits", "verify"]
    : ARGON.includes(name) || name === "PBKDF2" || name === "HKDF" ? ["deriveBits", "deriveKey"] : null);
  const U32 = 4294967295;
  // uint reads a WebIDL [EnforceRange] unsigned number: a missing, negative,
  // non-numeric or too large value is a TypeError; a fraction is cut.
  const uint = (v, what, max) => {
    if (v === undefined || v === null) throw new TypeErrorFn(what + " is required");
    const n = MathTrunc(NumberFn(v));
    if (!NumberIsFinite(n) || n < 0 || n > max) throw new TypeErrorFn(what + " must be a number from 0 to " + max);
    return n;
  };
  const needBytes = (v, what) => {
    if (v === undefined || v === null) throw new TypeErrorFn(what + " is required");
    return bytesOf(v);
  };
  const opError = (m) => new DOMException(m, "OperationError");
  const HASH_NAMES = ["SHA-1", "SHA-256", "SHA-384", "SHA-512"];
  const HASH_BYTES = { "SHA-1": 20, "SHA-256": 32, "SHA-384": 48, "SHA-512": 64 };
  const canonName = (list, n) => {
    const up = StringFn(n).toUpperCase();
    for (const k of list) if (k.toUpperCase() === up) return k;
    return null;
  };
  // algOf reads an AlgorithmIdentifier: a name, or an object with a name.
  const algOf = (alg) => {
    let o = {};
    let n = alg;
    if (typeof alg !== "string") {
      if (alg === null || typeof alg !== "object") throw new TypeErrorFn("the algorithm must be a name or an object with a name");
      o = alg;
      n = alg.name;
      if (n === undefined) throw new TypeErrorFn("the algorithm has no name");
    }
    const name = canonName(ALG_NAMES, n);
    if (name === null) throw new DOMException("Unrecognized algorithm name", "NotSupportedError");
    return { name, o };
  };
  const hashOf = (o, what) => {
    let h = o.hash;
    if (h === undefined || h === null) throw new TypeErrorFn(what + " needs a hash");
    if (typeof h === "object") h = h.name;
    const name = canonName(HASH_NAMES, h);
    if (name === null) throw new DOMException("Unrecognized hash name", "NotSupportedError");
    return name;
  };
  const usagesOf = (usages) => {
    if (usages === null || typeof usages !== "object" || typeof usages[Symbol.iterator] !== "function") {
      throw new TypeErrorFn("the key usages must be a list");
    }
    const out = [];
    for (const u of usages) {
      if (typeof u !== "string" || !KEY_USAGES.includes(u)) throw new TypeErrorFn("'" + StringFn(u) + "' is not a key usage");
      if (!out.includes(u)) arrayPush(out, u);
    }
    return out;
  };
  let keyState;
  class CryptoKey {
    #s;
    constructor(key, s) {
      if (key !== KEY) throw new TypeErrorFn("Illegal constructor");
      this.#s = s;
    }
    static { keyState = (k) => (k !== null && typeof k === "object" && #s in k ? k.#s : null); }
    get type() { return this.#s.type; }
    get extractable() { return this.#s.extractable; }
    get algorithm() { return { ...this.#s.algorithm }; }
    get usages() { return [...this.#s.usages]; }
  }
  const keyOf = (k) => {
    const s = keyState(k);
    if (s === null) throw new TypeErrorFn("the key is not a CryptoKey made by crypto.subtle");
    return s;
  };
  const importKey = (format, keyData, algorithm, extractable, usages) => {
    const { name, o } = algOf(algorithm);
    const use = usagesOf(usages);
    const allowed = secretUsages(name);
    if (allowed !== null) {
      const sfmt = StringFn(format);
      const sformats = name === "PBKDF2" || name === "HKDF" ? ["raw", "raw-secret"] : ["raw-secret"];
      if (!sformats.includes(sfmt)) throw new DOMException("a " + name + " key is imported as " + sformats.join(" or "), "NotSupportedError");
      if (extractable) throw new DOMException("a " + name + " key cannot be extractable", "SyntaxError");
      if (use.length === 0) throw new DOMException("a " + name + " key needs a usage", "SyntaxError");
      for (const u of use) {
        if (!allowed.includes(u)) throw new DOMException("a " + name + " key can only have the usages " + allowed.join(" and "), "SyntaxError");
      }
      return new CryptoKey(KEY, { type: "secret", extractable: false, algorithm: { name }, usages: use, data: latin1(bytesOf(keyData)) });
    }
    const algo = { name };
    let curve = "";
    if (name === "ECDSA") {
      if (o.namedCurve === undefined || o.namedCurve === null) throw new TypeErrorFn("ECDSA needs a namedCurve");
      curve = StringFn(o.namedCurve);
      if (curve !== "P-256" && curve !== "P-384") throw new DOMException("only the curves P-256 and P-384 are supported", "NotSupportedError");
      algo.namedCurve = curve;
    } else if (name !== "Ed25519") {
      const h = hashOf(o, name);
      if (h === "SHA-1") throw new DOMException("RSA signatures use SHA-256, SHA-384 or SHA-512", "NotSupportedError");
      algo.hash = { name: h };
    }
    const fmt = StringFn(format);
    const formats = name === "ECDSA" || name === "Ed25519" ? ["spki", "raw"] : ["spki"];
    if (!formats.includes(fmt)) throw new DOMException("a " + name + " public key is imported as " + formats.join(" or "), "NotSupportedError");
    for (const u of use) if (u !== "verify") throw new DOMException("a public key can only have the verify usage", "SyntaxError");
    const data = latin1(bytesOf(keyData));
    csys("wc.import", name, curve, fmt, data);
    return new CryptoKey(KEY, { type: "public", extractable: !!extractable, algorithm: algo, usages: use, format: fmt, data });
  };
  const verifySig = (algorithm, key, signature, data) => {
    const { name, o } = algOf(algorithm);
    if (name !== "bcrypt" && secretUsages(name) !== null) throw new DOMException(name + " does not verify", "NotSupportedError");
    const k = keyOf(key);
    if (k.algorithm.name !== name) throw new DOMException("the key was imported for " + k.algorithm.name, "InvalidAccessError");
    if (!k.usages.includes("verify")) throw new DOMException("the key does not have the verify usage", "InvalidAccessError");
    if (name === "bcrypt") {
      // The stored $2a$, $2b$ or $2y$ string is the signature; the password
      // is the key, so the data must be empty.
      const stored = latin1(bytesOf(signature));
      if (bytesOf(data).length !== 0) throw opError("bcrypt verify takes no data: the password comes from the key");
      return csys("wc.pwhash", JSONstringify({ alg: "bcrypt-verify", password: btoa(k.data), stored: btoa(stored) }));
    }
    let hash = "";
    if (name === "ECDSA") {
      hash = hashOf(o, "ECDSA");
      if (hash !== (k.algorithm.namedCurve === "P-256" ? "SHA-256" : "SHA-384")) {
        throw new DOMException("ECDSA " + k.algorithm.namedCurve + " is supported with its own hash only", "NotSupportedError");
      }
    } else if (name !== "Ed25519") {
      hash = k.algorithm.hash.name;
      if (name === "RSA-PSS" && o.saltLength !== undefined) {
        const n = o.saltLength;
        if (typeof n !== "number" || !NumberIsFinite(n) || n < 0 || MathTrunc(n) !== n) throw new TypeErrorFn("saltLength must be a non-negative whole number");
        if (n !== HASH_BYTES[hash]) throw new DOMException("RSA-PSS supports only a salt as long as the hash", "NotSupportedError");
      }
    }
    const sig = latin1(bytesOf(signature));
    return csys("wc.verify", name, k.algorithm.namedCurve || "", hash, k.format, k.data, sig, latin1(bytesOf(data)));
  };
  const deriveBits = (algorithm, key, length) => {
    const { name, o } = algOf(algorithm);
    if (secretUsages(name) === null) throw new DOMException(name + " does not derive bits", "NotSupportedError");
    const k = keyOf(key);
    if (k.algorithm.name !== name) throw new DOMException("the key was imported for " + k.algorithm.name, "InvalidAccessError");
    if (!k.usages.includes("deriveBits")) throw new DOMException("the key does not have the deriveBits usage", "InvalidAccessError");
    const noLength = length === undefined || length === null;
    if (name === "bcrypt") {
      const salt = needBytes(o.salt, "salt");
      const cost = uint(o.cost, "cost", 255);
      if (!noLength && uint(length, "length", U32) !== 480) throw opError("bcrypt gives 480 bits: leave out the length or pass 480");
      if (salt.length !== 16) throw opError("the bcrypt salt is 16 bytes");
      if (cost < 4 || cost > 31) throw opError("the bcrypt cost is from 4 to 31");
      return fromHex(csys("wc.pwhash", JSONstringify({ alg: name, password: btoa(k.data), salt: btoa(latin1(salt)), cost }))).buffer;
    }
    if (ARGON.includes(name)) {
      const nonce = needBytes(o.nonce, "nonce");
      const lanes = uint(o.parallelism, "parallelism", U32);
      const memory = uint(o.memory, "memory", U32);
      const passes = uint(o.passes, "passes", U32);
      const version = o.version === undefined ? 19 : uint(o.version, "version", 255);
      const secret = o.secretValue === undefined ? null : bytesOf(o.secretValue);
      const ad = o.associatedData === undefined ? null : bytesOf(o.associatedData);
      if (noLength) throw opError("Argon2 needs a length in bits");
      const bits = uint(length, "length", U32);
      if (bits % 8 !== 0 || bits < 32 || bits > 524288) throw opError("the length is a multiple of 8 from 32 to 524288 bits");
      if (version !== 19) throw opError("the only Argon2 version is 19");
      if (nonce.length < 8) throw opError("the nonce is at least 8 bytes");
      if (lanes < 1 || lanes > 255) throw opError("parallelism is from 1 to 255");
      if (passes < 1) throw opError("passes is 1 or more");
      if (memory < 8 * lanes || memory > 262144) throw opError("memory is at least 8 KiB for each lane and at most 262144 KiB");
      if (ad !== null && ad.length > 32) throw opError("associatedData is at most 32 bytes");
      const job = { alg: name, password: btoa(k.data), salt: btoa(latin1(nonce)), passes, memory, lanes, length: bits / 8 };
      if (secret !== null) job.secret = btoa(latin1(secret));
      if (ad !== null) job.data = btoa(latin1(ad));
      return fromHex(csys("wc.pwhash", JSONstringify(job))).buffer;
    }
    // PBKDF2 and HKDF read their parameters as browsers do.
    const hash = hashOf(o, name);
    const salt = needBytes(o.salt, "salt");
    let info = "";
    let iterations = 0;
    if (name === "HKDF") info = latin1(needBytes(o.info, "info"));
    else iterations = uint(o.iterations, "iterations", U32);
    if (noLength) throw opError(name + " needs a length in bits");
    const bits = uint(length, "length", U32);
    if (bits % 8 !== 0) throw opError("the length is a multiple of 8");
    return fromHex(csys("wc.derive", name, hash, k.data, latin1(salt), info, iterations, bits / 8)).buffer;
  };
  const promised = (f) => (...a) => {
    try {
      return PromiseResolve(f(...a));
    } catch (e) {
      return PromiseReject(e);
    }
  };
  const subtle = {
    importKey: promised(importKey),
    verify: promised(verifySig),
    deriveBits: promised(deriveBits),
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
  // ---------------------------------------------------------------- Temporal (R-JS-43)
  // The Temporal global (docs snapshot 2026-11-23). The classes hold a
  // value's type and text and nothing else: every constructor, static and
  // method is one "temporal" request to the worker, which runs it on the
  // date and time engine Sessel uses. Built on first use.
  const RangeErrorFn = RangeError;
  const BigIntFn = BigInt;
  const DateFn = Date;
  const dateGetTime = uncurry(Date.prototype.getTime);
  const dateToISO = uncurry(Date.prototype.toISOString);
  const isProtoOf = uncurry(Object.prototype.isPrototypeOf);
  const SymToPrimitive = Symbol.toPrimitive;
  const SymToStringTag = Symbol.toStringTag;
  // An exact time is kept for the years 1678 to 2261 (R-JS-43).
  const MIN_EXACT_MS = Date.UTC(1678, 0, 1);
  const MAX_EXACT_MS = Date.UTC(2262, 0, 1);
  let T = null;
  const temporal = () => T || (T = buildTemporal());
  const buildTemporal = () => {
    const state = new WeakMap(); // Temporal object -> { k: type, s: text, f: getters or undefined }
    const classes = ObjCreate(null);
    const make = (k, s, f) => {
      const o = ObjCreate(classes[k].prototype);
      wmSet(state, o, { k, s, f });
      return o;
    };
    const fix = (f) => {
      for (const k of ObjKeys(f)) {
        const v = f[k];
        if (v !== null && typeof v === "object") f[k] = BigIntFn(v.$big);
      }
      return f;
    };
    const revive = (v) => {
      if (v === null || typeof v !== "object") return v;
      if (typeof v.$t === "string") return make(v.$t, v.s, fix(v.f));
      if (typeof v.$big === "string") return BigIntFn(v.$big);
      return fix(v);
    };
    const enc = (v, depth) => {
      switch (typeof v) {
        case "undefined": return null;
        case "string": case "boolean": return v;
        case "number": return NumberIsFinite(v) ? v : { $n: StringFn(v) };
        case "bigint": return { $big: StringFn(v) };
        case "symbol": return { $x: "symbol" };
        case "function": return { $x: "function" };
      }
      if (v === null) return null;
      const st = wmGet(state, v);
      if (st !== undefined) return { $t: st.k, s: st.s };
      const o = ObjCreate(null);
      if (depth < 3) {
        let n = 0;
        for (const k in v) {
          if (++n > 64) break;
          const x = v[k];
          if (x !== undefined) o[k] = enc(x, depth + 1);
        }
      }
      return { $o: o };
    };
    const tsys = (k, m, s, a) => {
      const args = [];
      for (let i = 0; i < a.length; i++) arrayPush(args, enc(a[i], 0));
      let out;
      try {
        out = sys("temporal", JSONstringify({ k, m, s, a: args }));
      } catch (e) {
        const msg = StringFn(e && e.message);
        const i = msg.indexOf("|");
        if (i > 0) throw new (msg.slice(0, i) === "RangeError" ? RangeErrorFn : TypeErrorFn)(msg.slice(i + 1));
        throw e;
      }
      return revive(JSONparse(out));
    };
    // An object where an Instant is expected is read through its primitive
    // value, which must be a string.
    const instantArg = (v) => {
      if (v === null || typeof v !== "object") return v;
      const st = wmGet(state, v);
      if (st !== undefined) return st.k === "Instant" || st.k === "ZonedDateTime" ? v : st.s;
      let p = v;
      const exotic = v[SymToPrimitive];
      if (exotic !== undefined && exotic !== null) {
        p = ReflectApply(exotic, v, ["string"]);
      } else {
        for (const name of ["toString", "valueOf"]) {
          const f = v[name];
          if (typeof f !== "function") continue;
          const r = ReflectApply(f, v, []);
          if (r === null || typeof r !== "object") { p = r; break; }
        }
      }
      if (typeof p !== "string") throw new TypeErrorFn("an Instant is made from a string, an Instant or a ZonedDateTime");
      return p;
    };
    const prep = (k, m, a) => {
      if (k !== "Instant") return a;
      if (m === "from" || m === "equals" || m === "until" || m === "since") a[0] = instantArg(a[0]);
      if (m === "compare") { a[0] = instantArg(a[0]); a[1] = instantArg(a[1]); }
      return a;
    };
    const hidden = (o, name, value) => ObjDefine(o, name, { value, writable: true, configurable: true, enumerable: false });
    const define = (k, getters, methods, statics) => {
      const self = (o) => {
        const st = o !== null && typeof o === "object" ? wmGet(state, o) : undefined;
        if (st === undefined || st.k !== k) throw new TypeErrorFn("this is not a Temporal." + k);
        return st;
      };
      const C = class {
        constructor(...a) {
          // Reflect.construct with a new target that is not the type or a
          // subclass of it is refused.
          if (new.target !== C && !isProtoOf(C, new.target)) throw new TypeErrorFn("Temporal." + k + " is constructed as itself or a subclass");
          wmSet(state, this, wmGet(state, tsys(k, "new", null, a)));
        }
      };
      ObjDefine(C, "name", { value: k, configurable: true });
      const P = C.prototype;
      for (const g of getters) {
        ObjDefine(P, g, {
          get() {
            const st = self(this);
            if (st.f === undefined) st.f = tsys(k, "fields", st.s, []);
            return st.f[g];
          },
          configurable: true, enumerable: false,
        });
      }
      for (const m of methods) {
        hidden(P, m, { [m](...a) { return tsys(k, m, self(this).s, prep(k, m, a)); } }[m]);
      }
      hidden(P, "toJSON", { toJSON() { return self(this).s; } }.toJSON);
      hidden(P, "valueOf", { valueOf() { throw new TypeErrorFn("a Temporal." + k + " has no primitive value: compare with compare() or equals()"); } }.valueOf);
      ObjDefine(P, SymToStringTag, { value: "Temporal." + k, configurable: true });
      for (const m of statics) {
        hidden(C, m, { [m](...a) { return tsys(k, m, null, prep(k, m, a)); } }[m]);
      }
      classes[k] = C;
    };
    const DATE = ["calendarId", "year", "month", "monthCode", "day", "dayOfWeek", "dayOfYear", "weekOfYear", "yearOfWeek",
      "daysInWeek", "daysInMonth", "daysInYear", "monthsInYear", "inLeapYear"];
    const TIME = ["hour", "minute", "second", "millisecond", "microsecond", "nanosecond"];
    const TEXT = ["toString", "toLocaleString"];
    const MATH = ["add", "subtract", "until", "since", "with", "equals"];
    define("PlainDate", DATE, [...TEXT, ...MATH, "toPlainDateTime", "toZonedDateTime", "toPlainYearMonth", "toPlainMonthDay"], ["from", "compare"]);
    define("PlainTime", TIME, [...TEXT, ...MATH], ["from", "compare"]);
    define("PlainDateTime", [...DATE, ...TIME], [...TEXT, ...MATH, "toPlainDate", "toPlainTime", "toZonedDateTime"], ["from", "compare"]);
    define("ZonedDateTime", [...DATE, ...TIME, "timeZoneId", "offset", "epochMilliseconds", "epochNanoseconds"],
      [...TEXT, ...MATH, "withTimeZone", "toInstant", "toPlainDate", "toPlainTime", "toPlainDateTime"], ["from", "compare"]);
    define("Instant", ["epochMilliseconds", "epochNanoseconds"], [...TEXT, "add", "subtract", "until", "since", "equals", "toZonedDateTimeISO"],
      ["from", "compare", "fromEpochMilliseconds", "fromEpochNanoseconds"]);
    define("Duration", ["years", "months", "weeks", "days", "hours", "minutes", "seconds", "milliseconds", "microseconds", "nanoseconds", "sign", "blank"],
      [...TEXT, "with", "negated", "abs", "add", "subtract", "total"], ["from", "compare"]);
    define("PlainYearMonth", ["calendarId", "year", "month", "monthCode", "daysInMonth", "daysInYear", "monthsInYear", "inLeapYear"],
      [...TEXT, ...MATH, "toPlainDate"], ["from", "compare"]);
    define("PlainMonthDay", ["calendarId", "monthCode", "day"], [...TEXT, "with", "equals", "toPlainDate"], ["from"]);
    const Now = {};
    for (const m of ["instant", "timeZoneId", "plainDateISO", "plainTimeISO", "plainDateTimeISO", "zonedDateTimeISO"]) {
      hidden(Now, m, { [m](...a) { return tsys("Now", m, null, a); } }[m]);
    }
    ObjDefine(Now, SymToStringTag, { value: "Temporal.Now", configurable: true });
    const ns = {};
    for (const k of ObjKeys(classes)) hidden(ns, k, classes[k]);
    hidden(ns, "Now", Now);
    ObjDefine(ns, SymToStringTag, { value: "Temporal", configurable: true });
    return {
      ns, make,
      stateOf: (o) => wmGet(state, o),
      instantOfMs: (ms) => tsys("Instant", "fromEpochMilliseconds", null, [ms]),
    };
  };
  ObjDefine(Date.prototype, "toTemporalInstant", {
    value: { toTemporalInstant() {
      const ms = dateGetTime(this);
      if (ms !== ms) throw new RangeErrorFn("Invalid Date");
      return temporal().instantOfMs(ms);
    } }.toTemporalInstant,
    writable: true, configurable: true, enumerable: false,
  });
  defineLazyGlobal("Temporal", () => temporal().ns);

  // ---------------------------------------------------------------- base64 (R-JS-39b)
  // btoa and atob as HTML defines them: btoa takes characters U+0000 to
  // U+00FF; atob is forgiving about ASCII white space and padding.
  const B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  const charCode = uncurry(String.prototype.charCodeAt);
  const fromCharCode = String.fromCharCode;
  const B64REV = ObjCreate(null);
  for (let i = 0; i < 64; i++) B64REV[B64[i]] = i;
  const badChars = (what) => new DOMException(what, "InvalidCharacterError");
  function btoa(data) {
    if (arguments.length === 0) throw new TypeErrorFn("btoa requires 1 argument");
    const str = StringFn(data);
    let out = "";
    for (let i = 0; i < str.length; i += 3) {
      const n = str.length - i;
      const a = charCode(str, i), b = n > 1 ? charCode(str, i + 1) : 0, c = n > 2 ? charCode(str, i + 2) : 0;
      if (a > 255 || b > 255 || c > 255) throw badChars("The string to be encoded contains characters outside of the Latin1 range.");
      out += B64[a >> 2] + B64[((a & 3) << 4) | (b >> 4)] + (n > 1 ? B64[((b & 15) << 2) | (c >> 6)] : "=") + (n > 2 ? B64[c & 63] : "=");
    }
    return out;
  }
  function atob(data) {
    if (arguments.length === 0) throw new TypeErrorFn("atob requires 1 argument");
    const src = StringFn(data);
    let str = "";
    for (let i = 0; i < src.length; i++) {
      const ch = src[i];
      if (ch !== " " && ch !== "\t" && ch !== "\n" && ch !== "\f" && ch !== "\r") str += ch;
    }
    if (str.length % 4 === 0 && str.length > 0) {
      if (str[str.length - 1] === "=") str = str.slice(0, -1);
      if (str[str.length - 1] === "=") str = str.slice(0, -1);
    }
    if (str.length % 4 === 1) throw badChars("The string to be decoded is not correctly encoded.");
    let out = "", buf = 0, bits = 0;
    for (let i = 0; i < str.length; i++) {
      const v = B64REV[str[i]];
      if (v === undefined) throw badChars("The string to be decoded is not correctly encoded.");
      buf = ((buf << 6) | v) & 0xffffff;
      bits += 6;
      if (bits >= 8) {
        bits -= 8;
        out += fromCharCode((buf >> bits) & 255);
      }
    }
    return out;
  }

  // Only the documented base64 helpers of Uint8Array exist (R-JS-39b; docs
  // snapshot 2026-11-23: fromHex, toHex, setFromBase64 and setFromHex are
  // not available).
  delete Uint8Array.fromHex;
  delete Uint8Array.prototype.toHex;
  delete Uint8Array.prototype.setFromBase64;
  delete Uint8Array.prototype.setFromHex;
  const globals = { DOMException, crypto, TextEncoder, TextDecoder, btoa, atob };
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
      case "temporal": return temporal().make(v.$k, v.$s, undefined);
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
        // A Temporal object crosses as its type and text, a Date as the
        // Instant it stands for (R-JS-42).
        if (T !== null) {
          const st = T.stateOf(v);
          if (st !== undefined) return { $type: "temporal", $k: st.k, $s: st.s };
        }
        if (v instanceof DateFn) {
          const ms = dateGetTime(v);
          if (ms !== ms) throw rt("an invalid Date cannot be returned");
          if (ms < MIN_EXACT_MS || ms >= MAX_EXACT_MS) throw rt("a Date outside the years 1678 to 2261 cannot be returned");
          return { $type: "temporal", $k: "Instant", $s: dateToISO(v) };
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
  // The Pagelove global of code that may save (R-JS-23b; docs snapshot
  // 2026-11-23, saving-data). A refused save throws an Error whose message
  // is the verb, the status and the reason.
  const PLACEMENTS = ["append", "prepend", "before", "after"];
  const saveItem = (verb, item, elems) => {
    const t = typeof item;
    if (t !== "string" && (item === null || t !== "object")) {
      throw new TypeErrorFn("Pagelove." + verb + "() takes a schema instance, a string of HTML or a DOM element");
    }
    try {
      return JSONstringify([marshalOut(item, elems)]);
    } catch (e) {
      if (e instanceof ReturnTypeError) throw new TypeErrorFn("Pagelove." + verb + "() takes a schema instance, a string of HTML or a DOM element");
      throw e;
    }
  };
  const optSelector = (s) => (s === null || s === undefined ? null : StringFn(s));
  const Pagelove = {
    PUT(item, path, selector) {
      const elems = [];
      const payload = saveItem("PUT", item, elems);
      if (typeof path !== "string") throw new ErrorFn('PUT: Instance has no source path. Use PUT(instance, "/path.html")');
      sys("save", "PUT", payload, JSONstringify(elems), path, optSelector(selector), "");
      return undefined;
    },
    POST(item, path, selector, placement) {
      const elems = [];
      const payload = saveItem("POST", item, elems);
      if (typeof path !== "string") throw new TypeErrorFn("Pagelove.POST() takes a path");
      let p = "append";
      if (placement !== null && placement !== undefined) {
        if (typeof placement !== "string") throw new TypeErrorFn("Pagelove.POST's placement must be a string");
        p = strLower(placement).trim();
        if (!PLACEMENTS.includes(p)) throw new ErrorFn('POST: unknown placement "' + placement + '"; use append, prepend, before or after');
      }
      const sel = optSelector(selector);
      if (sel === null && (p === "before" || p === "after")) {
        throw new ErrorFn('POST: placement "' + p + '" needs a selector; with no selector the item goes inside the page\'s body');
      }
      sys("save", "POST", payload, JSONstringify(elems), path, sel, p);
      return undefined;
    },
    DELETE(target, selector) {
      if (typeof target !== "string") throw new TypeErrorFn("Pagelove.DELETE() takes a path");
      sys("save", "DELETE", "", "[]", target, optSelector(selector), "");
      return true;
    },
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
    if (cfg.saves) defineGlobal("Pagelove", Pagelove);
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
