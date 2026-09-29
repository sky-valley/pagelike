# 0001: Server-side JavaScript engine and isolation

- **Status:** proposed
- **Date:** 2026-09-28
- **Spike:** `research/spikes/js-engine/` (exploratory; not part of the public repository — its results are recorded here).
  - `all.jsonl`: the full matrix, where modernc's "configured" stack is 262,144 slots;
  - `modernc-stack-slots.jsonl`: modernc at 1k, 2k, 5k and 20k slots;
  - `results-helper-{modernc,goja}.jsonl`: the helper-process prototype.
- **Evidence snapshot:** docs 2026-09-28 (`research/docs/2026-09-28/md`):
  - `languages_javascript_server_javascript-in-schemas`
  - `languages_javascript_server_dom*`
  - `reference_composing-pages_JavaScript-Expression-Binding`
- **Affects:** `internal/jsrt` (new), `cmd/pagelike` (a worker subcommand), `internal/compose` (`j:` bindings, method elements), `internal/schema` (default/@read/@write/@validate/@computed, methods), `internal/reactions` (trigger/processor method dispatch)
- **Measured on:** Apple M1 Pro (10 cores, 16 GiB), macOS (Darwin 27.0), Go 1.26.4, `CGO_ENABLED=0`. Each probe ran in its own process. Linux was not measured; see Risks.

## Decision

1. **Engine: `modernc.org/quickjs` (v0.24.2, QuickJS release 2026-06-04, ccgo-transpiled, pure Go).**
   - It is the only candidate that passed all 23 ES feature probes and every module and ambient-capability probe.
   - With its limits set, it turned every hostile probe into a clean engine error. The one quirk is a `null` thrown value on some out-of-memory paths (see Risks).
   - It is the same engine family PageLove appears to run. The docs' exhaustion error is QuickJS's `InternalError: stack overflow`, and the documented stack-frame shape `    at default (eval:2:17)` is exactly what modernc prints for a module named `eval`.
   - It adds 2.3 MiB to a binary that already links `modernc.org/sqlite`, because the two share `modernc.org/libc`.
2. **Isolation: server JS never runs in the serving process.** It runs in a pool of bounded worker processes (`pagelike jsrt-worker`, the same binary). Limits are enforced in three layers:
   1. the engine's own limits, which give the exact PageLove error variants;
   2. OS limits the worker places on itself;
   3. a parent that SIGKILLs a worker on a hard deadline or an RSS ceiling and respawns it.

   The spike shows that none of the in-process options is a real bound on its own:
   - A 3-line script crashes the whole Go process (`fatal error: goroutine stack exceeds 1000000000-byte limit`, which cannot be recovered):
     - through modernc, with default settings or with an over-large stack setting;
     - through goja/sobek, with any setting.
   - goja/sobek hang the host thread on a backtracking regex, because `Interrupt` does not reach regexp2.
   - Go stack growth from deep recursion is outside every engine's memory cap.
3. **The DOM crosses the boundary as data, not as calls.**
   - The request carries the relevant document or element HTML.
   - The worker parses it with the same `internal/dom` and `internal/selector` code as the host, and serves every DOM call locally. A host call costs 0.64 µs in-worker, against about 47 µs for a pipe round trip.
   - It returns serialized results or mutations.
   - Only coarse operations go back to the host through a callback on the same pipe: schema lookup, Sessel method calls, and `Context` write-back.
4. **Rejected:**
   - **`fastschema/qjs` as shipped.** It exposes `qjs:std`/`qjs:os` and mounts the working directory: the spike read `go.mod` and wrote a file from JS. Its memory and stack limits are not effective in its WASM build, and a host call costs 18.6 µs.
   - **goja and sobek.** They have ES gaps: `\p{…}` silently fails, there are no async generators, no ES2024 builtins, and module strictness or `this` is wrong. They have no memory cap, and they hang or crash the host as described above.

   **Fallback, if process isolation proves unacceptable somewhere:** QuickJS in wazero with our own WASI build. Without `std`/`os`/WASI filesystem, with a declared memory max and with a Go-side module loader, it gives in-process fault isolation. It costs a C/WASI toolchain in the build.

## Requirements (from the PageLove docs)

| # | Requirement | Source |
|---|---|---|
| R1 | Slot source is an ES module. The server evaluates it in a **fresh context**, reads `default`, checks that it is a function, and calls it with `this` bound (instance, receiver, or `undefined`) and positional args (`context`, the pipeline value, or method params). An `async` default and a Promise-returning method are driven to settlement. | javascript-in-schemas: "The export default contract", "Method bodies" |
| R2 | The only allowed import is a schema import, `import Note from "<schema itemtype URL>" with { type: "https://pagelove.org/Schema" }`, resolved from the host's schema cache into a synthesized class that supports construction, accessors, `instanceof` and methods. Anything else gives `import-not-allowed`; an unknown schema gives `unknown-schema`. | "Importing schemas" |
| R3 | ES2020+ syntax (destructuring, spread/rest, template and tagged literals, default params, async/await, optional chaining, `??`, numeric separators) and the standard built-ins. `Math.random`/`Date.now` are allowed. | "Supported language features" |
| R4 | **Absent:** `fetch`, `process`, `require`, filesystem and environment, `setTimeout`/`setInterval`. `DOMException` is the only non-ES global the sandbox defines; there are also the DOM globals `document`, `DOMParser`, `XMLSerializer`, and the node classes. `j:` expressions may `await` built-ins such as `crypto.subtle`. | "Not supported", "Explaining why…", JavaScript-Expression-Binding |
| R5 | Limits per context: memory 16 MB (`out-of-memory`), stack 256 KB (`threw`, typically `InternalError: stack overflow`), time from the request budget shared with Sessel (`timeout`), and ops charged at the periodic interrupt. | "Resource limits" |
| R6 | Failure variants: `parse`, `shape`, `threw` (message + stack), `timeout`, `out-of-memory`, `marshal`, `return-type` (functions, symbols, cycles, depth > 64), `import-not-allowed`, `unknown-schema`. A thrown **plain object** with `schema_url`/`itemtype` = `https://pagelove.org/HTTPResponse` chooses the status and message. | "Errors", "Explaining why a value was rejected" |
| R7 | DOM surface: real WHATWG-shaped classes with `instanceof` over a flattened node. Members outside a node's interface are `undefined`, not thrown. `document` is read-only except in `default`, while construction is always writable. `querySelector` on an element scans the whole document; results are static `NodeList`s; `DOMException` names are defined. A returned element is spliced into the page. DOM operations are charged to the budget. | server/dom*, dom/parsing |
| R8 | Brief: pure Go (no cgo). Arbitrary server code runs under a real bounded isolation mechanism; an interruptible interpreter is not enough. Prefer a bounded helper if simpler. | task brief |

## Candidates and method

| Spike name | Module | Notes |
|---|---|---|
| `modernc` | `modernc.org/quickjs` v0.24.2 | QuickJS 2026-06-04 via ccgo. Limits: `SetMemoryLimit`, `SetMaxStackSize` (libc TLS "slots", about 1 per JS frame), `Interrupt()`. Go-side module loader and normalizer. |
| `qjs` | `github.com/fastschema/qjs` v0.0.6 | QuickJS-ng as WASI in wazero 1.9. Limits: `Option.MemoryLimit`/`MaxStackSize`; CPU through `CloseOnContextDone`, which kills the instance. |
| `qjs-wasmcap` | same | The module's memory section is patched to declare a maximum (cap + 16 MiB), so wazero refuses `memory.grow` past it. |
| `goja` | `github.com/dop251/goja` @2026-09-26 | Has no ES modules, so esbuild (pure Go) bundles driver, main and imports into an IIFE. Limits: `Interrupt`, `SetMaxCallStackSize`. |
| `sobek` | `github.com/grafana/sobek` @2026-09-15 | The goja fork with native ESM (k6). Same limits as goja. |

Every engine runs the same driver module. It does `import f from "main"` and exposes `__pl_call(thisJSON, argsJSON)`, which applies `f`, settles promises, and classifies thrown values. The same JS prelude builds `Node`/`Element`/`HTMLElement`/`Document` on one `__dom(op, handle, arg)` host function over an `x/net/html` + cascadia DOM, so the numbers compare like with like.

## Measurements

### Language and module contract

| Check | modernc | qjs | goja (+esbuild) | sobek |
|---|---|---|---|---|
| ES feature probes passed (23 modules, R3 and beyond) | **23/23** | 22/23 (no RegExp `v` flag) | 15/23 | 16/23 |
| `export default function`, called from Go with `this` and args | ✓ | ✓ (via bundle) | ✓ (via bundle) | ✓ |
| async default, top-level await | ✓ ✓ | ✓ ✓ | ✓ ✗ (IIFE) | ✓ ✓ |
| in-memory `import` resolved by the host | ✓ native loader hook | ✗ natively (loader reads WASI FS); ✓ bundled | bundled only | ✓ native |
| `import … with { type: "https://pagelove.org/Schema" }` | ✓ parses; the attribute is **not** passed to the Go loader | ✓ (bundler sees it) | ✓ (bundler sees it) | ✗ SyntaxError |
| relative, http and bare imports → `import-not-allowed` | ✓ static and dynamic | ✓ static; **✗ dynamic `import("qjs:"+"std")` loads** | ✓ static; `import()` is a syntax error | ✓ |
| `shape` (no default, or not a function), `parse` | ✓ ✓ | ✓ ✓ | ✓ ✓ | ✓ ✓ |
| thrown plain `HTTPResponse` object reaches the host | ✓ | ✓ | ✓ | ✓ |
| cyclic return → `return-type` | ✓ | ✓ | ✓ | ✓ |
| `\p{L}` with the `u` flag | ✓ | ✓ | **✗ silently `false`** | **✗ silently `false`** |
| async generators / `Symbol.asyncIterator` | ✓ | ✓ | ✗ | ✗ |
| ES2024–25 builtins (`Object.groupBy`, `Promise.withResolvers`, Set methods, iterator helpers) | ✓ | ✓ | ✗ | ✗ |
| module strict mode (`this === undefined`) | ✓ | ✓ | ✗ (IIFE bundle is sloppy mode) | ✗ (module `this` is an object) |
| error shape vs the docs (`    at default (eval:2:17)`) | **same shape** (`at default (eval:3:11)`) | bundle renames frames (`main_default (driver.js…)`) | `TypeError: … at main_default (<eval>…)` | goja style |

Notes:
- The feature list is in `cmd/spike/main.go`, var `features`.
- The message text differs from the docs example in every engine. The docs show V8 wording (`Cannot read properties of undefined (reading 'foo')`); QuickJS says `cannot read property 'foo' of undefined`. The harness should compare variants, not message text, until a live observation is recorded.

### Ambient capabilities

| Probe | modernc | qjs (stock) | qjs, globals deleted + empty mount | goja | sobek |
|---|---|---|---|---|---|
| non-ES globals present | none. The only extras are ES `InternalError`, `escape`/`unescape` and `SharedArrayBuffer`/`Atomics`; `Atomics.wait` throws "cannot block in this thread". | `std`, `os`, `bjson`, `print`, `console`, `setTimeout`, `setInterval`, `queueMicrotask`, `gc`, `navigator`, `performance`, `scriptArgs`, `DOMException`, `QJS_PROXY_VALUE` | `queueMicrotask`, `gc`, `performance`, `DOMException`, `QJS_PROXY_VALUE` remain | none | none |
| `import("qjs:std")` / `import("qjs:os")` | denied | **loads** | **still loads** (native module registry, not globals) | n/a | denied |
| read and write files through `std` | denied | **read `./go.mod`, wrote `./pwned-by-js.txt`** (CWD is mounted at `/`) | read denied; **write succeeded** into the temp mount | n/a | n/a |
| timers through `os.setTimeout` | denied | **fired** | **fired** | n/a | n/a |

### Resource controls (budget 100 ms, memory cap 16 MiB)

| Probe | modernc | qjs | qjs-wasmcap | goja | sobek |
|---|---|---|---|---|---|
| `for(;;){}` | `timeout` at 100 ms; VM reusable | instance killed at 103 ms; VM dead, the library panics instead of returning an error | same | 102 ms, reusable | 102 ms, reusable |
| `for(;;){try{for(;;){}}catch{}}` | 102 ms | 103 ms | same | 102 ms | 101 ms |
| backtracking regex `/^(a+)+(?=c)/` | 102 ms | 104 ms | same | **HANG** (killed at 90 s) | **HANG** (killed at 90 s) |
| `/^(a+)+$/` (RE2-compatible) | 102 ms | 104 ms | same | returns `false` in 2 ms (goja uses RE2 when it can) | same, 0.3 ms |
| native `sort` of 2 M strings ×50 | 110 ms | 103 ms | same | 102 ms | 102 ms |
| array bomb | `out-of-memory` at 41 ms; RSS 19.9 → 35.6 MiB | **not capped**: RSS 82 MiB → 1.09 GiB before the host watchdog | `out-of-memory` at 381 ms; RSS 83 → 195 MiB | **no cap**: RSS → 1.06 GiB before the watchdog | **no cap** (1.04 GiB) |
| `new Uint8Array(1<<30)` then `fill` | OOM at 1 ms | OOM at 3 ms (malloc fails) | OOM | **no cap** | **no cap** |
| unbounded `Map` of small objects | caught at 69 ms; the thrown value is `null` (OOM while allocating the error); RSS 40 MiB | thrown `null` only at 491 MiB RSS | thrown `null` at 209 MiB RSS | **no cap** | **no cap** |
| `s = s + s`, keeping copies | `InternalError: string too long` at 0.5 ms (rope strings) | OOM at 194 MiB RSS | OOM at 167 MiB RSS | **no cap** | **no cap** |
| JS recursion, default settings | **host process CRASH** (Go 1 GB stack limit) at 250 ms | wasm trap (out-of-bounds) at 3 ms; host survives, VM dead | same | heap grows to about 1 GiB, `timeout` at 20 s | same |
| JS recursion, configured | `InternalError: stack overflow` at depth 993 / 1,993 / 4,993 / 19,993 for 1k / 2k / 5k / 20k slots; RSS 38 / 55 / 130 / 456 MiB; **CRASH at 262,144 slots** | trap even at 16 KiB (the QuickJS stack check is inert in WASM) | same | error at 10,000 frames, which a JS `catch` does **not** see (the Go-side error escapes `try`) | same |
| parser nesting `[[[…]]]` ×1e5 | configured: `SyntaxError: stack overflow`; default: ok, 508 MiB RSS | trap | same | ok, 883 MiB RSS | ok, 499 MiB RSS |
| `new RegExp` nesting `(?:…)` ×1e5 | configured: `SyntaxError: stack overflow`; default: ok, 487 MiB RSS | trap | same | ok (RE2) | ok (RE2) |
| `JSON.parse` nesting ×1e6 | configured: `SyntaxError: stack overflow`; default: ok, **1 GiB RSS** | trap | same | ok, 916 MiB RSS | ok, 861 MiB RSS |
| `String(a)` / `Array.join` nesting ×1e6 | configured (1k–20k slots): `InternalError: stack overflow`; default: **CRASH** | trap | same | **CRASH** after ~50 s (any setting) | **CRASH** (any setting) |
| `JSON.stringify` nesting ×1e6 | configured (1k–20k slots): `InternalError: stack overflow`; default and 262,144 slots: **HANG** | trap | same | **HANG** (any setting) | **HANG** (any setting) |

Notes:
- "Configured" means:
  - modernc at 1,000–20,000 slots (`modernc-stack-slots.jsonl`) and at 262,144 slots (`all.jsonl`);
  - qjs at 16–256 KiB;
  - goja/sobek at `SetMaxCallStackSize(10000)`.
- modernc's slot limit must be set. The default and the "256 KiB" value (262,144 slots) both overflow Go's 1 GB goroutine stack, because each JS frame costs about 20 KiB of Go stack in the ccgo port. At 2,000 slots the depth is about 2,000 frames and the recursion probe peaks at 55 MiB RSS. The 150 MiB peaks in the join and stringify rows come from building the 1e6-deep array under a 256 MiB cap, not from the stack.

### Helper-process prototype (`cmd/spike/helper.go`)

The parent keeps no engine. Each worker is the same binary. At startup it sets `RLIMIT_CORE=0`, `FSIZE=0`, `NOFILE=16`, `CPU=120 s`, `debug.SetMaxStack(128 MiB)` and `SetMemoryLimit(256 MiB)`, clears its environment and does `chdir /`. The parent kills a worker at budget + 250 ms, or when its RSS passes 300 MiB (polled every 20 ms), and respawns it. Each request builds a fresh VM.

| Case (hostile budget 3 s) | modernc worker | goja worker |
|---|---|---|
| spawn + first eval | 15 ms | 16 ms |
| round trip, trivial module | mean 447 µs (p50 424, p99 769); in-worker 400 µs; **IPC overhead about 47 µs** | 698 µs (p99 2.3 ms) |
| DOM eval (document HTML shipped per request) | 0.79 ms | 0.95 ms |
| infinite loop | engine `timeout`; same worker | engine `timeout` |
| backtracking regex | engine `timeout`; same worker | **parent SIGKILL** at 3.25 s; respawn 17 ms |
| array bomb | engine `out-of-memory` at 40 ms | **parent RSS kill** at 301 MiB; respawn 17 ms |
| `Array.join` / `JSON.stringify` nesting | engine OOM or `stack overflow` | **parent RSS kill** |
| deep JS recursion | `InternalError: stack overflow` at depth 1,993 | engine error |
| host RSS across the whole run | 19.4 → 25.1 MiB | 19.6 → 27.4 MiB |

On macOS, `setrlimit(RLIMIT_AS/RLIMIT_DATA)` fails for a Go process, which the worker's lockdown line reports as `AS=…:false`. So on macOS the RSS poll is the only memory backstop. On Linux the backstop is cgroup v2 `memory.max`, or `RLIMIT_AS`.

### Performance and packaging

| | modernc | qjs | goja | sobek |
|---|---|---|---|---|
| host call crossing (`__dom_noop`) | 0.64 µs | **18.6 µs** | 0.17 µs | 0.18 µs |
| `el.textContent` (getter + host call) | 1.13 µs | 25.2 µs | 0.43 µs | 0.41 µs |
| `el.getAttribute("class")` | 1.37 µs | 29.6 µs | 0.43 µs | 0.42 µs |
| `document.querySelector` / `querySelectorAll` (3 hits) | 1.7 / 4.9 µs | 30.1 / 45.8 µs | 0.73 / 2.4 µs | 0.71 / 2.2 µs |
| pure-JS getter baseline | 0.23 µs | 0.74 µs | 0.22 µs | 0.21 µs |
| cold: first VM in a process (new + load + call) | 0.56 ms | **365 ms** (wazero compile, cacheable) | 2 ms (esbuild init) | 0.32 ms |
| fresh VM per evaluation (new + load + call + close) | 0.33 ms | 4 ms | 0.55 ms (includes an esbuild bundle) | 0.12 ms |
| warm call on a loaded VM (JSON marshal + timer arm) | 7.9 µs | 41.8 µs | 6.1 µs | 5.8 µs |
| RSS per live VM (50 live, module loaded) | 0.2 MiB | 0.6 MiB, over an ~85 MiB process baseline | 0.2 MiB | 0.1 MiB |
| binary delta, linux/amd64 stripped, over `fmt`+`net/http` (3.3 MiB) | +2.8 MiB | +3.7 MiB | +8.0 MiB (+12.1 with esbuild) | +8.2 MiB |
| binary delta over a binary already linking `modernc.org/sqlite` (7.2 MiB) | **+2.3 MiB** (shared libc) | n/a | n/a | n/a |
| `CGO_ENABLED=0` cross-builds (linux amd64/arm64, freebsd) | ✓ | ✓ | ✓ | ✓ |

The spike DOM surface is `querySelector(All)`, `textContent` get/set, `getAttribute`, `tagName`, `children`, `instanceof HTMLElement`/`Node`, and wrapper identity (`first === lis[0]`). It is correct on all four engines.

## Isolation design

### Process model

- `internal/jsrt` owns a pool of workers.
  - Size defaults to `GOMAXPROCS`; it is configurable per deployment.
  - A worker is `pagelike jsrt-worker`, started with an empty environment, `Dir=/`, its own process group, and only stdin/stdout/stderr open.
  - Each worker serves one evaluation at a time. Each evaluation runs on a fresh goroutine with a **fresh VM** (R1), so a bounded but deep Go stack is released afterwards.
- Workers are recycled:
  - after N evaluations;
  - when the RSS high-water in their response passes a threshold (the spike's worker reached 89 MiB after the hostile series);
  - after any kill.
- The host never links an engine into the request goroutines. A worker death is a failed evaluation, never a failed server.

### Protocol

- Length-prefixed frames on the pipes. The spike used JSON lines; production must not, because documents can be large.
- A request carries:
  - the module source, keyed by source hash so the worker can cache compiled bytecode;
  - the slot kind;
  - `this` and args, marshalled per R6/R7 in the dombase value encoding;
  - the remaining time and memory budget;
  - the needed schema descriptors (itemtype, parent, declared properties, methods, with JS method sources inline);
  - the document or element HTML and a writable flag.
- The response carries:
  - the variant (R6);
  - the value, or serialized element(s) or NodeList;
  - the serialized mutated `document` for a writable `default`;
  - elapsed time, a memory high-water, and a host-call count for tracing (`dombase_js.evaluate`).
- Callbacks from worker to host use the same pipe with request IDs. Only coarse operations call back:
  - a Sessel method implementation invoked from JS;
  - an `unknown-schema` lookup miss;
  - `Context.foo = …` write-back;
  - later, `crypto.subtle` if it is not implemented in-worker.

  Per-node DOM calls never cross.

### How each limit is enforced

| Limit | First line (engine, precise variant) | Backstop (OS / parent) |
|---|---|---|
| time | a `time.AfterFunc(remaining)` calls `vm.Interrupt()`, which is uncatchable and gives `timeout`. The variant is decided by the host-side "timer fired" flag, not by message text. Overshoot measured at 1–10 ms. | The parent SIGKILLs the process group at remaining + grace (250 ms) and reports `timeout`. `RLIMIT_CPU` bounds a worker's lifetime CPU. |
| memory | `SetMemoryLimit(remaining memory budget, default 16 MiB)` gives an `InternalError: out of memory`. A thrown `null` from an evaluation whose limit is set is also classified `out-of-memory` (quirk measured above). | Linux: a per-worker cgroup v2 `memory.max`, or `RLIMIT_AS`. macOS/dev: parent RSS polling. `debug.SetMemoryLimit` as a GC target. A kill gives `out-of-memory`. |
| stack | `SetMaxStackSize(≈2000 slots)`, calibrated with the harness against PageLove's 256 KB depth. It gives `InternalError: stack overflow` for JS, `Array.join` and `JSON.stringify` recursion, and `SyntaxError: stack overflow` for parser, `JSON.parse` and `RegExp` nesting. | `debug.SetMaxStack(128 MiB)` turns any unguarded recursion into a quick worker death, never a host death. When the worker's stderr shows a Go stack overflow, the parent maps the death to `threw` / `InternalError: stack overflow`; any other death is an internal error. |
| ambient I/O | The VM has no host functions except the ones jsrt registers. The module normalizer rejects every specifier except schema URLs (`import-not-allowed`), for static and dynamic `import()` alike. `SetDefaultModuleLoader` and `StdAddHelpers` are never called. | Linux: `PR_SET_NO_NEW_PRIVS` + a seccomp-bpf allowlist (read/write on fds 0–2, mmap/munmap/madvise, futex, clock_gettime, sched_yield, exit_group, rt_sig*), plus Landlock deny-all and an unshared network namespace where available. Also `RLIMIT_FSIZE=0` and `RLIMIT_NOFILE`. All of these are installable from pure Go. |

### DOM across the boundary

- The worker links `internal/dom` and `internal/selector`, so selector semantics are identical to the host's (ADR 0003).
- Node wrappers are JS classes, in the spike's `DOMPrelude` pattern. They are keyed by integer handles into the worker's node table, which gives the flattened-node / prototype-layer model of R7 and `instanceof` for free. The raw host function is captured in a closure and deleted from `globalThis`. Production should use private fields rather than a `Symbol` key.
- For read-only slots, the worker marks the nodes of the shipped tree read-only and throws `NoModificationAllowedError`. Constructed nodes are writable. Returned elements are serialized in the worker, and the host splices them.
- DOM operations are counted per evaluation and reported, so they can be charged to the composition budget.

### Schema imports

- The normalizer accepts exactly the schema itemtype URLs present in the request's schema descriptors. The loader returns generated module source: a class with `Symbol`-keyed backing storage, getters and setters for declared properties, `extends` for schema inheritance or `Map`, and methods whose JS bodies are inlined or whose Sessel bodies call back to the host.
- modernc's loader receives only the specifier, not import attributes. Until modernc exposes them, the worker checks the `type` attribute with a small lexical pre-scan of top-level `import` statements. Otherwise it would accept any attribute, which is a divergence (see Risks).

### Throughput plan

A fresh-VM evaluation costs about 0.4 ms including IPC (about 2,500/s per worker). For list pages with a per-item `@read`:
- batch every evaluation for one composition pass into one request;
- cache compiled bytecode per source hash in the worker;
- measure before relaxing "fresh context", because reusing a VM for the same source within one request would change what global mutation can observe.

## Consequences

- One engine family serves all server-JS slots and `j:` bindings. Error variants and stack shapes are QuickJS-native, which is what PageLove's documented errors appear to be.
- The serving process has no JS attack surface or failure mode. The cost is about 50 µs IPC per request, pool management, and platform-specific lockdown code, which is strongest on Linux.
- `modernc.org/libc` is shared with `modernc.org/sqlite`, so the binary grows by about 2.3 MiB.

## Risks

1. **Linux behaviour is unmeasured.** Docker was not running on the spike machine, so none of the following was exercised: `RLIMIT_AS` against Go's address-space reservations, cgroup v2 `memory.max`, seccomp, Landlock, and netns. Add a Linux CI job that runs `helper-bench` and the hostile probes before `internal/jsrt` lands.
2. **The ccgo port is not memory-safe against QuickJS bugs.** Transpiled C uses unsafe pointer arithmetic in the worker's address space, and modernc.org/quickjs and libc are essentially one maintainer's (cznic) work. The worker boundary contains this, but only with seccomp, Landlock and netns does a compromised worker lose filesystem and network access. On macOS the worker is resource-bounded, not privilege-separated.
3. **Stack calibration.** The slot limit is not bytes, and each slot costs about 20 KiB of Go stack. The mapping from PageLove's "256 KB" is empirical: record live the depth where PageLove throws, and pin it with a harness case.
4. **Missing engine hooks:**
   - no `JS_ComputeMemoryUsage` accessor (needed for `budget.memory_delta_bytes`);
   - no op-count hook for "ops charged at periodic interrupt" (approximate with CPU time);
   - no import attributes passed to the loader;
   - OOM while throwing yields `null`.

   Each needs a small upstream patch or a vendored shim; workarounds are listed above.
5. **Message text divergence.** The docs show V8-style messages; QuickJS's wording differs. Record live responses, and compare variants rather than messages in the harness.
6. **Per-evaluation cost.** Fresh VM plus IPC is 0.4–0.8 ms. Bindings on large lists need batching and bytecode caching, and possibly VM reuse within one source and request after checking against PageLove.
7. **Built-ins PageLove mentions that QuickJS lacks:**
   - `crypto.subtle` (the `j:` docs) must be provided in-worker, for example a Go `digest`;
   - `Intl` is absent, and `modernc.org/quickjs/intl` offers opt-in `DateTimeFormat`/`NumberFormat`;
   - `structuredClone` is absent.

   Each is a harness question: does PageLove have it?
8. **Bytecode format is tied to the QuickJS release** (changed 2026-07-27). Never persist compiled bytecode across upgrades; cache it in memory, per worker only.
9. **macOS development backstop.** RSS polling is coarse, at a 20 ms interval, and a fast allocator can overshoot the ceiling between polls. This is acceptable for development, not for production.
