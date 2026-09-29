# Sessel — language specification (area: sessel)

Normative behaviour of the Sessel expression/query language as pagelike must
implement it in `internal/sessel` (lexer, parser, interpreter) and expose it to
the host (QUERY, composition bindings, schema slots, reactions). The HTTP shell
of `QUERY … text/sessel` (status codes, media types, `entries` paging,
authorization) is owned by the protocol area (`docs/spec/protocol.md` §7–§8,
R-PROTO-70..85); this document owns what a program *means*.

Keywords MUST / SHOULD / MAY are used as in RFC 2119. Every requirement is
numbered `R-SESSEL-<n>` and carries an evidence level (`documented`,
`client-source`, `demo-source`, `inferred`), a source citation, and a
confidence. Contradictions are collected in §20 (`SC-n`), live probes in §21
(`SP-n`), harness cases in §22.

---

## 0. Sources and conventions

### 0.1 Documentation (snapshot 2026-09-28, `research/docs/2026-09-28/md/`)

The combined page `docs.pagelove.com_all_languages_sessel.md` was split at
its `# ` headings and diffed against every individual `languages_sessel*`
page: apart from the `<div class="header">` navigation wrappers the text is
identical, so the two are one source. The index page ends mid-sentence
("see the Concept pages:") in both copies.

| Tag | Page |
|---|---|
| [SES] | languages/sessel (index) |
| [GLS] | reference/glossary/sessel |
| [SES-Q] | languages/sessel/learn/querying |
| [SES-C] | languages/sessel/learn/collections |
| [SES-K] | languages/sessel/learn/construction |
| [SES-V] | languages/sessel/learn/composition |
| [SYN] | languages/sessel/reference/syntax |
| [TYP] | languages/sessel/reference/types |
| [NUM] | languages/sessel/reference/number |
| [STR] | languages/sessel/reference/string |
| [LST] | languages/sessel/reference/list |
| [DCT] | languages/sessel/reference/dictionary |
| [ELM] | languages/sessel/reference/element |
| [TMP] | languages/sessel/reference/temporal |
| [EXB] | reference/composing-pages/Expression-Binding |
| [JXB] | reference/composing-pages/JavaScript-Expression-Binding |
| [STP] | reference/composing-pages/Stamp |
| [MEL] | reference/composing-pages/Method-Elements |
| [RTE] | reference/composing-pages/Parameterized-Routes |
| [RSB] | reference/composing-pages/Resource-Binding |
| [XML] | reference/composing-pages/XML-Documents |
| [SXT] | reference/composing-pages/Selector-Extensions |
| [MTH] | reference/modeling-data/Methods |
| [PRP] | reference/modeling-data/Property |
| [RSV] | reference/modeling-data/Resolvers |
| [SCH] | reference/modeling-data/Schema |
| [GRP] | reference/permissions/Group |
| [TRG] | reference/reacting-to-changes/Trigger |
| [PRC] | reference/reacting-to-changes/Processor |
| [HRQ] | reference/reacting-to-changes/HTTPRequest |
| [TRH] | reference/reacting-to-changes/TransitionHandler |
| [QRY] | reference/protocol/QUERY |
| [ACR] | reference/protocol/Accept-Ranges |
| [REQ] | reference/reading-and-writing/Request-Document |
| [JSS] | languages/javascript/server/javascript-in-schemas (also in all_languages_javascript) |
| [BLG] | learn/build-a-blog |
| [RTD] | recipes/transforming-data |
| [RWH] | recipes/sending-a-webhook |

### 0.2 Upstream code (commits in `research/COMMITS.txt`)

| Tag | File |
|---|---|
| [POLLS] | pagelove-polls/site/admin/auth.html@c9270e5:104-149 (trigger `when`/`action` in Sessel) |
| [SHOP-AUTH] | pagelove-shop/site/admin-auth.html@d887054:22-57 |
| [SHOP-ROUTE] | pagelove-shop/site/products/:slug.html@d887054:13, orders/:id.html:14, admin/orders/:id.html:10, checkout.html:10, admin/settings.html:10 (`e:` bindings) |
| [ATS-OUT] | pagelove-ats/site/outbox/index.html@8f200fc:25-29 |
| [BJS-CORS] | beta-js/cors.html@c204746:19-39 (comment describing Sessel limits) |
| [LIVE] | earlier read-only probe `read-probes.json` case `query-sessel` (anonymous `QUERY text/sessel` → 401) |

No upstream repository contains a Sessel implementation; all evidence is docs
plus programs written in the language. Where the docs are silent on
date/time behaviour, [TMP] says the types "mirror JavaScript's Temporal API";
pagelike follows TC39 Temporal (ISO 8601 calendar only) in those gaps and
says so per requirement.

### 0.3 Terms

- **program** — the complete source text evaluated once (a QUERY body, the
  text of one `<script>` slot, one `e:` attribute value).
- **frame** — a `{ … }` block, a lambda body or the program body; holds `let`
  bindings.
- **current document** — the document whose root element `self` denotes
  (context dependent, §1).
- **site** — every stored document of the site the program runs in.
- **queried element** — an element obtained from stored documents; immutable.
- **constructed element** — built by `new`, `.clone()`, `Element.fromString`,
  `Document.parse`; mutable.
- **text representation** — the string used by `unique/subset/disjoint/contains/join`
  and interpolation (R-SESSEL-89).

---

## 1. Where Sessel runs (host contexts)

### R-SESSEL-1 — Evaluation contexts and their bindings

Every evaluation has a context table. pagelike MUST provide these bindings:

| Context (owner area) | `self` | `prior` (after `from`) | Other names | Result use |
|---|---|---|---|---|
| `QUERY` `text/sessel` on a document (protocol) | root element of the target document | `null` | `request` (the QUERY request) | encoded per R-PROTO-72/73 |
| `QUERY` on a directory (protocol) | unbound (reference → 416, R-PROTO-71) | `null` | `request` | same |
| `e:` expression binding (composition) | root element of the document being composed | `null` | earlier `r:`/`e:`/`j:` bindings of the element and its ancestors by name; `request` (`path`, `method`, `query`, `headers`, `params`, `auth`) | stored in `Context.<name>` for templates/stamps |
| Trigger `when` / `action` / `otherwise`, dynamic `HttpRequest` property (reactions) | unbound (inferred) | `null` | `Context` (`Context.request.*`); `Pagelove` writes allowed in `action` | `when`: truthiness; action: value discarded; property: String() |
| Processor `when` / `action` (reactions) | unbound (inferred) | `null` | `Context.request.*`, `Context.response.*` (read/write) | same |
| TransitionHandler `when` (reactions) | the Transition item element | `null` | no `Context.request`/`response` | truthiness |
| Property `default` (modeling) | unbound ("`self` is unavailable") | `null` | — | String() stored as the value |
| Property `@validate` (modeling) | List of the property's value elements | pre-write document root or `null` | — | must be truthy (R-MOD-45 says "anything other than `true`" fails; §20 SC-17) |
| Schema-level `@validate` (modeling) | the item element being written | pre-write document or `null` | — | truthy |
| `@write` / `@read` resolver (modeling) | List of the property's value elements (mutable working copies, R-SESSEL-242) | as above | — | Element or List of Elements |
| `@computed` / legacy bare-Sessel `@read` (modeling) | the Instance | — | — | the property value |
| Method `implementation` (modeling/composition) | the dispatched element (composition), the receiver Instance (in-VM call) or the Class (static) | — | declared parameters as named locals; `Context`; `messageName`, `parameters` for `doesNotUnderstand` | R-SESSEL-371 |
| Group subtype `includes(email)` (permissions) | the group Instance | — | `email` | truthiness |

- **Evidence:** documented except rows marked inferred. **Source:** [QRY]
  §Sessel mode; [SES-Q] §`self` and `prior`; [SYN] §Context variables;
  [EXB]; [TRG] §Context; [PRC] §Context; [TRH] §The `when` gate; [PRP]
  §Defaults, §Validators, §Computed properties; [SCH] §Schema-level
  `@validate`; [RSV] §The pipeline value; [MEL] §How the result becomes HTML;
  [MTH] §Implementation languages; [GRP] §Custom membership; [JSS] §method
  third context. **Confidence:** high (documented rows), low (inferred rows).
- **Edge cases:** `document` is an alias of `self` wherever `self` is the root
  of a document ([SYN] "alias for `self` in most contexts"); where `self` is
  not a document root (validators, resolvers, methods) `document` is the root
  of the document containing `self` (inferred), or unbound if none.
- **Cross-area:** protocol (QUERY), composition (bindings, method elements,
  stamps), modeling (slots), reactions (triggers/processors/handlers),
  permissions (Group `includes()`).

### R-SESSEL-2 — Source text extraction
- A `<script>` slot's program is the element's text content (HTML raw text,
  no entity decoding). An `e:NAME="…"` program is the attribute value after
  HTML attribute decoding (`&quot;` → `"`). Leading and trailing whitespace is
  insignificant.
- The language of a schema/reaction slot is chosen by the wrapper's
  `itemtype` (`https://pagelove.org/Sessel`, `…/Sessel/Lambda`) — not by the
  `<script type>`; a bare `<script type="text/sessel" itemprop="@validate">`
  (no wrapper item) is also Sessel (modeling R-MOD table).
- **Evidence:** documented. **Source:** [TRG] §`when` ("dispatch is by
  `itemtype`, not by the `<script>` tag"); [PRP] §Defaults; [SCH]; [EXB].
  **Confidence:** high.
- **Edge cases:** an `e:` value containing `"` inside a `"`-delimited HTML
  attribute is broken HTML (the [EXB] examples do exactly this, SC-14); authors
  use single quotes ([BLG] explains why). pagelike parses whatever the HTML
  parser produced; it MUST NOT try to repair it.

---

## 2. Lexical structure

### R-SESSEL-10 — Characters, whitespace, comments
- Source is Unicode text. Whitespace (U+0020, U+0009, U+000A, U+000D, U+000C)
  separates tokens and is otherwise insignificant, including newlines (a
  newline never terminates a statement by itself).
- `//` starts a comment running to the end of the line.
- `/* … */` block comments SHOULD be accepted (inferred; not shown in docs).
- Comments are not recognised inside string literals or selector literals.
- **Evidence:** documented (`//` appears inside Sessel sources in [SES-Q],
  [TYP], [TRG] §Authentication example, where a program body is *only* a
  comment after the declaration); inferred (`/* */`). **Confidence:** high /
  low (SP-19).

### R-SESSEL-11 — Identifiers and member names
- **Identifier** (variables, parameters, `let` names, dictionary keys, type
  and class names, function names): `[A-Za-z_][A-Za-z0-9_]*`. Case-sensitive.
- **Member name** (the token after `.` or `?.` in property access and method
  calls): an identifier that MAY contain interior hyphens — `-` is part of the
  name when it is immediately followed by a letter or `_`:
  `[A-Za-z_][A-Za-z0-9_]*(-[A-Za-z_][A-Za-z0-9_]*)*`. So `self.first-name`,
  `.org-id` are single member names, while `x.count - 1` and `x.count-1`
  (digit after `-`) are subtractions.
- **Evidence:** documented (identifiers `org_id`, `hmac_sha256`,
  `aboveThreshold`; member names `self.first-name`, `self.last-name`
  [PRP] §Computed properties; `.org-id` [SES-Q] §Typed search); inferred (the
  exact hyphen rule). **Confidence:** high (plain identifiers) / medium
  (hyphen rule).
- **Edge cases:** dictionary-literal keys cannot contain hyphens unquoted
  ([TYP] "use quotes when the key contains hyphens or spaces"); construction
  names (tags, classes, ids, attributes) use the CSS name rule of R-SESSEL-16.

### R-SESSEL-12 — Keywords
Reserved (cannot be identifiers or bare dictionary keys): `let if else try
catch return throw new from true false null isa`. Contextual (identifiers
elsewhere): `prior` (a document reference only directly after `from`,
including in a from-list), `text` (special only as `text:` inside a
construction body), `url` (only inside declarations), `self` and `document`
(context variables; also accepted as from-sources).
- **Evidence:** documented (`isa` reserved [SES-Q], [TYP]; `prior`
  contextual [SYN] §Block-level `from`); inferred (the complete list, from the
  grammar's terminals). **Confidence:** medium.
- **Edge cases:** `{ "isa": true }` must quote the key (documented).
  `let prior = 5; prior + 1` evaluates to `6`.

### R-SESSEL-13 — Number literals
- `INTEGER = [0-9]+` — a 64-bit signed integer; a literal outside
  `0..9223372036854775807` is a parse error (inferred).
- `FLOAT = [0-9]+ "." [0-9]+` — IEEE-754 double. A `.` not followed by a
  digit is not part of the number, so `5.abs()`, `42.Float()`, `0.Bool()` are
  an integer followed by a method call, and `3.7.floor()` is the float `3.7`
  followed by `.floor()`.
- No sign (negative numbers are unary minus), no exponent form, no leading
  `.5`, no hex, no digit separators (inferred; none appear in docs).
- **Evidence:** documented (literal table [NUM]; `5.abs()`, `3.7.floor()`,
  `42.Float()`, `0.Bool()`, `3.14.Integer()`); inferred (exclusions).
  **Confidence:** high / low (SP-20).

### R-SESSEL-14 — String literals
- Delimited by `"…"` or `'…'`; the two forms are equivalent.
- Escapes: `\"`, `\'`, `\\`, `\n` (LF), `\t`, `\r`, `\#` (a literal `#`,
  used to write `\#{` without interpolating). An unknown escape `\c` SHOULD
  produce the two characters `\c` unchanged (inferred; keeps regex sources
  such as `"\d+"` working). Raw newlines are allowed inside a literal.
- **Interpolation:** `#{ expr }` inside either quote style evaluates `expr` and
  splices its text representation (R-SESSEL-89); `null` splices the empty
  string. The expression extends to the matching `}` (nested braces, strings
  and selector literals balanced). A `#` not followed by `{` is literal.
- **Evidence:** documented (both quote styles and `'It\'s fine'` [STR];
  interpolation and `\#{` [SYN], [STR]; `"\n"` in [LST] reduce example;
  `"{\"path\": \""` [RWH]; `"^[^@]+@[^@]+\\.[^@]+$"` [PRP]); inferred
  (`\t`, `\r`, unknown escapes, interpolation in single quotes — the grammar
  shows only `"`). **Confidence:** high / low (SP-17, SP-18).

### R-SESSEL-15 — Selector literals
- `${` starts a selector literal; the literal ends at the matching `}`.
  While scanning, `(`/`)`, `[`/`]`, `{`/`}` nest, quoted strings (either quote)
  are skipped whole, and a nested `${ … }` is scanned recursively. The text
  between the delimiters, trimmed, is the **selector source**.
- The same scanner is used for sub-select `.${ … }` (R-SESSEL-211).
- **Evidence:** documented (`${div[data-id=host.${ [itemprop="id"] }.first().value()]}`
  [SYN] §Expression embedding; `s.${ li }` with inner spaces [LST]).
  **Confidence:** high.

### R-SESSEL-16 — Construction names
After `new`, and inside construction modifiers, names follow CSS
identifier syntax: tag `[A-Za-z][A-Za-z0-9-]*` (optionally `prefix|tag`);
class and id `-?[A-Za-z_][A-Za-z0-9_-]*`; attribute names
`[A-Za-z_:][A-Za-z0-9_.:-]*` (optionally `prefix|name`).
- **Evidence:** documented (`new div.card[aria-hidden="true"]`,
  `span.badge-green`, `data-count`, `p|transient`, `svg|svg`). **Confidence:**
  high (shapes shown) / medium (exact character classes).

### R-SESSEL-17 — Operators and punctuation
`+ - * / ! == != < > <= >= <=> && || ?? ? : ?. . , ; ( ) [ ] { } => ... = |`
plus `${` and `#{`. `%`, `**`, `++`, bitwise operators and compound
assignment do not exist; using them is a parse error.
- **Evidence:** documented (precedence table [SYN]); inferred (absence of
  others). **Confidence:** high / medium (SP-20).

---

## 3. Grammar

### R-SESSEL-30 — Reconstructed grammar (EBNF)

The grammar printed in [SYN] is incomplete (no assignment statement, lambdas
only as call arguments, `let` only as a statement, construction bodies too
strict). The following grammar accepts every program in the docs and demo apps
and MUST be implemented; deviations from [SYN] are marked `(*Δ*)` and
explained in R-SESSEL-31..40.

```ebnf
program        = { declaration } frame_body ;
declaration    = ( "@namespace" | "@schema" ) IDENT "url" "(" STRING ")" [ ";" ] ;   (*Δ ";" optional*)

frame_body     = { statement [ ";" ] } ;              (* value = last expression statement *)
statement      = let_stmt | assign_stmt | expr ;
let_stmt       = "let" pattern "=" expr ;
pattern        = IDENT
               | "{" field { "," field } [ "," ] "}"
               | "[" item  { "," item  } [ "," ] "]" ;
field          = IDENT [ ":" IDENT ] ;
item           = IDENT | "..." IDENT ;
assign_stmt    = target "=" expr ;                    (*Δ*)
target         = IDENT "." MEMBER { "." MEMBER }
               | IDENT "[" expr "]" ;

expr           = let_expr | lambda | return_expr | throw_expr | ternary ;  (*Δ*)
let_expr       = "let" pattern "=" expr ";" expr ;    (*Δ let as an expression*)
return_expr    = "return" [ expr ] ;
throw_expr     = "throw" expr ;
lambda         = params "=>" ( block | map_lit | expr ) ;   (*Δ*)
params         = IDENT | "(" [ param { "," param } ] ")" ;
param          = IDENT | "..." IDENT ;

ternary        = coalesce [ "?" ternary ":" ternary ] ;
coalesce       = or_expr  { "??" or_expr } ;
or_expr        = and_expr { "||" and_expr } ;
and_expr       = compare  { "&&" compare } ;
compare        = additive { ( "==" | "!=" | "<" | ">" | "<=" | ">=" | "<=>" ) additive
                          | "isa" type_name } ;
type_name      = IDENT { "." IDENT } ;
additive       = mult  { ( "+" | "-" ) mult } ;
mult           = unary { ( "*" | "/" ) unary } ;
unary          = ( "!" | "-" ) unary | postfix ;
postfix        = primary { postfix_op } ;
postfix_op     = "." MEMBER [ "(" [ args ] ")" ]
               | "?." MEMBER [ "(" [ args ] ")" ]
               | "[" expr "]"
               | "?." "[" expr "]"
               | "." SELECTOR ;                        (* sub-select *)
args           = expr { "," expr } ;

primary        = literal | selector | construct | if_expr | try_expr | from_block
               | map_lit | list_lit | "(" expr ")" | call | IDENT ;
call           = IDENT "(" [ args ] ")" ;
literal        = INTEGER | FLOAT | STRING | "true" | "false" | "null" ;
selector       = SELECTOR [ from_list ] ;
from_list      = "from" source { "," source } ;
                 (*Δ R-SESSEL-36: a "," continues the list only when the next token is
                    self, document, prior or a STRING literal *)
source         = "prior" | postfix ;                   (* self, document are IDENTs *)
from_block     = from_list block ;
map_lit        = "{" [ entry { "," entry } [ "," ] ] "}" ;
entry          = ( IDENT | STRING ) ":" expr ;
list_lit       = "[" [ expr { "," expr } [ "," ] ] "]" ;
if_expr        = "if" "(" expr ")" block { "else" "if" "(" expr ")" block } [ "else" block ] ;
try_expr       = "try" block "catch" "(" IDENT ")" block ;
block          = "{" frame_body "}" ;

construct      = "new" [ IDENT "|" ] NAME { modifier } [ cbody ] ;
modifier       = "." NAME            (* not when followed by "(" : then it is a method call *)
               | "#" NAME
               | "[" [ IDENT "|" ] NAME [ "=" attr_value ] "]" ;
attr_value     = STRING | EXPR_TEXT ;                 (* unquoted → expression, R-SESSEL-262 *)
cbody          = "{" [ citem { "," citem } [ "," ] ] "}" ;   (*Δ items may mix*)
citem          = "text" ":" expr | IDENT ":" expr | expr ;
```

- **Evidence:** documented ([SYN] §Grammar, §Construction grammar) +
  inferred (Δ items, each justified below). **Confidence:** high for the
  documented core; medium for the Δ items.

### R-SESSEL-31 — Statement separation
`;` between statements is optional ([SYN] grammar `(statement ';'?)*`).
Statements are delimited by the parser: an expression statement ends at the
first token that cannot continue it. A trailing `;` never changes a frame's
value. Because newlines are insignificant, `a\n(b)` is the call `a(b)` and
`x\n-1` is `x - 1`.
- **Evidence:** documented ([SYN]; [POLLS]:114-126 has two `if` statements
  with no `;` between them). **Confidence:** high.

### R-SESSEL-32 — `let` is both a statement and an expression
`let p = e; rest` is an expression whose value is `rest` evaluated with `p`
bound. It may appear wherever an expression may, including as an unbraced
lambda body: `products.map(el => let n = el.attr("data-name"); let p = …; new li { … })`.
Inside such a chain the body extends to the enclosing `)`, `,`, `]` or `}`.
- **Evidence:** documented ([SES-V] §`let` bindings "an expression, not a
  statement", §Putting it all together). **Confidence:** high.

### R-SESSEL-33 — Blocks versus dictionary literals
`{` in expression position is always a dictionary literal. Blocks appear only
after `if (…)`, `else`, `try`, `catch (…)`, a from-block's sources, and `=>`.
After `=>`, `{` begins a **dictionary literal** when the next tokens are
`IDENT ":"` or `STRING ":"`, otherwise a **block** (so
`p => { name: p.text(), price: p.attr("data-price") }` builds a dictionary and
`(a, b) => { let c = …; c }` is a block). `=> {}` is an empty block (value
`null`; inferred).
- **Evidence:** documented (both forms in [SES-C], [LST]); inferred (the
  lookahead rule, `=> {}`). **Confidence:** high / low (SP-16).

### R-SESSEL-34 — Lambdas anywhere
A lambda is an expression at the lowest precedence: argument
(`xs.filter(el => …)`), `let` value (`let f = el => …`), or statement
(`(el, i) => { … }`). `(` in primary position starts a lambda iff the matching
`)` is followed by `=>`; `IDENT =>` starts a single-parameter lambda. `() =>`
(no parameters) SHOULD be accepted (inferred). At most one `...rest`
parameter, and it must be last (parse error otherwise).
- **Evidence:** documented ([SYN] §Lambda expressions, §Rest parameters,
  §Closures, §`return` keyword). **Confidence:** high / low (`()`).

### R-SESSEL-35 — Assignment statements
Only these targets exist: `IDENT.m1(.m2)* = expr` and `IDENT[expr] = expr`.
A subscript after a dotted path (`Context.response.headers["X"] = …`) is a
parse error ("index assignment is not a valid assignment target"). Plain
`IDENT = expr` is a parse error (rebind with `let`).
- **Evidence:** documented (`product.name = …`, `headers["Content-Type"] = …`
  [DCT], [SYN]; `Context.response.body = …` [PRC]; `Context[messageName] = result`
  [MEL]); client-source ([BJS-CORS]:21-24). **Confidence:** high (forms shown)
  / medium (rejections).

### R-SESSEL-36 — `from` clause binding
- After `from`, the first source is `self`, `document`, `prior`, or a
  **postfix expression** (primary plus its postfix operators). Consequently
  trailing method calls are absorbed into the source:
  `${li} from self.count()` means `from (self.count())`. To call a method on
  the selector result, parenthesise: `(${li} from self).count()` — every doc
  example does this.
- A `,` after a source continues the source list only when the next token is
  `self`, `document`, `prior` or a string literal; otherwise the comma belongs
  to the enclosing list (call arguments, list literal, construction body).
  This makes `f(${a} from self, 2)` a two-argument call and
  `${x} from "/a/*", "/b/*"` a two-source query.
- **Evidence:** documented (`${[itemprop="related"]} from ${a.nav}.first().attr("href")`,
  multi-source examples [SYN] §Multi-source `from`); inferred (the comma rule).
  **Confidence:** medium / low (SP-25).

### R-SESSEL-37 — `new` parsing
- `new NAME` followed by modifiers; a `.NAME` immediately followed by `(` is
  **not** a class modifier but a method call on the constructed element:
  `new p.text("Hello")`, `new div.card.attr("id", "x")`,
  `new a[itemprop="url"].value("https://example.com")`.
- The body `{…}` is optional (grammar), so `new p` alone builds `<p></p>`.
  [SES-K] says void elements "still require" braces; pagelike MUST accept both
  (SC-9).
- After the construct, ordinary postfix operators apply:
  `new img {}.value("/images/banner.jpg").attr("alt", "Banner")`.
- **Evidence:** documented ([ELM] §Setting data examples; [SES-K] §Method
  chaining). **Confidence:** high / medium (no-brace form).

### R-SESSEL-38 — `isa`
`x isa T` is a comparison-level operator; `T` is a dotted type name
(`Temporal.PlainDate`, `Project`, `Class`) resolved like an identifier path
(R-SESSEL-60). It is left-associative with the other comparison operators:
`a isa T == true` is `(a isa T) == true`.
- **Evidence:** documented ([SYN] grammar and precedence; [TYP], [TMP]).
  **Confidence:** high.

### R-SESSEL-39 — Declarations first
`@namespace`/`@schema` must precede every statement; a declaration after a
statement is a parse error. Several declarations are allowed. The URL string
may use either quote style.
- **Evidence:** documented ([SES-V], [SYN] §Declarations). **Confidence:** high
  / low (quote style).

### R-SESSEL-40 — Parse errors
A program that does not match the grammar fails **before** evaluation with a
parse error; `try/catch` cannot catch it. Reporting per host: QUERY → 400
(R-PROTO-75), composition → request fails (500, [EXB] "fails to compile"),
`@validate` → 422 on every write, `@read`/`@write` → the step is skipped
([RSV]), trigger/processor → error response ([TRG] §Error handling),
TransitionHandler gate → handler never fires ([TRH]).
- **Evidence:** documented (per-host rows). **Confidence:** high.

### R-SESSEL-41 — Operator precedence

| Level (low → high) | Operators | Associativity |
|---|---|---|
| 0 | lambda `=>`, `let … ;`, `return`, `throw` | right |
| 1 | ternary `? :` | right |
| 2 | `??` | left |
| 3 | `||` | left |
| 4 | `&&` | left |
| 5 | `==` `!=` `<` `>` `<=` `>=` `<=>` `isa` | left |
| 6 | `+` `-` | left |
| 7 | `*` `/` | left |
| 8 | unary `!` `-` | right |
| 9 | postfix `.m()` `?.m()` `[i]` `?.[i]` `.p` `?.p` `.${…}` | left |

Consequences pinned by cases: `-5.abs()` is `-(5.abs())` = `-5`;
`(-5).abs()` is `5`; `1 + 2 * 3` is `7`; `a ?? b || c` is `a ?? (b || c)`;
`x > 0 ? "a" : "b"` needs no parentheses.
- **Evidence:** documented (levels 1–9 [SYN] §Operator precedence); inferred
  (level 0). **Confidence:** high.

---

## 4. Evaluation model

### R-SESSEL-60 — Name resolution
An identifier resolves, first match wins:
1. `let` bindings, lambda parameters, `catch` variables and method parameters,
   innermost frame outward (lexical scoping);
2. `@schema` / `@namespace` names of the program (a `let` of the same name
   shadows them);
3. host context names for the current context (R-SESSEL-1): `self`,
   `document`, `request`, `Context`, earlier binding names, `messageName`,
   `parameters`, and in authorization-rule expressions `auth`, `method`,
   `path`, `query` (permissions area);
4. type namespaces and built-in globals: `Number`, `Integer`, `Float`,
   `String`, `Bool`, `Null`, `Element`, `List`, `Map`, `Document`,
   `Temporal`, `Selector`, `Class`, `Instance`;
5. otherwise evaluation raises `RuntimeError` ("unresolved variable"),
   catchable by `try`.
- `let String = "hello"; String` → `"hello"`.
- `prior` outside a from-source is an ordinary identifier (step 1–5).
- **Evidence:** documented ([TYP] §Name resolution; [SYN] §Context variables,
  §Try/Catch "an unresolved variable" is a `RuntimeError`; [EXB] error table).
  **Confidence:** high (order of 1 vs 4, unresolved error) / medium (position of
  2 and 3).
- **Edge cases:** in composition the bare authorization-rule names
  (`auth.claims.email`, `method`, `path`, `query`) are **not** bound and raise
  "undefined variable" ([SYN] §Authenticated identity in composition).
  `Context` SHOULD be pre-bound in trigger/processor/method contexts even
  without `@schema Context` ([PRC] AuditLog example uses it undeclared, SC-13).

### R-SESSEL-61 — Frames, `let`, shadowing
- Every block, lambda body and the program body is a frame. `let` binds in the
  current frame for the remaining statements of that frame; bindings made
  inside an `if`/`try`/`catch`/from block are not visible after it.
- Re-`let` of an existing name in the same or an inner frame shadows it for the
  following statements (inferred); a lambda created earlier keeps seeing the
  binding it captured.
- `let` bindings are immutable except through property assignment on the
  bound dictionary/instance (R-SESSEL-71).
- **Evidence:** documented ([SES-V] "Bindings are evaluated in order. A later
  binding can reference an earlier one"; [SYN] §`if` "scoped to that body");
  inferred (re-`let`). **Confidence:** high / medium.

### R-SESSEL-62 — Frame value
A frame's value is the value of the last **expression statement** executed.
If the frame is empty, or its last statement is a `let` or an assignment, its
value is `null` (inferred). `return` ends the enclosing function frame early
(R-SESSEL-66). A trailing `;` does not change the value.
- **Evidence:** documented ([SYN] §`return` "The value of a block is its last
  expression", "A trailing semicolon … does not change the block's value");
  inferred (null cases). **Confidence:** high / low.

### R-SESSEL-63 — Closures
A lambda captures the frames visible at its definition (including `self` and
`let` bindings) **by reference**: property assignments made to a captured
dictionary after the lambda was created are visible when it runs. Parameters
shadow captured names. A lambda keeps working after its defining frame has
finished.
- **Evidence:** documented ([SYN] §Closures). **Confidence:** high.

### R-SESSEL-64 — Invoking lambdas
- Arguments bind positionally to parameters. Missing arguments bind `null`;
  surplus arguments are ignored unless a trailing `...rest` parameter collects
  them as a List (empty List when none).
- Built-in higher-order methods pass `(element, index, list)` to callbacks
  (`filter`, `reject`, `find`, `map`, `each`, `all`, `any`, `takeWhile`,
  `dropWhile`); `reduce`/`reduceRight` pass `(accumulator, element)` and MAY
  also pass `(index, list)` after them (inferred). `(el, ...extras) => extras`
  in `.map()` yields `[index, list]` per element.
- The **declared arity** (number of non-rest parameters) is observable: `sort`
  uses key mode for arity 1 and comparator mode for arity 2 (R-SESSEL-154).
- **Evidence:** documented ([SYN] §Multi-parameter lambdas, §Rest parameters;
  [LST]). **Confidence:** high (callbacks) / medium (missing → null).

### R-SESSEL-65 — Call expressions `IDENT(args)`
- If `IDENT` resolves to a lambda, call it (R-SESSEL-64).
- If it resolves to a type namespace, `T(v)` is the conversion `v.T()`:
  `String(date)` is `date.String()` (documented), `Number("4")`,
  `Integer(…)`, `Float(…)`, `Bool(…)` likewise (inferred).
- `size(x)` SHOULD be accepted as `x.count()` (a legacy spelling in [RSB]
  §Graph constraint; low).
- Any other value → `TypeError` (not callable); an unresolved name →
  `RuntimeError`.
- **Evidence:** documented (`String(date)` [TMP], [TYP]; stored lambdas "called
  from schema methods" [SYN]); inferred (rest). **Confidence:** medium.

### R-SESSEL-66 — `return`
`return e` ends the innermost **function frame** — the enclosing lambda body,
or the program — with value `e` (`return` alone → `null`). A `return` inside
an `if`/`try`/from block inside a lambda returns from the lambda.
- **Evidence:** documented ([SYN] §`return` example: `if (i > 10) { return null }`
  inside a lambda block). **Confidence:** high (lambda) / medium (program level).

### R-SESSEL-67 — `if`
Conditions are evaluated top to bottom; the first truthy one runs its block;
no later condition or block is evaluated. With no `else` and no truthy
condition the value is `null`. The value is the taken block's value.
- **Evidence:** documented ([SYN] §`if` expressions). **Confidence:** high.

### R-SESSEL-68 — from-blocks
`from S1, S2 { body }` evaluates `body` with the **default selector scope**
set to the union of the sources. Every bare selector literal lexically inside
the body (including inside lambdas defined there) uses it; a selector with its
own `from` overrides it. The from-block's value is the body's value.
- **Evidence:** documented ([SYN] §Block-level `from`; [SES-Q]). **Confidence:**
  high (override) / medium (lambdas inherit lexically).

### R-SESSEL-69 — Destructuring
- `let { a, b: c } = v` binds `a = v.a`, `c = v.b` using member access
  (R-SESSEL-75), so it works on Dictionaries, Elements and Instances; a missing
  key binds `null`.
- `let [x, y, ...r] = list` binds `x = list.at(0)`, `y = list.at(1)`,
  `r = list.slice(2)`; out-of-range positions bind `null`; `...r` must be last
  and unique (parse error otherwise).
- Destructuring `null` binds every name to `null` (and `...r` to `[]`)
  (inferred); a list pattern on a non-List value → `TypeError` (inferred).
- **Evidence:** documented ([SYN] §Destructuring). **Confidence:** high /
  low (null, non-list).

### R-SESSEL-70 — Evaluation order
Operands and arguments are evaluated left to right, each exactly once;
`&&`, `||`, `??`, `?:`, `if` and `?.` short-circuit; a dictionary literal
evaluates entries in source order; construction evaluates modifiers, then body
items, in source order.
- **Evidence:** inferred (standard; `.get(key, default)` "evaluates" its
  default lazily, R-SESSEL-187). **Confidence:** medium.

### R-SESSEL-71 — Property assignment
- `d.k = v` / `d[kexpr] = v`: `d` must be a let-bound Dictionary, an
  Instance, or `Context` (or a Dictionary reached through a dotted path under
  one of them, e.g. `Context.response.body`). The key is added if absent,
  replaced if present, keeping its original position.
- On an Instance the assignment goes through the schema (R-SESSEL-287).
- On `Context` it writes the shared per-request context (visible to later
  bindings/templates/stamps and, for `Context.response.*`, to the processor
  host).
- Any other target value → `TypeError`. The statement's value is `null`.
- **Evidence:** documented ([DCT] §Property assignment; [SYN]; [PRC]; [MEL]
  `Context[messageName] = result`; [TYP] `p.status = "archived"`); inferred
  (statement value, errors). **Confidence:** high / low.

### R-SESSEL-72 — Null propagation
A **method call** on `null` returns `null` without evaluating the method; this
applies to every method, including coercions (`null.Number()`, `null.String()`,
`null.Bool()` are `null`). Member access (`null.k`), subscript (`null[i]`) and
sub-select (`null.${…}`) on `null` also yield `null` (inferred). Null
propagation is not an error, so `try` does not see it.
- Arguments of a call whose receiver is `null` are not evaluated (inferred).
- **Evidence:** documented ([SES-C] §Null handling "Null propagates through
  subsequent method calls"; [SYN] §Try/Catch `try { null.text() } …` → `null`;
  [TYP] "Null propagates through coercion"). **Confidence:** high (methods) /
  medium (member, subscript, sub-select) / low (argument skipping).

### R-SESSEL-73 — Optional chaining
`a?.x…` evaluates `a`; if it is `null` the **entire remaining postfix chain**
is skipped and the result is `null` (no later calls, no argument
evaluation). Otherwise `?.` behaves like `.`. `?.` applies to method calls,
member access and subscripts (`?.[k]`); `?.${…}` does not exist (parse error;
use `??`).
- **Evidence:** documented ([SYN] §Optional chaining). **Confidence:** high.

### R-SESSEL-74 — Subscript `e[k]`
- Dictionary + String key → value or `null` when absent.
- List + Integer → like `.at(k)` (negative counts from the end; out of range →
  `null`) (negative index inferred).
- Element/Instance + String → member access with that name (R-SESSEL-75)
  (inferred).
- `Context.request.headers[name]` matches header names ASCII
  case-insensitively (R-SESSEL-293).
- Other combinations → `TypeError` (inferred).
- **Evidence:** documented ([SYN] §Subscript access; [DCT]). **Confidence:**
  high (dict, list) / low (others).

### R-SESSEL-75 — Member access `e.name` (no parentheses)
| Receiver | Result |
|---|---|
| Dictionary | value for key `name`, or `null` |
| Instance | the property through schema dispatch: computed → `@read` pipeline → cardinality shaping (R-SESSEL-286) |
| Element (not an Instance) | the element's microdata property `name`: `null` if absent, the value (String, or the nested item Element) if it occurs once, a List of values if several (R-SESSEL-232) |
| Temporal value | accessor (`date.year`, `dur.sign`, …) |
| Class | static member / static method reference (R-SESSEL-282) |
| `Context`, `request` objects | the member or `null` |
| anything else | `TypeError` |
- **Evidence:** documented (dictionary [DCT]; instances [TYP]; element dispatch
  "works with any receiver that supports message dispatch — Elements, Instances,
  Dictionaries" [SYN] §Destructuring; `self.path` on a Transition item [TRH];
  "a property read off an element returned by a selector query still gives you
  a plain value when there is exactly one … every value when there is more than
  one" [PRP] §Reading a multi-valued property). **Confidence:** high
  (dictionary, instance) / medium (element).

### R-SESSEL-76 — Method dispatch and unknown methods
`recv.m(args)` dispatches on the runtime type of `recv` (§7–§15). An unknown
method name on a non-null value raises `TypeError`, except on an Instance
whose schema declares `doesNotUnderstand` (called with `messageName` and
`parameters` = the argument List). Wrong argument count or types for a
built-in method raise `TypeError`.
- Methods and keys do not collide: `d.keys()` always calls the method, `d.keys`
  reads the key.
- **Evidence:** documented (doesNotUnderstand [MTH], [MEL]); inferred
  (TypeError). **Confidence:** medium / low (SP-34).

---

## 5. Operators

### R-SESSEL-80 — `+`
| Left | Right | Result |
|---|---|---|
| Integer | Integer | Integer; overflow of int64 → `RuntimeError` |
| Integer/Float | Float/Integer | Float |
| String | String | concatenation |
| any other combination | | `TypeError` with message `Cannot add <L> and <R>` |
`+` never coerces: `"hello" + 42` is `TypeError` "Cannot add String and
Integer"; use `.String()` or interpolation. Type names in the message:
`Integer`, `Float`, `String`, `Boolean`, `Null`, `List`, `Map`, `Element`,
the Temporal type name, `Instance`, `Class`, `Selector`, `Lambda` (only the
String/Integer text is documented).
- **Evidence:** documented ([SYN] §Try/Catch: `"hello" + 42` caught, message
  "Cannot add String and Integer"; [NUM]; [STR] §Concatenation; [SES-V] always
  converts with `.String()`); inferred (other names). **Confidence:** high.
- **Live 2026-09-29:** the message is `type error: cannot use '<v>' in
  arithmetic`, quoting the first non-numeric operand by its text (List items
  joined with `, `); numeric Strings are not coerced (`"5" + 1` → `'5'`).
  pagelike follows (docs/compat/decisions-2026-09-29/sessel.md).

### R-SESSEL-81 — `-`, `*`, `/`
- Numbers only (else `TypeError`). Integer ∘ Integer → Integer with overflow
  → `RuntimeError`; any Float operand → Float.
- `/` on two Integers truncates toward zero (`10 / 4` → `2`, `-7 / 2` → `-3`);
  with a Float operand it is float division (`10.0 / 4` → `2.5`).
- Division by zero (Integer or Float divisor `0`/`0.0`) → `RuntimeError`.
- **Evidence:** documented ([NUM] §Arithmetic operators; `1 / 0` →
  `"RuntimeError"` [SYN]); inferred (float divisor zero, `-7 / 2`).
  **Confidence:** high / medium.

### R-SESSEL-82 — Unary operators
`-x`: numbers only (`TypeError` otherwise); `-(Integer min)` →
`RuntimeError`. `!x`: Boolean negation of the truthiness of `x` (always
returns a Boolean).
- **Evidence:** documented (precedence table); inferred (details).
  **Confidence:** medium.

### R-SESSEL-83 — Equality `==`, `!=`
- Integer/Float compare numerically (`1 == 1.0` is `true`).
- Strings compare by code points (case-sensitive); Booleans and `null` by
  identity.
- Values of different types (other than Integer/Float) are unequal — no
  coercion (`1 == "1"` is `false`, `null == false` is `false`); never an error.
- Lists and Dictionaries compare structurally (same length/keys, pairwise `==`)
  (inferred).
- Elements are equal when they are the same node (same document and position,
  or the same constructed node) (inferred).
- Temporal values: same as `.equals()` (inferred).
- `!=` is the negation of `==`.
- **Evidence:** documented usage (`prior != null`, `range.matches(…) == false`
  [POLLS]:117, `p.path() == path` [POLLS]:145, string `==` everywhere);
  inferred (the rules). **Confidence:** medium (SP-9).

### R-SESSEL-84 — Ordering `<`, `>`, `<=`, `>=`, `<=>`
- Numbers: numeric. Strings: lexicographic by Unicode code point
  (`"2026-01-05" < "2026-03-01"` is `true`).
- `<=>` returns the Integer `-1`, `0` or `1`.
- If either operand is `null`, `<`,`>`,`<=`,`>=` yield `false` and `<=>`
  yields `null` (compat decision, low); a sort comparator returning `null`
  is treated as `0`.
- Other type combinations (number vs string, booleans, lists, elements) →
  `TypeError` (inferred). Same-type Temporal values MAY be ordered via their
  `compare` (inferred).
- **Evidence:** documented (numbers [NUM]; string `<=>` in sort comparators
  [SES-C], [LST]; string `<` on ISO dates [SCH] §Schema-level `@validate`);
  inferred (null, mixed). **Confidence:** high (numbers, strings) / low (SP-8).

### R-SESSEL-85 — Logical `&&`, `||`
Short-circuit on truthiness; the result is always a **Boolean** (`true` or
`false`), not an operand value: `"a" || "b"` is `true`, `0 && x` is `false`
without evaluating `x`.
- **Evidence:** documented ([TYP] "Boolean … Produced by comparison and logical
  operators"; truthiness governs `&&`/`||`); inferred (short-circuit).
  **Confidence:** medium (SP-10).

### R-SESSEL-86 — Null-coalescing `??`
`a ?? b` is `a` unless `a` is `null`, in which case `b` is evaluated and
returned. Only `null` triggers it (`0 ?? 5` → `0`, `"" ?? "x"` → `""`).
Left-associative: `null ?? null ?? 3` → `3`.
- **Evidence:** documented ([SES-Q], [SES-C] §Null handling, [SYN]).
  **Confidence:** high.

### R-SESSEL-87 — Ternary
`c ? a : b` evaluates `c` for truthiness and then exactly one branch;
right-associative (`x ? 1 : y ? 2 : 3`).
- **Evidence:** documented. **Confidence:** high.

### R-SESSEL-88 — `isa`
`v isa T` → Boolean:
| `T` | true when |
|---|---|
| a Class (`@schema` name) | `v` is an Instance/element whose `itemtype` equals the class URL or any schema whose parent chain reaches it (reflexive, like `:isa()`) |
| `Class` | `v` is a Class |
| `Instance` | `v` is an Instance (every user schema inherits `https://pagelove.org/Instance`) |
| `Temporal` | `v` is any Temporal value |
| `Temporal.X` | `v` is that Temporal type |
| `Number`/`Integer`/`Float`/`String`/`Bool`/`Null`/`List`/`Map`/`Element` | runtime type matches (`Number` matches Integer and Float; `Element` matches Instances too) (inferred) |
| `Selector` | `v` is a Selector (inferred) |
A `T` that does not resolve → `RuntimeError`; resolving to a non-type →
`TypeError`.
- **Evidence:** documented (`p isa Project`, `UserConfig isa Class`,
  `date isa Temporal.PlainDate`, `date isa Temporal`); inferred (primitive
  names). **Confidence:** high / low.

### R-SESSEL-89 — Text representation
Used by interpolation, `.join()`, `.unique()`, `.subset()`, `.disjoint()`,
`List.contains()`, `.String()` on non-strings, and `text:`/attribute values in
construction:
| Value | Text |
|---|---|
| String | itself |
| Integer | decimal, `-` sign when negative |
| Float | R-SESSEL-92 |
| Boolean | `true` / `false` |
| `null` | `""` in interpolation, `join`, construction; `null` from `.String()` (null propagation) |
| Element / Instance | its microdata value (`.value()`), `""` when that is `null` |
| Temporal | ISO 8601 canonical form (R-SESSEL-300) |
| Selector | its CSS source text |
| List / Dictionary | the `application/sessel+json` serialisation (inferred) |
- **Evidence:** documented (null → "" [SYN] §String interpolation; `.String()`
  "Always succeeds"; Temporal `String(date)` [TMP]; "compared by text
  representation" [LST]); inferred (element row: needed so that
  `(${[itemprop="tag"]} from self).subset(${[itemprop="allowed-tag"]} from …)`
  compares tag values across differently-attributed elements [SES-C]).
  **Confidence:** high (scalars) / low (element, list rows; SP-11).

---

## 6. Types and values

### R-SESSEL-90 — The value universe
Every value has exactly one runtime type: Integer, Float (both "Number"),
String, Boolean, Null, List, Dictionary (type namespace `Map`), Element
(queried or constructed; Document and Instance are Elements), Instance,
Class, Selector, Lambda, Blob, and the eight Temporal types
(`Temporal.PlainDate`, `PlainTime`, `PlainDateTime`, `Instant`,
`ZonedDateTime`, `PlainYearMonth`, `PlainMonthDay`, `Duration`).
- **Evidence:** documented ([TYP] §Type summary, §Document, §Blob); Lambda
  inferred (first-class lambdas [SYN] §Closures). **Confidence:** high.

### R-SESSEL-91 — Numbers
Integer = signed 64-bit, overflow is a `RuntimeError` (never wraps, never
silently becomes Float). Float = IEEE-754 binary64; no NaN or infinity can be
produced (operations that would produce them raise `RuntimeError`,
inferred). Expressions see one "Number" type but the Integer/Float
distinction is preserved and observable through `.String()`, division, JSON
encoding and `isa`.
- **Evidence:** documented ([TYP] §Number "the underlying representation
  tracks integer vs. float precision"; overflow listed under `RuntimeError`
  [SYN]); inferred (width, NaN). **Confidence:** high / medium (SP-21).

### R-SESSEL-92 — Float formatting
`Float.String()`: if the value is integral and `|x| < 1e16`, the integer digits
followed by `.0` (`42.Float().String()` → `"42.0"`, `100.0` → `"100.0"`);
otherwise the shortest decimal string that round-trips (`3.14`, `2.5`,
`0.30000000000000004`), switching to exponent notation (`1e16`, `1.5e-7`)
outside `1e-5 <= |x| < 1e16` (inferred, mirrors Rust `{:?}`). `-0.0` prints
`"-0.0"` (inferred).
- **Evidence:** documented (`42.Float() // 42.0` [NUM]; "Sum: 100.0" rendered
  from a Sessel `.sum()` in [EXB] §Examples); inferred (the rest).
  **Confidence:** medium (integral case) / low (exponent thresholds; SP-7).
- **Live 2026-09-29 (SP-7 settled):** the text form has no `.0` and no
  exponent: `42.Float().String()` → `"42"`, `1e16` → `"10000000000000000"`,
  `1.5e-7` → `"0.00000015"`, `-0.0` → `"-0"`. The `.0` form is the JSON
  encoding only (R-SESSEL-97); Liquid still renders a Float `100.0` (docs/compat/decisions-2026-09-29/sessel.md).

### R-SESSEL-93 — Truthiness
Falsy: `false`, `null`, `0`, `0.0`, `""`, `[]`, `{}`. Everything else is
truthy — including `"0"`, `"false"`, `-1`, every Element (even an empty one),
Temporal value, Instance, Class, Selector and Lambda. Used by `if`, `?:`,
`&&`, `||`, `!`, `.Bool()`, trigger/processor/handler `when` gates and Group
`includes()`.
- **Evidence:** documented ([TYP] §Truthiness; [TRG] §`when`). **Confidence:**
  high.

### R-SESSEL-94 — Conversion methods
| Method | String | Integer | Float | Boolean | other |
|---|---|---|---|---|---|
| `.Number()` | smart parse: `"42"` → 42, `"3.14"` → 3.14, else `null` | itself | itself | `null` (inferred) | `null` (inferred) |
| `.Integer()` | base-10 integer (optional leading `-`), else **TypeError** (SC-1) | itself | truncate toward zero | TypeError (inferred) | TypeError (inferred) |
| `.Float()` | decimal parse, else **TypeError** (SC-1) | widen | itself | TypeError (inferred) | TypeError (inferred) |
| `.String()` | itself | R-SESSEL-89 | R-SESSEL-92 | `"true"`/`"false"` | R-SESSEL-89 |
| `.Bool()` | truthiness | truthiness | truthiness | itself | truthiness |
- All are `null` on a `null` receiver (R-SESSEL-72).
- Numeric parse grammar (inferred): optional leading `-`, digits, optional
  `.` + digits; surrounding ASCII whitespace is **not** allowed; `"1e3"`,
  `".5"`, `"+1"`, `""` are not numbers. `"5.0".Integer()` fails (decimal
  part). `"42".Float()` → `42.0`.
- **Evidence:** documented ([TYP] §Type coercion; [NUM] §Number coercion;
  [STR] §Type coercion methods); inferred (grammar, non-string receivers).
  **Confidence:** high (documented cells) / low (whitespace, exotic forms).

### R-SESSEL-95 — Type namespaces
`Number`, `Integer`, `Float`, `String`, `Bool`, `Null`, `Element`, `List`,
`Map` (plus `Document`, `Temporal`, `Selector`) are first-class values giving
access to static methods: `Integer.random`, `Float.random`, `Number.random`,
`String.random`, `Element.fromString`, `Document.parse`, `Temporal.*`. They are
resolved after variables (R-SESSEL-60) and are truthy.
- **Evidence:** documented ([TYP] §Type Namespaces; [ELM]; [TYP] §Document).
  **Confidence:** high.

### R-SESSEL-96 — Value vs reference semantics
Elements, Instances and `Context` are references: every name bound to a
constructed element sees mutations made through any other name (e.g.
`let el2 = el.prepend(…)` returns the same element). Strings, numbers,
Booleans, Temporal values and Selectors are immutable values. Lists are
immutable (every list method returns a new list). Dictionaries are mutable only
through property assignment on a let-bound name; `let b = a` for a
dictionary SHOULD copy (value semantics) — unknown upstream (SP-32).
- **Evidence:** documented (element "link is live" [ELM] §`.parent()`;
  `.delete()`/`.merge()` return new dictionaries [DCT]); inferred (the rest).
  **Confidence:** high (elements) / low (dictionary aliasing).

### R-SESSEL-97 — JSON encoding (`application/sessel+json`)
The host encoding of a result value (QUERY, cross-area R-PROTO-73):
Integer → JSON integer; Float → JSON number (pagelike SHOULD keep `.0` for
integral floats, e.g. `100.0`); String/Boolean/null → JSON; List → array;
Dictionary → object in insertion order; Element → `{"$type":"element",
"$html": <outer HTML>, "$source": <document path>}` with `$source` omitted for
constructed elements; Instance → element form; Temporal → ISO string; Selector
→ its source string; Lambda, Class, Blob → `TypeError` at encoding time
(inferred, low).
- **Evidence:** documented (element tagging [QRY]); inferred (rest).
  **Confidence:** high / low (P-21 in protocol).
- **Live 2026-09-29:** integral Floats keep `.0` (`42.0`, `100.0`); outside
  `1e-5 <= |x| < 1e16` the form is `1e+16`, `1.5e-7`, `1e-6` (docs/compat/decisions-2026-09-29/sessel.md).

---

## 7. Number methods

All return `null` on a `null` receiver (R-SESSEL-72). Evidence: documented
([NUM]) unless noted. Confidence high unless noted.

### R-SESSEL-100 — `.abs()`
Absolute value, same numeric kind (`(-5).abs()` → `5`, `(-3.14).abs()` →
`3.14`). `Integer min .abs()` → `RuntimeError` (inferred).

### R-SESSEL-101 — `.floor()`, `.ceil()`, `.round()`
Return **Integer**. Integers are returned unchanged. `floor` rounds toward
−∞ (`3.7` → 3, `-3.2` → -4); `ceil` toward +∞ (`3.2` → 4, `-3.7` → -3);
`round` to nearest with ties away from zero (`3.5` → 4, `3.4` → 3, `-3.5` →
-4). A result outside int64 → `RuntimeError` (inferred).

### R-SESSEL-102 — `.String()`, `.Number()`, `.Integer()`, `.Float()`, `.Bool()`
Per R-SESSEL-94 (`3.14.Integer()` → 3, `42.Float()` → 42.0,
`${div.item}.count().String()` → `"4"`, `0.Bool()` → `false`).

### R-SESSEL-103 — `Integer.random(min, max)`
Uniform random Integer in the **inclusive** range `[min, max]`. Both
arguments must be Integers (`TypeError` otherwise). If `min > max` the bounds
are swapped; if `min == max`, `min` is returned.

### R-SESSEL-104 — `Float.random(min, max)`
Uniform random Float in the half-open range `[min, max)`; Integer arguments
are promoted. Swap / equal rules as above (equal → that value as Float).

### R-SESSEL-105 — `Number.random(min, max)`
Integer result (inclusive range) when both arguments are Integers; Float
result (half-open) when either is a Float. Swap / equal rules as above.
- **Edge cases (103–105):** non-numeric arguments → `TypeError`; randomness
  need not be cryptographic (inferred); `Temporal.Now`/random make constraint
  evaluation non-repeatable ([TMP] warns authors).

---

## 8. String methods

Indices and lengths count Unicode **code points** ("operating on characters",
inferred unit). Evidence documented ([STR]) unless noted.

### R-SESSEL-110 — `.contains(s)`, `.startsWith(p)`, `.endsWith(s)`
Case-sensitive substring / prefix / suffix tests → Boolean. Empty argument →
`true` (inferred). Non-String argument → `TypeError` (inferred).

### R-SESSEL-111 — `.matches(pattern)`
`true` when the regular expression `pattern` matches **anywhere** in the string
(unanchored search; authors anchor with `^…$`). Syntax: RE2/Rust-regex
compatible (no backreferences or lookaround; inferred). An invalid pattern →
`TypeError` (inferred).
- **Source:** [STR]; [POLLS]:117/122 (`range.matches("^selector=#responses$") == false`).
  **Confidence:** high (search semantics) / medium (flavour).

### R-SESSEL-112 — `.count()`, `.isEmpty()`
Number of code points; `isEmpty()` ⇔ `count() == 0`.

### R-SESSEL-113 — `.trim()`, `.lower()`, `.upper()`
`trim` removes leading/trailing Unicode whitespace; `lower`/`upper` are
Unicode case mappings (`"Hello World".lower()` → `"hello world"`).
**Aliases:** `.lowercase()` and `.uppercase()` MUST behave as `.lower()` /
`.upper()` (used by [RSV] and [RTD] resolver examples; SC-6).

### R-SESSEL-114 — `.replace(pattern, replacement)`
Replaces **all** non-overlapping occurrences of `pattern`, left to right.
pagelike treats `pattern` as a **literal** substring and `replacement` as
literal text (compat decision, low: the name "pattern" could mean a regex;
SP-6). Empty pattern → string unchanged (inferred).

### R-SESSEL-115 — `.split(delimiter)`
List of the substrings between occurrences of the literal delimiter
(`"a,b,c".split(",")` → `["a","b","c"]`; `"a,,b"` → `["a","","b"]`;
no occurrence → one-element list). Empty delimiter → list of single
code points (inferred).

### R-SESSEL-116 — `.slice(start[, end])`
Code-point substring `[start, end)`; `end` defaults to the length; negative
indices count from the end; indices are clamped to `[0, length]`; if
`start >= end` after resolution the result is `""`. Never an error for
out-of-range numbers. (`"hello".slice(1,3)` → `"el"`, `.slice(1)` → `"ello"`,
`.slice(-3)` → `"llo"`, `.slice(-3,-1)` → `"ll"`.)

### R-SESSEL-117 — Conversions
`.Number()`, `.Integer()`, `.Float()`, `.String()`, `.Bool()` per R-SESSEL-94.

### R-SESSEL-118 — `.sha256()`
Lowercase hex SHA-256 of the UTF-8 bytes (`"hello".sha256()` →
`"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"`).

### R-SESSEL-119 — `.hmac_sha256(key)`
Lowercase hex HMAC-SHA256 with the UTF-8 bytes of `key` as the key and the
receiver as the message (`"message".hmac_sha256("secret-key")` →
`"287a3bd8a4fc7731a94c722079055323644d8798bd291bf9878abc9b8fd4b1d0"`;
the argument order is inferred from the call shape).

### R-SESSEL-120 — `.bcrypt([options])`
bcrypt hash of the UTF-8 bytes in modular-crypt form `$2b$<cost>$<53 chars>`
(`$2a$`/`$2y$` acceptable; inferred), fresh random salt per call. Default
cost 12; `{ cost: n }` with `4 <= n <= 31`, else `TypeError` (inferred).
Input longer than 72 bytes: truncated per bcrypt (inferred).

### R-SESSEL-121 — `.argon2([options])`
argon2id hash in PHC string form `$argon2id$v=19$m=<KiB>,t=<iter>,p=<lanes>$<salt>$<hash>`
with a fresh random salt. Options: `memory` (KiB), `time` (iterations),
`length` (output bytes). Defaults (inferred, "sensible defaults"): `m=19456`,
`t=2`, `p=1`, length 32. Unknown option keys → `TypeError` (inferred).
- **Edge cases (118–121):** "server side only" ([STR]) — all pagelike Sessel
  runs server-side. No verify method is documented (SP-38); pagelike MAY add
  `.bcrypt_verify(hash)`/`.argon2_verify(hash)` only under a pagelike-namespaced
  name.

### R-SESSEL-122 — `String.random(length[, options])`
Cryptographically random string of `length` code points.
- Without options: alphabet `A-Za-z0-9`.
- Options map: named alphabets `upper` (A–Z), `lower` (a–z), `digits` (0–9),
  `symbols`, `alphanumeric` (A–Za–z0–9), `url_safe` (A–Za–z0–9 plus `-` `_`);
  each value `true` (include), `false` (exclude) or an Integer `n` (include and
  guarantee at least `n` characters from it). `chars: "…"` adds a custom pool,
  `chars_min: n` guarantees `n` of them.
- Algorithm: first draw each alphabet's minimum from that alphabet, then fill
  the remaining length from the union of all included alphabets, then shuffle
  uniformly.
- An options map that includes no alphabet (e.g. `{}` or only `false` values)
  → the default alphabet (inferred). Sum of minimums > `length`, negative
  length, or unknown option key → `TypeError` (inferred). `length` 0 → `""`.
- `symbols` alphabet (inferred, low; SP-37): `!#$%&*+-=?@^_~`.
- **Evidence:** documented (alphabets, algorithm, examples; the `@key`
  auto-default `String.random(1, { lower: true }) + String.random(7, { lower: true, digits: true })`
  [PRP]). **Confidence:** high / low (unspecified cells).

### R-SESSEL-123 — Undocumented string methods used by documented examples
pagelike MUST provide:
- `.slugify()` — lowercase; every maximal run of characters outside
  `[a-z0-9]` becomes one `-`; leading/trailing `-` removed; non-ASCII letters
  SHOULD be transliterated by stripping diacritics (NFKD, drop marks) before
  that (inferred).
- `.parseDateTime()` — parses an ISO 8601 date-time: with `Z`/offset →
  `Temporal.Instant`; with `[Zone]` → `ZonedDateTime`; without → `PlainDateTime`;
  date only → `PlainDate`; failure → `null` (inferred).
- **Evidence:** documented usage only ([RSV] §Generate a slug on write;
  [RTD] §Formatting a date on read: `el.value().parseDateTime()` then
  `.format("d MMMM yyyy")` renders `2026-04-08T09:15:00Z` as `8 April 2026`).
  **Confidence:** low (SC-6, SP-5).

---

## 9. List methods

Lists are ordered, immutable values; every method returns a new value. All
are `null` on a `null` receiver. Callback-taking methods accept any Lambda
(inline or stored). Evidence documented ([LST], [SES-C]) unless noted.

### R-SESSEL-140 — Literals
`[]`, `[1, 2, 3]`, trailing comma allowed, any element types, nesting allowed.

### R-SESSEL-141 — `.count()`, `.isEmpty()`
Length (Integer) / length is zero.

### R-SESSEL-142 — `.first()`, `.last()`, `.at(n)`
Element at 0 / at length−1 / at `n` (negative counts from the end: −1 last);
`null` when empty or out of range. Non-Integer `n` → `TypeError` (inferred).
**Compat:** `.first()` and `.last()` on a non-null, non-List receiver return
the receiver itself (so `md["email"].first()` works when a `microdata()`
value is a single String; SC-3; low).

### R-SESSEL-143 — `.slice(start[, end])`
Sub-list `[start, end)`, negative indices from the end, clamped, empty when
`start >= end` (same rules as strings, R-SESSEL-116; clamping inferred for
lists).

### R-SESSEL-144 — `.sum()`
For each item: an Element contributes its microdata value parsed with
`.Number()`; a String is parsed with `.Number()`; a Number is used as is;
anything else, and any unparseable value, is **skipped**. Result: Integer when
every contributing value is an Integer, else Float; `0` (Integer) for an empty
or all-skipped list (inferred). Integer overflow → `RuntimeError`.

### R-SESSEL-145 — `.min()`, `.max()`
Same value extraction as `.sum()`; the smallest / largest numeric value, keeping
its kind; `null` when no numeric value is present.

### R-SESSEL-146 — `.filter(cb)`, `.reject(cb)`
Keep items whose callback result is truthy / falsy. `cb(el, i, list)`.

### R-SESSEL-147 — `.find(cb)`
First item whose callback result is truthy, else `null`.

### R-SESSEL-148 — `.map(cb)`
New list of callback results, same length.

### R-SESSEL-149 — `.each(cb)`
Calls `cb` for every item for its side effects; returns the original list.

### R-SESSEL-150 — `.takeWhile(cb)`, `.dropWhile(cb)`
Leading items while truthy / the rest from the first falsy item
(`[1,2,3,10,4].takeWhile(n => n < 5)` → `[1,2,3]`; `dropWhile` → `[10,4]`).

### R-SESSEL-151 — `.all(cb)`, `.any(cb)`
Every / some callback result truthy; `all` of `[]` is `true`, `any` of `[]`
is `false`; both stop at the first decisive item (inferred).

### R-SESSEL-152 — `.unique()`
Removes items whose text representation (R-SESSEL-89) equals an earlier
item's; keeps first occurrences and their original values
(`[1, 2, 2, 3]` → `[1, 2, 3]`; `[1, "1"]` → `[1]`).

### R-SESSEL-153 — `.subset(other)`, `.disjoint(other)`, `.contains(v)`
Text-representation membership: `subset` — every item of the receiver appears
in `other` (`true` when the receiver is empty); `disjoint` — no item in
common (`true` when either is empty); `contains(v)` — some item's text equals
`v`'s text (`["admin","editor"].contains("admin")` → `true`). A non-List
`other` → `TypeError` (inferred).

### R-SESSEL-154 — `.sort([cb])`
Stable sort, new list.
- Arity-1 callback — **key mode**: items ordered by `cb(item)` keys using
  R-SESSEL-84 (numbers numerically, strings by code point); `null` keys sort
  first (inferred).
- Arity-2 callback — **comparator mode**: `cb(a, b)` returns a negative,
  zero or positive Number (`<=>` idiom); `null` → 0.
- No callback: natural order of the items by R-SESSEL-84 (inferred).
- Keys of incomparable types → `TypeError` (inferred).

### R-SESSEL-155 — `.reverse()`
Items in reverse order.

### R-SESSEL-156 — `.flatten()`
**Recursively** flattens nested lists (`[[1,2],[3,[4,5]]]` → `[1,2,3,4,5]`).

### R-SESSEL-157 — `.join(delimiter)`
Concatenation of the items' text representations separated by the
delimiter (`["a","b","c"].join(", ")` → `"a, b, c"`); `[]` → `""`.

### R-SESSEL-158 — `.reduce(...)`
- `reduce(initial, cb)` (documented order) and `reduce(cb, initial)` (JS
  order) are both accepted: the argument that is a Lambda is the callback. If
  both are Lambdas, the second is the callback (inferred).
- `cb(acc, el)` left to right; empty list → `initial` unchanged.
- `reduce(cb)` alone: the first item seeds the accumulator and folding starts
  at the second; an empty list → `RuntimeError` (inferred category).
- (`[1,2,3,4].reduce(0, (acc, el) => acc + el)` → `10`.)

### R-SESSEL-159 — `.reduceRight(initial, cb)`
As `reduce` but right to left (`["a","b","c"].reduceRight("", (acc, el) => acc + el)`
→ `"cba"`); the JS order and seedless form are accepted as for `reduce`
(inferred).

### R-SESSEL-160 — `.microdata()` on a List
Maps `.microdata()` over the items (R-SESSEL-233); non-element items → `null`.

### R-SESSEL-161 — Sub-select on a List
`list.${ sel }` runs the selector against each element's subtree and
concatenates the results in list order (≡ `list.map(el => el.${ sel }).flatten()`);
non-element items are skipped (inferred). See R-SESSEL-211.

---

## 10. Dictionary methods

Keys are Strings, insertion-ordered. Evidence documented ([DCT], [SES-V])
unless noted.

### R-SESSEL-180 — Literals
`{}`, `{ name: "Widget", price: 42 }`, `{ "Content-Type": "text/html" }`;
keys are identifiers or quoted strings; trailing comma allowed; nested values
of any type. A duplicate key: the later value wins at the first key's position
(inferred).

### R-SESSEL-181 — Access
`d.k` and `d["k"]` / `d[expr]` → value or `null` when absent.

### R-SESSEL-182 — Assignment
`d.k = v`, `d[kexpr] = v` on a let-bound dictionary (R-SESSEL-71); adds or
replaces.

### R-SESSEL-183 — `.keys()`, `.values()`, `.entries()`
Lists in insertion order; `entries()` yields `{ key: k, value: v }`
dictionaries.

### R-SESSEL-184 — `.contains(key)`
`true` iff the key is present (even with a `null` value; inferred).

### R-SESSEL-185 — `.count()`, `.isEmpty()`
Number of entries / no entries.

### R-SESSEL-186 — `.delete(key)`
New dictionary without `key`; the receiver is unchanged; missing key → equal
copy.

### R-SESSEL-187 — `.get(key, default)`
The value when the key is present (even if `null`, inferred), otherwise the
**lazily evaluated** `default` ("otherwise evaluates and returns default").
Both arguments are required: one argument → `TypeError`.
- **Confidence:** high / low (laziness, present-null).

### R-SESSEL-188 — `.merge(other)`
New dictionary: receiver entries, then `other`'s; on conflict `other` wins and
the key keeps the receiver's position (inferred ordering). Non-dictionary
`other` → `TypeError` (inferred).

---

## 11. Selector literals, scopes and the `from` clause

### R-SESSEL-200 — Selector literal value
`${ css }` evaluates to a **List** of queried elements (possibly empty), never
a single element and never `null`. Order: documents in scope order
(R-SESSEL-204), then document (pre-order tree) order.
- **Evidence:** documented ([SES-Q], [LST] §Selectors return lists).
  **Confidence:** high.

### R-SESSEL-201 — Selector language
The selector source is a CSS selector list evaluated by the shared selector
engine (cross-area: `internal/selector`, owned by reading-writing /
composition): Selectors Level 4 including `:has()`, `:is()`, `:where()`,
`:not()`, structural pseudo-classes, attribute operators `= ~= |= ^= $= *=`
with `i`/`s` flags, namespace prefixes, and the PageLove extensions
`:contains()`, `:equals()`, `:value-contains()`, `:value-equals()` (each with
optional `, i`), `:greater-than()`, `:less-than()`, `:value-greater-than()`,
`:value-less-than()`, `:only()`, `:isa()`, and the selector functions
`count()`, `text-of()`, `value-of()`, `attr-of()` — with the text
normalisation (NFC, trim, collapse) and numeric parsing rules of [SXT].
Selectors never descend into `<template>` content (inert boundary).
An invalid selector → `TypeError` when the literal is evaluated (inferred;
QUERY maps it to 400 via R-PROTO-75).
- **Evidence:** documented ([SYN] §Selector literals lists four
  pseudo-classes; [SXT] is the full set and is referenced from Sessel docs;
  inert boundary [ELM] §`.children()`, §`.content()`). **Confidence:** high
  (set) / medium (error category).

### R-SESSEL-202 — Expressions embedded in selectors
Inside a selector literal (and a sub-select), an **unquoted** attribute
value (all six operators) or an **unquoted** argument of an extended
pseudo-class (`:contains`, `:equals`, `:value-contains`, `:value-equals`,
`:greater-than`, `:less-than`, `:value-greater-than`, `:value-less-than`) is a
Sessel expression evaluated in the current scope; its text representation is
substituted as a quoted CSS string (CSS-escaped). Quoted values stay CSS
literals. Nested selector arguments (`:has(…)`, `:not(…)`, `:is(…)`) are
scanned recursively with the same rule.
- Extent: an attribute value expression ends at the `]` that closes the
  attribute selector (balanced); a pseudo-class argument ends at the matching
  `)`; a final top-level `, i` in the extended pseudo-classes is the case
  flag, and a trailing ` i`/` s` after an attribute value expression is the
  attribute flag.
- `null` substitutes `""` (inferred).
- **Compatibility fallback (SC-2):** when the unquoted text is a single
  identifier that does not resolve (R-SESSEL-60 steps 1–4), or is a CSS
  number/dimension token, or does not parse as a Sessel expression, pagelike
  MUST use the raw text as the CSS value instead of raising an error. This
  keeps `[itemprop=hostname]`, `[type=checkbox]` (construction),
  `[itemtype*=User]` and selector functions such as `[data-name=text-of(#title)]`
  working. A bound identifier always wins (so `[itemprop=price]` uses a
  variable named `price` when one is in scope — authors should quote).
- **Evidence:** documented ([SES-Q] §Expressions inside selectors; [SYN]
  §Expression embedding, including `${div[data-id=host.${ [itemprop="id"] }.first().value()]}`;
  `:value-equals(request.params.slug)` in [BLG] and [SHOP-ROUTE]); inferred
  (substitution form, flags, fallback). **Confidence:** high (rule) / low
  (fallback; SP-2).

### R-SESSEL-203 — Namespaces in selectors
`@namespace p url("…")` makes `p|tag` and `[p|attr]` usable in selector
literals (and construction). An undeclared prefix in a selector literal →
`TypeError` (inferred).
- **Evidence:** documented ([SYN] §`@namespace` `${svg|circle}`).
  **Confidence:** high / low.

### R-SESSEL-204 — Default scope: the whole site
A selector literal with no `from` clause, outside any from-block, queries
**every stored HTML document of the site**. Scope order: documents sorted by
path in ascending byte order (inferred; the same total order the reactions
area uses for triggers), then tree order.
- Documents are read as **stored** (raw) markup — not composed, no `@read`
  resolvers applied (queried elements are "snapshots of what the database
  holds"). Blobs and non-HTML resources are not searched (inferred). XML
  documents MAY be searched (inferred, low).
- The QUERY target does not narrow bare selectors (C-10 in protocol).
- **Evidence:** documented ([SES-Q] §The `from` clause "By default, a selector
  queries across the entire site — every document in the store"; [ELM]
  §Queried elements; [SYN]). **Confidence:** high (site scope) / low (order;
  SP-13).
- **Live 2026-09-29:** in a QUERY the default scope is the target document
  only (R-SESSEL-209); bindings and triggers keep the whole site.

### R-SESSEL-205 — `from` sources
Each source value selects roots to search:
| Source value | Roots |
|---|---|
| `self`, `document` | the current document (whole document, root included) |
| `prior` | the pre-mutation document, or nothing when `prior` is `null` |
| String without glob metacharacters | the stored document at that path; nothing if absent or not HTML |
| String with glob metacharacters (`* ? [ {`) | every stored document whose path matches the glob (R-SESSEL-206), in path order |
| Element that is a document root (`self`, `prior`, `.document()`, `Pagelove.GET`) | that whole document |
| other Element / Instance | that element's **descendants** (≡ sub-select, R-SESSEL-211) |
| List | the union of its items, each interpreted by this table |
| `null` | nothing (inferred) |
| anything else | `TypeError` |
A relative path string (no leading `/`) is resolved against the current
document's directory (inferred, low).
- **Evidence:** documented (table [SYN] §`from` clause; `${li} from s` with
  `s` an element ≡ `s.${ li }` [SES-C], [LST] §flatten); inferred (null,
  relative, root rule). **Confidence:** high (documented rows) / low (SP-26).
- **Live 2026-09-29:** in a QUERY only `from self` reaches a document; path,
  relative, glob, element, List and `document` sources match nothing
  (R-SESSEL-209).

### R-SESSEL-206 — Globs
Glob matching is the same anchored glob language as AuthorizationRule
`resource` (cross-area R-PERM-20: `*` matches any run of characters including
`/`, `?` one character, `[…]` classes, `{a,b}` alternation, `\` escape).
`"/products/*"` therefore also matches `/products/a/b.html`.
- **Evidence:** documented (`from "/products/*"` "all documents matching a
  glob pattern" [SES-Q]); inferred (shared engine). **Confidence:** medium.

### R-SESSEL-207 — Multi-source union
`from A, B, …` (inline or block) returns the concatenation of each source's
matches in source order; an element reached through more than one source
appears once, at its first position (inferred).
- **Evidence:** documented ("The result is the union of all matches" [SYN]).
  **Confidence:** high / low (dedupe).

### R-SESSEL-208 — `self`, `document`, `prior`
- `self` evaluates to the context value of R-SESSEL-1 (for documents: the root
  `<html>` element, a queried element with provenance).
- `prior` is the pre-mutation document root in write contexts and `null`
  otherwise, including when the document is being created. It is only a
  keyword directly after `from` (R-SESSEL-12); to test it, use a from-query:
  `(${x} from prior).count()`. The docs also show the bare test
  `prior != null`; pagelike SHOULD bind the identifier `prior` to the same
  value in write contexts so that test works (SC-12).
- **Evidence:** documented ([SES-Q] §`self` and `prior`; [SYN] §Context
  variables, §Block-level `from`). **Confidence:** high / low (bare `prior`).

### R-SESSEL-209 — Trust model
Selector literals and `.search()` read any document of the site regardless
of the caller's authorization (as resource bindings do). Stored programs are
trusted author code. Request-supplied programs (QUERY) are gated by the
protocol area (R-PROTO-74) and otherwise have the same reach (pagelike
decision recorded there).
- **Evidence:** documented for resource bindings ([RSB] §Security) and
  implied for Sessel ([SES-Q] "every document in the store"); inferred for
  QUERY. **Confidence:** medium.
- **Live 2026-09-29 (supersedes the QUERY part):** a request-supplied QUERY
  program sees only its target document: no other document, no provenance
  (`.path()`, `.document()`, microdata `@id` are null) and no `Pagelove.GET`
  ("unknown function: GET"). pagelike: `sessel.Env.DocumentOnly` (docs/compat/decisions-2026-09-29/sessel.md).

### R-SESSEL-210 — The Selector type
- `new Selector { "css" }` or `new Selector { selector: expr }` builds an
  inert Selector value (the class is built in; `@schema Selector
  url("https://pagelove.org/Selector")` names the same class).
- `.execute()` ≡ `${css} from self`; `.execute(path)` ≡ `${css} from path`;
  `.execute([p1, p2])` ≡ `${css} from p1, p2` (any R-SESSEL-205 source value is
  accepted).
- `.toString()` returns the CSS source text; `String(sel)` likewise.
- The CSS text of a Selector value is **not** subject to expression embedding
  (it is already a string; inferred).
- **Evidence:** documented ([SES-Q] §The Selector type; [TYP] §Selector;
  `new Selector { selector: parameters[0] }.execute()` [MEL]). **Confidence:**
  high / low (no embedding). **Contradiction:** SC-4 (the [MEL] example uses
  `execute()` to implement site-wide `r:` bindings).
- **Live 2026-09-29:** `new Selector {…}` builds the Selector type only where
  `@schema Selector url("https://pagelove.org/Selector")` imports it;
  otherwise it is a `<selector>` element (so `.execute()` is "unknown
  function: execute", LO-10). In a QUERY, `.execute(path)` matches nothing.

### R-SESSEL-211 — Sub-select `recv.${ css }`
Runs the selector in memory against the receiver's **descendants** (the
receiver itself never matches); combinators are relative to the receiver
(`product.${ > [itemprop="name"] }` = direct children, `product.${ h1 + p }`).
Works on queried and constructed elements (including those from
`Document.parse` and `.content()`), and on Lists (R-SESSEL-161). On `null` →
`null`. Results keep the provenance of the receiver.
- **Evidence:** documented ([ELM] §Sub-select). **Confidence:** high /
  medium (descendants-only).
- **Live 2026-09-29:** results are stored (immutable) elements even when the
  receiver is constructed; `.children()` returns live, mutable handles.

### R-SESSEL-212 — Provenance
Queried elements carry their source path and document; sub-selections and all
list operations preserve it; `.clone()`, embedding into a constructed tree,
and `.content()` produce elements without provenance (inferred for the last
two).
- **Evidence:** documented ([ELM] §`.path()`, §`.clone()`). **Confidence:**
  high / low.

---

## 12. Elements

### R-SESSEL-220 — Queried vs constructed
Queried elements are immutable snapshots; constructed elements are mutable.
Getter methods work on both. Setter and mutation methods (R-SESSEL-234..241)
on a queried element raise `TypeError` (inferred category), except inside
resolver pipelines (R-SESSEL-242). Documents and Instances are Elements.
- **Evidence:** documented ([ELM] §Two kinds of element). **Confidence:** high
  / low (error category).

### R-SESSEL-221 — `.text()`
Concatenation of all descendant text nodes in tree order (no markup, no
trimming, no whitespace collapsing; template content excluded); `null` when
that concatenation is empty.
- **Evidence:** documented ([ELM] §`.text()`); inferred (no normalisation —
  [ELM] §`.value()` tells authors to trim `.text()` themselves). **Confidence:**
  high / medium (SP-33).
- **Live 2026-09-29 (SP-33 settled):** never `null` (`""` when there is no
  text); a stored element's text is whitespace-collapsed and trimmed
  (`"  a\n  b "` → `"a b"`), a constructed element's is raw (docs/compat/decisions-2026-09-29/sessel.md).

### R-SESSEL-222 — `.value()`
| Element | Value |
|---|---|
| `a`, `area`, `link` | `href` attribute |
| `audio`, `embed`, `iframe`, `img`, `source`, `track`, `video` | `src` |
| `object` | `data` |
| `meta` | `content` |
| `data`, `meter` | `value` |
| `time` | `datetime`, or the text content when `datetime` is absent |
| all others | text content |
Values are **verbatim** (no trimming, URLs not resolved). A listed element
missing its attribute yields `""` (not its text); a text-content element (and
a `<time>` falling back to empty text) with empty text yields `null`.
- **Evidence:** documented ([ELM] §`.value()`; [SXT] §Text content vs
  microdata values). **Confidence:** high.
- **Live 2026-09-29:** a stored text-content element's value is collapsed
  and trimmed like `.text()` (R-SESSEL-221), and `null` when that is empty.

### R-SESSEL-223 — `.attr(name)`
Attribute value or `null` when absent; a present boolean attribute yields
`""`. Attribute-name matching is ASCII case-insensitive on HTML elements
(inferred). Namespaced attributes are addressed by their serialized name
(`"p:transient"`) (inferred).

### R-SESSEL-224 — `.path()`, `.document()`
Source document path (String) / root element of the source document; both
`null` for constructed elements.
- **Live 2026-09-29:** both `null` for every element of a QUERY program
  (R-SESSEL-209).

### R-SESSEL-225 — `.selector()`
A CSS selector that identifies the element within its tree:
1. if the element has an `id`: `#id`;
2. otherwise `tag:nth-child(n)` (n = 1-based position among the parent's
   element children), prefixed by the selector of its parent and ` > `;
   the walk stops at the nearest ancestor with an `id` (emitted as `#id`);
3. an element with no parent and no id is emitted as its bare tag name.
Examples: `#main > h1:nth-child(1)`, `#hero`, `(new div#panel {})` → `#panel`,
`(new span {})` → `span`, an `h1` under `<html><head/><body>` with no ids →
`html > body:nth-child(2) > h1:nth-child(1)` (inferred).
- **Evidence:** documented ([ELM] §`.selector()`); inferred (no-id ancestry,
  CSS escaping of ids). **Confidence:** high / low (SP-28).

### R-SESSEL-226 — `.clone()`
Deep, independent constructed copy; loses provenance.

### R-SESSEL-227 — `.children()`
List of direct child **elements** (text nodes excluded). Whether a
`<template>`'s children include its content is unspecified — use `.content()`.

### R-SESSEL-228 — `.parent()`
Parent element or `null` at the top of the tree or for a constructed root.
Links inside constructed trees are live; a child handle keeps its tree alive.

### R-SESSEL-229 — `.content()`
For `<template>`: List of the top-level elements of the template content,
parsed into a fresh fragment so that selectors and sub-selects work; `null`
for any other element.

### R-SESSEL-230 — `.getHTML()`, `.innerhtml()`
Inner HTML serialisation (R-SESSEL-247); `""` for an element with no
children. The two are identical.

### R-SESSEL-232 — Member access on elements
`el.name` reads the microdata property `name` of the item rooted at `el`
(elements with `itemprop~=name` inside `el`, not inside nested `itemscope`
elements, following `itemref`): none → `null`; one → its microdata value (a
nested item → that Element); several → List in tree order. When `el` has no
`itemscope` the same crawl is applied from `el` (inferred).
- **Evidence:** documented ([PRP] §Reading a multi-valued property "one
  route"; [TRH] `self.path`; [SYN] §Destructuring). **Confidence:** medium.

### R-SESSEL-233 — `.microdata()`
For an element with `itemscope`: a Dictionary with `"@type"` (the itemtype's
short name after its last `/`, as JSON-LD context inference in R-RW-41),
then each property in first-occurrence order (value per R-SESSEL-222; a
nested item → nested Dictionary; several values → List), then `"@id"` =
`<source path>#<id>` when the element has both provenance and an `id`.
Elements without `itemscope` → `null`. On a List: mapped (R-SESSEL-160).
- **Evidence:** documented ([ELM] §`.microdata()` example
  `{ "@type": "Product", "name": "Widget", "price": "29.99", "@id": "/products/widget.html#widget" }`);
  inferred (ordering, list shape). **Confidence:** medium. **Contradiction:**
  SC-3 (docs elsewhere call `.first()` on every property value).
- **Live 2026-09-29:** no `@id` in a QUERY program (no provenance,
  R-SESSEL-209).

### R-SESSEL-234 — Setters (constructed only), arity-overloaded
- `.text(x)` replaces all children with one text node (text representation
  of `x`; `null` → no text).
- `.value(x)` sets the tag's value attribute from the R-SESSEL-222 table
  (`href`, `src`, `data`, `content`, `value`, `datetime`), or replaces the
  text for other tags.
- `.attr(name, x)` sets the attribute (text representation); `null` removes
  it (inferred).
All return the receiver for chaining.
- **Evidence:** documented ([ELM] §Setting data; [SES-K] §Method chaining).
  **Confidence:** high / low (null).

### R-SESSEL-235 — `.append(child)`, `.prepend(child)`
Insert as last / first child. `child` may be an Element (a queried element is
deep-copied; a constructed one is moved), a String (text node), a List (each
item in order) or `null` (no-op) (inferred for non-element kinds). Return the
receiver.

### R-SESSEL-236 — `.setHTML(html)`
Replace the children with the HTML fragment parsed in the receiver's context;
return the receiver.

### R-SESSEL-237 — `.empty()`
Remove all children; return the receiver.

### R-SESSEL-238 — `.remove()`
Detach from the parent (no-op at a root); returns `null`.

### R-SESSEL-239 — `.replaceWith(r)`
Put `r` in the receiver's place in its parent; returns `r`.

### R-SESSEL-240 — `.insertBefore(ref)`
Receiver-first: inserts the **receiver** into `ref`'s parent immediately
before `ref`; returns the receiver. `ref` without a parent → `TypeError`
(inferred).
- **Evidence (235–240):** documented ([ELM] §Mutation methods). **Confidence:**
  high.
- **Live 2026-09-29:** on a stored element (a queried element or a sub-select
  result, R-SESSEL-211) `.remove()` is a no-op returning `null`;
  `.replaceWith()` is `TypeError` "type error: replaceWith() cannot be used on
  stored (immutable) elements"; the text setter is "type error: text(value)
  setter requires a constructed element, got element"; and a stored `ref` is
  "type error: insertBefore() reference must be a mutable child element, not
  a stored element". The docs' `el.${ li }.first()` references therefore
  fail; `el.children()` handles work (docs/compat/decisions-2026-09-29/sessel.md).

### R-SESSEL-241 — Resolver-style aliases
`.set_text(x)` ≡ `.text(x)` and `.set_attr(name, x)` ≡ `.attr(name, x)`
(setter forms). Required because [RSV] documents
`self.map((el) => el.set_text(el.text().trim().lowercase()))` and
`el.set_attr("data-org-name", org_name)`.
- **Evidence:** documented usage (SC-6). **Confidence:** medium.

### R-SESSEL-242 — Mutability inside resolver pipelines
In `@write`/`@read` pipelines the elements of `self` are working copies of
the property's value elements and are **mutable**; the pipeline result replaces
the originals (modeling R-MOD-49). Elsewhere, `self`/`prior` and all
selector results are immutable.
- **Evidence:** documented usage ([RSV]); inferred (the working-copy model).
  **Confidence:** medium.

### R-SESSEL-243 — `Element.fromString(html)`
Parses `html` as an HTML fragment and returns its first top-level element as a
constructed element. Empty input, or input with no element → `TypeError`.
(`Element.fromString("<p>hello</p>").getHTML()` → `"hello"`.) Further
top-level nodes are ignored (inferred).

### R-SESSEL-244 — `Document.parse(html)`
Parses an HTML string into a constructed tree and returns: the `<html>` root
when the input is a full document (doctype or `<html>` start); otherwise the
single top-level element of the fragment (`Document.parse("<div class='card'><p>Hello</p></div>").attr("class")`
→ `"card"`); with zero or several top-level elements, a constructed `<body>`
wrapping them (inferred). Never raises for malformed HTML (HTML parsing is
total; inferred).

### R-SESSEL-245 — Document values
`Pagelove.GET(path)` of an HTML resource returns a Document: the queried root
element (with provenance) plus `.metadata()` → Dictionary with `mimetype`,
`etag`, `created`, `modified`, `size`, `version` (value types inferred:
Strings, `size`/`version` Integers).

### R-SESSEL-246 — Blob values
`Pagelove.GET(path)` of a non-HTML resource returns a Blob: not an Element;
methods `.metadata()` and `.path()` only (others → `TypeError`).
- **Evidence (243–246):** documented ([ELM] §`Element.fromString`; [TYP]
  §Document, §Blob). **Confidence:** high (documented) / low (inferred parts).

### R-SESSEL-247 — Serialisation
Outer/inner HTML uses the WHATWG HTML fragment serialisation algorithm:
attributes in insertion order, void elements without end tags, text escaping
`&` `<` `>` and U+00A0, attribute escaping `&` `"` U+00A0. Boolean attributes
created by construction (`[required]`) serialise as the bare name
(`required`); `required=""` is equivalent and consumers MUST NOT distinguish
them. The body string of a Sessel `HTTPResponse` is parsed as HTML and
re-serialised, so `=>` in a body becomes `=&gt;` ([BJS-CORS]:27-32).
- **Evidence:** documented output samples ([SES-K]); client-source
  ([BJS-CORS]). **Confidence:** medium.

---

## 13. Element construction

### R-SESSEL-260 — `new tag …`
Creates a constructed element with the given tag name (serialised lowercase
for HTML elements). `new div {}` → `<div></div>`; `new p` (no body) →
`<p></p>` (R-SESSEL-37).
- **Evidence:** documented ([SES-K] §The `new` keyword). **Confidence:** high.

### R-SESSEL-261 — `.class` and `#id` modifiers
Each `.c` appends `c` to the `class` attribute (space-separated, in order),
the attribute being created at the position of the first class modifier;
`#i` sets `id` (a later `#` wins, inferred). Attribute order follows modifier
order: `new p.intro#main {…}` → `<p class="intro" id="main">`.
- **Evidence:** documented ([SES-K] §ID and classes). **Confidence:** high.

### R-SESSEL-262 — Attribute modifiers
- `[name]` → boolean attribute (present, empty value).
- `[name="literal"]` / `[name='literal']` → literal value.
- `[name=expr]` → the unquoted text up to the balancing `]` is a Sessel
  expression, evaluated and converted with the text representation; `null`
  (and `false`) omit the attribute; `true` makes a boolean attribute
  (inferred). The fallback of R-SESSEL-202 applies (`[type=checkbox]` →
  `type="checkbox"` when `checkbox` is unbound).
- `[p|name…]` → attribute serialised as `p:name` (the prefix need not be
  declared; inferred from [SES-K] which uses `p|` without `@namespace`).
- **Evidence:** documented ([SES-K] §Attributes, §Namespaced attributes,
  §Dynamic attribute values; [SYN] §Construction syntax). **Confidence:** high
  / low (null/true/false, fallback).

### R-SESSEL-263 — Body items
Items are comma-separated and processed in order:
| Item | Effect |
|---|---|
| `text: e` | appends a text node with the text representation of `e` (`null` → nothing); several `text:` items and mixing with children are allowed |
| Element (constructed) | appended |
| Element (queried) | a deep copy is appended ("embedded verbatim") |
| Instance | its element is appended |
| String | appended as a text node (`new p { "Hello, " + name + "!" }`) |
| Number / Boolean | appended as text (text representation; inferred) |
| List | each item appended by this table, **one level** only (a List inside the List → `TypeError`, inferred) |
| `null` | skipped (enables `cond ? new li {} : null` and `if` without `else`) |
| Dictionary / other | `TypeError` (inferred) |
| `name: e` (name ≠ `text`) on a plain element | `TypeError` (inferred; for Instances see R-SESSEL-285, for Selector R-SESSEL-210) |
- **Evidence:** documented ([SES-K] §Children, §Null skipping, §List
  flattening, §Mixing queried and constructed elements; string children in
  [MTH] and [MEL] examples; mixing `text:` with children [ELM] §`.empty()`).
  **Confidence:** high / low (inferred rows). **Contradiction:** SC-8 (the
  [SYN] construction grammar forbids mixing).

### R-SESSEL-264 — Void elements
`area base br col embed hr img input link meta source track wbr` serialise
without an end tag; body items given to them are ignored (inferred).
- **Evidence:** documented ([SES-K] §Void elements). **Confidence:** high /
  low (ignored children).

### R-SESSEL-265 — Namespaced elements
`new p|tag` with `@namespace p url(U)`: an element in namespace `U`. For the
SVG and MathML namespaces the element is serialised with its local name as
foreign content (`<svg width="200" …><circle …></circle></svg>`); for other
namespaces as `p:tag` (inferred).
- **Evidence:** documented ([SES-V] §`@namespace` declarations, SVG example).
  **Confidence:** low (serialisation; SP-39).

---

## 14. Schemas, classes, instances and platform objects

### R-SESSEL-280 — `@schema Name url("U")`
Binds `Name` (program-wide) to the Class for type URL `U`. The URL need not
have a Schema item on the host: construction then builds plain microdata
without defaults/validation, `.search()` still finds items with
`itemtype=U`, and no methods are available (inferred). Built-in URLs give
special classes whatever local name is chosen:
| URL | Class | Section |
|---|---|---|
| `https://pagelove.org/Context` | the per-request Context object | R-SESSEL-290 |
| `https://pagelove.org/HTTPResponse` | throwable response | R-SESSEL-294 |
| `https://pagelove.org/Pair` | key/value pair (`key`, `value`) | R-SESSEL-294 |
| `https://pagelove.org/1.0` | platform interface (`GET`, `PUT`, `DELETE`) | R-SESSEL-295 |
| `https://pagelove.org/Sessel` | reflection (`stored`, `properties`, `schemaOf`) | R-SESSEL-289 |
| `https://pagelove.org/Selector` | the Selector class | R-SESSEL-210 |
- Schema-typed construction, instance methods, reflection and the platform
  interface require the declaration ([SYN] §`@schema`); pagelike additionally
  pre-binds `Context` (R-SESSEL-60) and `Selector`.
- **Evidence:** documented ([SYN] §`@schema`; [TRG]; [PRC]; [TYP] §Reflection
  API; [MEL]). **Confidence:** high (table) / low (undeclared-URL behaviour).

### R-SESSEL-281 — Class values
`Name` evaluates to a Class: truthy, `Name isa Class` is `true`, text
representation is the type URL (inferred). Classes are resolved through the
host's schema registry (cross-area: modeling) including the inheritance chain
(`parent`; implicit parent `https://pagelove.org/Instance`).
- **Evidence:** documented ([TYP] §Class). **Confidence:** high.

### R-SESSEL-282 — Static methods
Methods declared with `static: true` are called on the Class
(`Name.m(args)`), with `self` bound to the Class; arguments bind to the
declared parameters by position and are visible as named locals.
- **Evidence:** documented ([TYP] §Class; [MTH] §Fields `static`).
  **Confidence:** medium.

### R-SESSEL-283 — `Class.search(criteria?, options?)`
Returns a List of Instances found across **all** stored documents (path
order, then tree order):
| `criteria` | Filter |
|---|---|
| omitted / `null` | all instances of the type |
| Dictionary | AND: each entry `k: v` requires `:has([itemprop="k"]:value-equals("<text of v>"))` |
| List of Dictionaries | OR of the AND-filters; `[]` → all instances; `[d]` ≡ `d` |
Values may be String, Integer, Float or Boolean (text representation:
`"true"`/`"false"`); other value types → `TypeError` (inferred).
`options` (Dictionary): only `"isa"` (Boolean, default `false`) — when `true`
the type test is `:isa(U)` (the type and every schema whose parent chain
reaches it), and each Instance keeps its **actual** schema. Any other option
key → `TypeError`; a non-Boolean `"isa"` → `TypeError` (inferred).
The `:has()` translation deliberately inherits CSS semantics: a matching
property inside a nested item also satisfies it.
- **Evidence:** documented ([TYP] §`.search()`; [SES-Q] §Typed search).
  **Confidence:** high.

### R-SESSEL-284 — `Class.construct(map)`
Same as `new Class { … }` with the map's entries as properties (for use when
the class is held in a variable: `let cls = UserConfig; cls.construct({ "org-id": "abc", theme: "dark" })`).
- **Evidence:** documented ([TYP]). **Confidence:** high.

### R-SESSEL-285 — Instance construction `new Name { … }`
- `Name` resolving to a Class builds an Instance: a constructed
  `<div itemscope itemtype="U">` (tag `div`; inferred from the materialised
  example in [PRP] §Primary key).
- Body items: `prop: value` adds a property value — String/Number/Boolean →
  `<meta itemprop="prop" content="text">`; Instance → that instance's element
  with `itemprop="prop"` added; Element → that element with `itemprop` added
  (inferred); List → one value per item (inferred); `null` → nothing. A
  property name may repeat (cardinality `0..n`, e.g. several `header:`
  Pairs). Positional items (no `name:`) are appended as element children.
- Schema defaults are filled for missing properties (including the `@key`
  auto-default `[a-z][a-z0-9]{7}`), then the instance is validated
  (cardinality, types); a violation raises an error (`TypeError`, inferred
  category).
- Children built for scalar properties and positional children are ordinary
  element children: `(new Foo { count: 5 }).children().first().parent().attr("itemtype")`
  → `"https://example.com/Foo"`.
- `new Name(…)` with parentheses is not Sessel syntax (the [JSS] page claims a
  Sessel form `new HTTPResponse(status, message)` exists; no Sessel example
  shows it; SC-15).
- **Evidence:** documented ([TYP] §Instance; [TRG] HTTPResponse/Pair examples;
  [JSS] "the same defaults and validation Sessel construction gives"; [PRP]
  §Auto-generated default). **Confidence:** medium.

### R-SESSEL-286 — Instance property read `inst.prop`
Through the schema: an `@computed` property evaluates its slot with `self` =
the instance; otherwise the stored values go through the `@read` pipeline
(ancestor-first) and are shaped by cardinality: explicit `0..n`/`1..n` → List
(`[]` when absent); otherwise the single value (or `null`). Undeclared
properties read as for plain elements (R-SESSEL-232).
- **Evidence:** documented ([TYP] §Instance; [PRP] §Reading a multi-valued
  property, §Computed properties; modeling R-MOD-20, R-MOD-44).
  **Confidence:** medium.

### R-SESSEL-287 — Instance property write `inst.prop = v`
Replaces the property's value elements with elements built from `v` (as in
R-SESSEL-285); writing a computed property → error (`TypeError`, inferred).
Changes affect the in-memory instance only; persisting requires
`Pagelove.PUT` (inferred).
- **Evidence:** documented ([TYP] `p.status = "archived"`; [PRP] computed
  write rejected). **Confidence:** medium / low.

### R-SESSEL-288 — Instance method calls `inst.m(args)`
Dispatch to the schema's (or an ancestor's) Method `m`: arguments bind to the
declared parameters **in declared order** (Sessel bodies read them as named
locals; missing → `null`); overloads with the same name are chosen by the
number of supplied arguments (the overload whose parameter count equals it;
inferred); `self` is the receiver; the body may read and write `Context`. A
typed Instance returned from a method gets defaults + validation as in
R-SESSEL-285. An undeclared name falls back to `doesNotUnderstand`
(`messageName`, `parameters` = argument List) or raises `TypeError`.
- **Evidence:** documented ([TYP] `p.escalate(2)`; [JSS] third dispatch
  context; [MTH] §Overloading, §`doesNotUnderstand`). **Confidence:** medium.

### R-SESSEL-289 — Reflection (`@schema Sessel url("https://pagelove.org/Sessel")`)
- `Sessel.stored(el, name)` → raw microdata value of the first `itemprop=name`
  element in the item (no methods, resolvers or computed properties), or
  `null` when there is none.
- `Sessel.properties(el)` → List of the distinct itemprop names of the item,
  in first-occurrence order.
- `Sessel.schemaOf(el)` → the `itemtype` attribute value, or `null`.
- **Evidence:** documented ([TYP] §Reflection API). **Confidence:** high
  (existence) / medium (first-value, ordering).

### R-SESSEL-290 — `Context`
A per-request mutable object shared by every binding and method of one
composition/reaction pass:
- `Context.request` — see R-SESSEL-291.
- `Context.response` — processors only: `status` (Integer), `body` (String,
  fully buffered), `headers` (Dictionary); all three readable, `status` and
  `body` writable by assignment (header writes: SC-7).
- Arbitrary keys: `Context.foo = v` and `Context[name] = v` create entries
  visible to later bindings/templates/stamps in the same subtree (composition)
  or to later triggers/processors (reactions). `e:` bindings write
  `Context.<name>`.
- Reading an absent key → `null`.
- **Evidence:** documented ([TRG] §Context; [PRC] §Context, §Reading and
  writing the response; [MEL]; [STP]; [JSS] third context). **Confidence:**
  high / medium (subtree visibility in composition, owned by composition).

### R-SESSEL-291 — The request object
`Context.request` (reactions, methods) and `request` (composition; also
`Context.request` there) expose:
| Member | Type | Notes |
|---|---|---|
| `method` | String | upper case |
| `path` | String | request path |
| `headers` | Dictionary | lower-case names; lookup case-insensitive (R-SESSEL-293) |
| `query` | Dictionary | decoded query parameters (String values) |
| `params` | Dictionary | parameterized-route captures (`request.params.id`) |
| `body` | String | raw request body (reactions) |
| `rawBody` | String | body as raw UTF-8 bytes (reactions) |
| `auth` | Dictionary | `claims` (Dictionary of OIDC claims), `username` (the `sub`), `roles` (List) |
For anonymous requests `request.auth.claims.email` and `request.auth.username`
are `null` (falsy, no error). Reading `request.auth`/`headers` marks a composed
page `Cache-Control: private` (composition area).
- **Evidence:** documented ([SYN] §Context variables, §Authenticated identity
  in composition; [TRG] §Context; [RTE]; [REQ]). **Confidence:** high
  (members) / medium (anonymous null).

### R-SESSEL-292 — Authorization-rule spellings
In authorization-rule expressions (permissions area) the bare names
`auth.claims.*`, `method`, `path`, `query.*` are bound; they are unbound
elsewhere (R-SESSEL-60).
- **Evidence:** documented ([SYN] §Context variables). **Confidence:** high.

### R-SESSEL-293 — Header lookup is case-insensitive
`Context.request.headers["Authorization"]` and `…["authorization"]` MUST both
find the `Authorization` header. Keys enumerate in lower case.
- **Evidence:** documented (`headers["Authorization"]` in [TRG]) +
  demo-source (`headers["range"]` [POLLS]:107,114; `headers["authorization"]`
  [SHOP-AUTH]:25,50) + documented lower-case names in [REQ]. **Confidence:**
  medium (SP-36).

### R-SESSEL-294 — `HTTPResponse` and `Pair`
`throw new HTTPResponse { status: n, message: s, body: s, header: new Pair { key: k, value: v }, … }`
raises a response-carrying error. Fields: `status` (default 500), `message`
(body fallback), `body`, `header` (repeatable Pair). Hosts: in triggers and
processors it ends the chain and becomes the response; in schema slots it
chooses the rejection status/message (modeling R-MOD-47); in composition and
QUERY it is an ordinary uncaught error (inferred, SP-31). The body string is
parsed as HTML and re-serialised (R-SESSEL-247).
- **Evidence:** documented ([TRG] §Chain termination, §Response headers);
  client-source ([BJS-CORS]); demo-source ([POLLS]:118,123,146;
  [SHOP-AUTH]:32,56). **Confidence:** high (triggers) / low (other hosts).

### R-SESSEL-295 — Platform interface (`@schema Pagelove url("https://pagelove.org/1.0")`)
- `Pagelove.GET(path)` → Document (HTML), Blob (other) or `null` when absent
  (inferred).
- `Pagelove.PUT(item, path)` — writes `item` (Instance or Element) as the
  document at `path` through the normal write pipeline (authorization as the
  current request's actor, schema validation, `@key` → root `id`); when `path`
  equals `Context.request.path` inside a trigger it **replaces the request body**
  instead (transient; discarded if the chain later throws). Side-effect writes
  are not rolled back by later failures. Return value: `null` (inferred).
- `Pagelove.DELETE(path)` — deletes the document (signature inferred).
- Only trigger/processor Sessel **actions** have a write provider;
  `PUT`/`DELETE` elsewhere → `RuntimeError` (inferred).
- **Evidence:** documented ([TRG] §Writing from triggers; [PRC] §Writing from
  processors; [TYP] §Document; [LST] §each; [PRP] §Primary key).
  **Confidence:** medium / low (signatures, return values).

### R-SESSEL-296 — Method-element parameters
When a Method runs from a method element, each declared parameter is a named
local bound from the element attribute of the same name (`null` when absent);
`doesNotUnderstand` receives `messageName` and `parameters` (element form:
List of `{ name, value }`; attribute form: List with one String).
- **Evidence:** documented ([MEL] §Passing arguments, §`doesNotUnderstand`).
  **Confidence:** high.

---

## 15. Temporal

Where not stated otherwise: ISO 8601 calendar only; behaviour follows TC39
Temporal; evidence documented ([TMP]); confidence high for the documented
examples and medium for TC39-derived gaps.

### R-SESSEL-300 — Common rules
- Values are immutable, truthy, and belong to `Temporal` and to their own type
  (`isa`). Accessors are members **without** parentheses (`date.year`);
  operations are methods.
- **Live 2026-09-29:** PageLove answers accessors only as zero-argument
  methods (`date.dayOfWeek()`, `duration.months()`; `PlainDateTime` has no
  `dayOfWeek()`); the property form is `null`. pagelike accepts both
  (keep-standard, `sessel.query.values.temporal-format.live`) (docs/compat/decisions-2026-09-29/sessel.md).
- `String(v)`, `v.String()` and `v.toString()` return the canonical ISO form
  (table below); JSON encoding uses the same string.
- `Temporal.X.from(v)` accepts an ISO string or a Dictionary of fields. An
  unparseable or out-of-range **string** raises an error; Dictionary fields out
  of range are **constrained** (e.g. day 31 in April → 30) (TC39 default).
  Error category: `TypeError` (inferred; SP-24).
- `Temporal.X.compare(a, b)` (static) → `-1`/`0`/`1`; `a.equals(b)` → Boolean.
- Arithmetic takes a `Temporal.Duration` (a Dictionary of duration fields
  SHOULD also be accepted; inferred).
| Type | Canonical string |
|---|---|
| PlainDate | `2026-03-23` |
| PlainTime | `14:30:00`, fractional seconds with trailing zeros removed (`14:30:00.5`) |
| PlainDateTime | `2026-03-23T14:30:00` (seconds always present) |
| Instant | `2026-03-23T14:30:00Z` (UTC, fraction trimmed) |
| ZonedDateTime | `2026-03-23T14:30:00-04:00[America/New_York]` |
| PlainYearMonth | `2026-03` |
| PlainMonthDay | `--03-23` |
| Duration | `P1Y2M3DT4H5M6S`, zero components omitted, `PT0S` for zero, leading `-` when negative |

### R-SESSEL-301 — `Temporal.PlainDate`
- `from("2026-03-23")`, `from({ year, month, day })`.
- Accessors: `year`, `month`, `day`, `dayOfWeek` (ISO: Monday 1 … Sunday 7),
  `dayOfYear`, `weekOfYear` (ISO week), `daysInMonth`, `daysInYear`,
  `inLeapYear`. (2026-03-23: 1, 82, 13, 31, 365, `false`.)
- `add(d)`/`subtract(d)`: calendar arithmetic, month-end constrained
  (2026-03-23 + P1M → 2026-04-23; − P7D → 2026-03-16; 2026-01-31 + P1M →
  2026-02-28, inferred).
- `until(other)`/`since(other)`: Duration in **days** (2026-03-23 until
  2026-12-31 → `P283D`; since 2026-01-01 → `P81D`; TC39 default largest unit).
- `with({ … })` replaces fields (`with({ day: 1 })` → 2026-03-01;
  `with({ month: 12 })` → 2026-12-23).
- `toPlainDateTime(time?)` (midnight by default: `2026-03-23T00:00:00`;
  with `PlainTime.from("09:00")` → `2026-03-23T09:00:00`);
  `toZonedDateTime("Europe/London")` → start of that day in the zone.
- `compare`, `equals`, `format`, `toLocaleString`, String.

### R-SESSEL-302 — `Temporal.PlainTime`
- `from("14:30:00")`, `from("14:30")`, `from("14:30:00.500")`, `from({ hour, minute, … })`.
- Accessors `hour`, `minute`, `second`, `millisecond`, `microsecond`,
  `nanosecond` (`14:30:45.123456789` → 14, 30, 45, 123, 456, 789).
- `add`/`subtract` wrap around midnight (inferred): 14:30 + PT2H30M → 17:00:00;
  − PT1H → 13:30:00.
- `until`/`since` → Duration in hours and smaller (14:30 until 18:00 →
  `PT3H30M`; since 09:00 → `PT5H30M`).
- `with({ hour: 9 })` → 09:30:00; `with({ minute: 0, second: 0 })` → 14:00:00.
- `toPlainDateTime(date)` → 2026-03-23T14:30:00.
- `format("HH:mm")` → `14:30`; `format("h:mm a")` → `2:30 PM`;
  `toLocaleString("en-US")` → `2:30:00 PM`; String `14:30:00`.

### R-SESSEL-303 — `Temporal.PlainDateTime`
- `from("2026-03-23T14:30:00")`, with fraction, or a Dictionary.
- Accessors: all PlainDate and PlainTime accessors.
- `add(P1DT2H)` → 2026-03-24T16:30:00; `subtract(PT30M)` → 2026-03-23T14:00:00.
- `until`/`since` → Duration with days as the largest unit (inferred TC39):
  2026-03-23T14:30 until 2026-03-30T09:00 → `P6DT18H30M`.
- `with({ hour: 9, minute: 0 })` → 2026-03-23T09:00:00.
- `toPlainDate()`, `toPlainTime()`, `toZonedDateTime(tz)`.
- `format("yyyy-MM-dd HH:mm")` → `2026-03-23 14:30`;
  `format("MMMM d, yyyy 'at' h:mm a")` → `March 23, 2026 at 2:30 PM`;
  `toLocaleString("en-GB")` → `23/03/2026, 14:30:00`.

### R-SESSEL-304 — `Temporal.Instant`
- `from("…Z")` (an explicit `Z` or offset is required), `fromEpochSeconds(n)`
  (`1` → 1970-01-01T00:00:01Z). `fromEpochMilliseconds`/`…Nanoseconds` MAY
  exist (inferred).
- Accessors `epochSeconds`, `epochMilliseconds`, `epochMicroseconds`,
  `epochNanoseconds` (Integers; 1, 1000, 1000000, 1000000000 for
  1970-01-01T00:00:01Z).
- `add`/`subtract` accept only hours and smaller units (calendar units →
  error, inferred TC39); `add(PT1H)` of 14:30Z → `2026-03-23T15:30:00Z`.
- `until`/`since` → Duration with **hours** as the largest unit
  (documented `PT3H30M`, `PT2H30M`; TC39 would give seconds — SC-10).
- `toZonedDateTimeISO(tz)`.
- `format(p)` formats in UTC (`format("yyyy-MM-dd HH:mm 'UTC'")`);
  `toLocaleString(l)` formats in UTC (inferred); String `2026-03-23T14:30:00Z`.

### R-SESSEL-305 — `Temporal.ZonedDateTime`
- `from("2026-03-23T14:30:00[Europe/London]")`, with offset
  `…+05:30[Asia/Kolkata]` (an offset inconsistent with the zone → error,
  inferred). IANA zone names; `UTC` allowed.
- Accessors: date/time fields, `timeZoneId`, `offset` (`"-04:00"` for
  New York on 2026-03-23, DST).
- `add`/`subtract`: calendar units on the wall clock, time units on the
  timeline (TC39).
- `until`/`since` → Duration with hours as the largest unit (TC39 default).
- `with({ hour: 9, minute: 0 })` → `2026-03-23T09:00:00-04:00[America/New_York]`;
  `with({ timeZone: "Europe/London" })` keeps the wall-clock fields and
  re-interprets them in the new zone (a documented extension; TC39 has no
  such key).
- `withTimeZone(tz)` keeps the instant. `toInstant()`, `toPlainDate()`,
  `toPlainTime()`, `toPlainDateTime()`.
- `compare` compares instants; `equals` requires the same instant **and**
  the same zone.
- `format("yyyy-MM-dd HH:mm z")` → `2026-03-23 14:30 EDT`; String
  `2026-03-23T14:30:00-04:00[America/New_York]`.
- Wall-clock times in a DST gap/overlap resolve with TC39 `compatible`
  disambiguation (inferred).

### R-SESSEL-306 — `Temporal.Duration`
- `from("P1Y2M3DT4H5M6S")`, `"P1M"`, `"PT30M"`, `"P7D"`, `"-PT1H"`, weeks
  `"P2W"`, fractional seconds `"PT1.5S"`; `P` required, `T` required before
  time units. Dictionary form with keys `years months weeks days hours minutes
  seconds milliseconds microseconds nanoseconds` (unknown keys, mixed signs →
  error).
- Accessors: the ten fields, `sign` (1/−1/0), `blank` (all zero).
- `with({ … })` replaces the given fields (`P1Y2M3DT4H5M6S.with({ years: 2, hours: 0 })`
  → `P2Y2M3DT5M6S`).
- `add(other[, relativeTo])`, `subtract(other[, relativeTo])`: if either
  operand has years, months or weeks a `relativeTo` (PlainDate,
  PlainDateTime or ZonedDateTime) is required (error otherwise); the result is
  balanced up to the larger of the operands' largest units
  (`P1M.add(P1M, 2026-01-31)` → `P2M`; `PT2H.add(PT30M)` → `PT2H30M`).
- `total(unit[, relativeTo])` → Number of `unit`s (`"days"` etc., singular
  accepted; `P1Y.total("days", 2026-01-01)` → 365); calendar units need
  `relativeTo`. Integer when integral, else Float (inferred).
- `negated()` (`P1M` → `-P1M`), `abs()` (`-PT2H` → `PT2H`).
- `equals` is component-wise (`P30D` ≠ `P1M`).

### R-SESSEL-307 — `Temporal.PlainYearMonth`
- `from("2026-03")`, `from({ year, month })`.
- Accessors `year`, `month`, `daysInMonth`, `daysInYear`, `inLeapYear`.
- `add(P3M)` → `2026-06`; `subtract(P1Y)` → `2025-03`.
- `until`/`since` → Duration in years and months (2026-03 until 2027-01 →
  `P10M`; since 2026-01 → `P2M`).
- `with({ month: 12 })` → `2026-12`; `toPlainDate(15)` → `2026-03-15`
  (a bare day number; `{ day: 15 }` SHOULD also work).
- `format("MMMM yyyy")` → `March 2026`; String `2026-03`.

### R-SESSEL-308 — `Temporal.PlainMonthDay`
- `from("--03-23")`, `from({ month, day })`.
- Accessors `month`, `day`.
- `with({ day: 1 })` → `--03-01`; `toPlainDate(2026)` → `2026-03-23` (a bare
  year; `--02-29` in a non-leap year constrains to the 28th, inferred).
- `equals`, `format("MMMM d")` → `March 23`, String `--03-23`. No `compare`
  (TC39 has none; inferred).

### R-SESSEL-309 — `Temporal.Now`
`instant()`, `zonedDateTimeISO(tz?)`, `plainDateISO(tz?)`,
`plainTimeISO(tz?)`, `plainDateTimeISO(tz?)`. Non-deterministic. The "system
timezone" is UTC unless the site configures one (pagelike decision, low).
Within one evaluation, repeated calls MAY return different values.

### R-SESSEL-310 — `.format(pattern)`
CLDR-style tokens; a token is a maximal run of one pattern letter:
| Token | Meaning | Token | Meaning |
|---|---|---|---|
| `yyyy` | 4-digit year | `yy` | 2-digit year |
| `MMMM` | full English month name | `MMM` | short English month name |
| `MM` | 2-digit month | `M` | month number |
| `dd` | 2-digit day | `d` | day number |
| `HH` | 24-hour, padded | `H` | 24-hour |
| `hh` | 12-hour, padded | `h` | 12-hour (0 → 12) |
| `mm` | minutes, padded | `ss` | seconds, padded |
| `a` | `AM`/`PM` | `'…'` | literal text (`''` = a quote) |
| `z` | zone abbreviation (`EDT`, `GMT`, `UTC`; `GMT±h[:mm]` when none) | | |
Non-letters are copied. Other letters: pagelike SHOULD support `EEEE`/`EEE`
(weekday names), `m`, `s`, `SSS`, and MUST raise `TypeError` for the rest
(inferred). Tokens that the value lacks (time tokens on a PlainDate, date
tokens on a PlainTime) → `TypeError` (inferred). English month names only.
- **Evidence:** documented (table and examples, `z` from the ZonedDateTime
  example). **Confidence:** high (table) / low (extensions, errors).

### R-SESSEL-311 — `.toLocaleString(locale)`
Locale-aware formatting equivalent to ECMA-402 `toLocaleString(locale)` of the
corresponding JavaScript Temporal value with default options (numeric date
and/or time components): PlainDate `en-US` → `3/23/2026`, `de-DE` →
`23.3.2026`, `ja-JP` → `2026/3/23`; PlainTime `en-US` → `2:30:00 PM`;
PlainDateTime `en-GB` → `23/03/2026, 14:30:00`. The space before `AM`/`PM`
may be U+0020 or U+202F (CLDR version dependent); tests MUST accept both.
Missing argument → `en-US` (inferred).
- **Evidence:** documented (examples; "ICU4X locale-aware formatting").
  **Confidence:** high (examples) / low (other locales; SP-23).

### R-SESSEL-312 — Temporal HTTP examples
The four "Testable examples" in [TMP] show rendered pages
(`/temporal-ref/date-accessors.html` → "Year: 2026, Month: 3, Day: 23",
"Day of week: 1", "Leap year: false"; `date-arithmetic.html` →
"2026-04-23", "2026-03-16", "2026-03-01"; `instant.html` → "Epoch seconds: 1",
"Plus one hour: 2026-03-23T15:30:00Z", "Compare: -1"; `duration.html` →
"P1Y2M3DT4H5M6S", "Years: 1", "Sign: 1", "Negated: -PT1H", "Blank: true";
`zoned.html` → "Timezone: UTC", "New York hour: 6", "Date: 2026-03-23";
`formatting.html` → "2026-03-23", "23/03/2026", "March 23, 2026", "2:30 PM").
Their source pages are not published, so they are reproduced as expression
tests with reconstructed inputs, and Booleans/Numbers rendered into HTML use
the text representation (`false`, `1`).
- **Evidence:** documented. **Confidence:** medium (inputs reconstructed).

---

## 16. Errors

### R-SESSEL-340 — Error categories
Every runtime failure is an error value with a `type` (category) and a
`message`. Categories:
| `type` | Raised for |
|---|---|
| `TypeError` | operand/argument of the wrong type (`"a" + 1`, `-"x"`, `5.upper()`), not callable, `.Integer()`/`.Float()` parse failure (SC-1), `Element.fromString` of empty/element-less input, unknown `search()` option, mutation of a queried element, invalid selector, invalid regex, bad format token, instance validation failure |
| `RuntimeError` | division by zero, integer overflow, unresolved variable, query timeout, budget exhaustion, the evaluation depth limit, an unreadable stored document, `reduce(cb)` on an empty list, `Pagelove.PUT/DELETE` without a write provider |
| `Error` | a user `throw` of a value that is not an error dictionary (pagelike choice, R-SESSEL-342) |
Parse errors are not runtime errors: they fail the whole program before it
runs and are never caught (R-SESSEL-40).
- **Evidence:** documented ([SYN] §Try/Catch: `TypeError`, `RuntimeError`,
  and the RuntimeError list "division by zero, integer overflow, an unresolved
  variable, a query timeout, budget exhaustion, the evaluation depth limit, and
  an unreadable stored document"; [ELM] `Element.fromString("")` → TypeError;
  [NUM]/[STR] "throw a type error"; [TYP] unknown option keys → TypeError);
  inferred (other rows, `Error`). **Confidence:** high (documented rows) / low
  (the rest; SP-15).

### R-SESSEL-341 — `try { … } catch (e) { … }`
Evaluates the try block; on a runtime error evaluates the catch block with `e`
bound to a Dictionary `{ message: String, type: String }` (message first) and
returns its value; otherwise returns the try block's value. The catch
variable is scoped to the catch block. `null` results are not errors
(`try { null.text() } catch (e) { "caught" }` → `null`).
- Budget-exhaustion errors are reported as `RuntimeError`; they MAY be caught,
  but the budget stays exhausted, so any further metered step inside the
  handler re-raises (pagelike decision, low).
- **Evidence:** documented ([SYN] §Try/Catch). **Confidence:** high.

### R-SESSEL-342 — `throw v`
Raises a user error carrying `v`. Caught by `try`, the catch variable is:
`v` itself when `v` is a Dictionary with String `message` and `type` keys;
the Instance when `v` is an Instance (e.g. an `HTTPResponse`; `e.status`
readable through dispatch); otherwise `{ message: <text of v>, type: "Error",
value: v }` (pagelike decision, low; SP-14). Uncaught, it fails the
evaluation like any runtime error; hosts give `HTTPResponse` instances special
treatment (R-SESSEL-294).
- **Evidence:** documented (`throw_expr` [SYN]; HTTPResponse throws [TRG]);
  inferred (catch shape). **Confidence:** medium / low.

### R-SESSEL-343 — Messages
The only documented message is `Cannot add String and Integer`; pagelike
MUST produce exactly `Cannot add <Left> and <Right>` for invalid `+`
(R-SESSEL-80). (Live 2026-09-29: superseded by `type error: cannot use '<v>'
in arithmetic`, observed for `+` and `*`; pagelike uses it for all four
operators, R-SESSEL-80.) Other messages are implementation-defined but MUST mention the
operation and the offending type or name (e.g. `unresolved variable: foo`,
`division by zero`, `no method 'upper' on Integer`). Cases MUST NOT assert
other message texts.
- **Evidence:** documented. **Confidence:** high.

### R-SESSEL-344 — Host reporting of uncaught errors
| Host | Result |
|---|---|
| QUERY | parse error → 400; uncaught `TypeError` or unresolved variable → 400 ("fails to … evaluate"); other uncaught errors and user throws → 500 ("raised a runtime error"); budget exhaustion → 503 (recommendation to the protocol owner; SC-19) |
| `e:` binding, method element, stamp | composition fails: HTTP 500 with the error message ([EXB], [MEL]) |
| trigger/processor `when`/`action` | request fails with an HTML-microdata error body describing the failure ([TRG] §Error handling; status 500, inferred) unless the error is an `HTTPResponse` |
| property/schema `@validate` | 422 SchemaViolation, `check` `@validate` (modeling) |
| `@write`/`@read` | the write/read request fails (modeling R-MOD-52: 500) |
| `default` | write error on the affected item (modeling) |
| TransitionHandler `when` | no delivery for that handler; the write is unaffected ([TRH]) |
| HttpRequest dynamic property | the trigger/processor fails as above (inferred) |
Error documents for schema slots embed a `https://pagelove.org/BindingFailure`
item with `language` `https://pagelove.org/Sessel`, `variant` (`parse`,
`threw`, `timeout`, `out-of-memory`) and `message` ([JSS] §Errors: "Sessel
bindings produce structurally identical BindingFailure items with a different
`language` value"; the exact `language` value is inferred).
- **Evidence:** documented per row except as marked. **Confidence:** medium.

---

## 17. Budgets and limits

### R-SESSEL-356 — Shared per-request budget
Sessel and server JavaScript draw from **one** per-request budget (ops,
memory, time), configured by the host's `https://pagelove.org/TransactionBudget`
microdata item (cross-area: its property vocabulary is not documented;
modeling/js areas). Ops are charged per evaluated AST node and per
element visited by selector matching; memory for strings, lists, dictionaries
and constructed nodes; time as wall-clock.
- **Evidence:** documented ([JSS] §Resource limits: "shared with Sessel … a
  single shared budget"; [SYN] budget exhaustion is a RuntimeError); inferred
  (charging points). **Confidence:** high (sharing) / low (charging).

### R-SESSEL-357 — Limits and pagelike defaults
When no TransactionBudget applies, pagelike uses: 2,000,000 ops per request,
64 MiB Sessel heap per request, 2 s wall-clock per evaluation, call/nesting
depth 256 (lambda calls, method calls, nested evaluation), program source ≤
256 KiB, selector literal results ≤ 100,000 elements before `.count()`-style
reduction (all pagelike decisions; inferred). Exceeding any limit →
`RuntimeError` (depth: "evaluation depth limit").
- Composition additionally has 500 method dispatches per request →
  `503 composition budget exceeded` (cross-area, [MEL] §Error cases).
- **Evidence:** documented (existence of depth/time/budget errors);
  inferred (numbers). **Confidence:** low.

---

## 18. Results at host boundaries

### R-SESSEL-370 — QUERY
The result value is encoded per R-SESSEL-97 / R-PROTO-72..73 (single
Element → `text/html` outer HTML; anything else → `application/sessel+json`).
`Range: entries=` slices a List result (R-PROTO-80..83).

### R-SESSEL-371 — Method elements, stamps
| Result | Effect |
|---|---|
| Element | serialised, parsed as a fragment, spliced in place; composition recurses |
| Instance | its microdata element is spliced |
| List of Elements/Instances | each spliced in order |
| scalar | inserted as text using the text representation (`value_to_html`) |
| `null` | the method element is removed |
- **Evidence:** documented ([MEL] §How the result becomes HTML; [STP]).
  **Confidence:** high.

### R-SESSEL-372 — `e:` bindings
The value (any type) is stored under the binding name in `Context` and
exposed to later bindings as a bare identifier and to Liquid templates
(cross-area liquid: Float renders like `100.0`, per [EXB] "Sum: 100.0").

### R-SESSEL-373 — Gates and predicates
Trigger/processor/handler `when` and Group `includes()` use truthiness
(R-SESSEL-93).

### R-SESSEL-374 — Validators
Property and schema `@validate` results are judged by the modeling area
(truthy vs exactly `true`, SC-17).

### R-SESSEL-375 — Resolvers
`@write`/`@read` must return an Element or a List of Elements (a single
Element is wrapped); anything else fails the request; an empty List removes
the property ([RSV] §Error cases).

### R-SESSEL-376 — Defaults and dynamic properties
A Sessel `default` and a dynamic `HttpRequest` property (`url`, `body`,
header `value`) use the text representation of the result; `null` means "no
value" (default not injected / header dropped; inferred).
- **Evidence (372–376):** documented ([EXB]; [TRG]; [GRP]; [RSV]; [PRP];
  [HRQ]); inferred where marked. **Confidence:** medium.

---

## 19. Cross-area dependencies

| Area | Dependency |
|---|---|
| protocol | QUERY shell, statuses and encoding (R-PROTO-70..85); this spec recommends the 400/500 split of R-SESSEL-344 (SC-19) and the encodings of R-SESSEL-97 |
| reading-writing / selector engine | CSS Level 4 + PageLove pseudo-classes, text normalisation, template inertness, microdata value extraction (R-SESSEL-201, 222), JSON-LD `@type` shortening reused by `.microdata()` |
| permissions | glob language for `from` (R-PERM-20); Group `includes()` evaluation; authorization-rule context names (`auth`, `method`, `path`, `query`); QUERY grant (R-PERM-24/46) |
| modeling | schema registry, inheritance, defaults, validation, `@key`, computed properties, resolver pipelines and their error mapping (R-MOD-19/20/44..52); `set_text`/`set_attr` aliases required here |
| composition | `e:` binding order/scope, `Context` subtree visibility, method-element dispatch, stamps, parameterized-route `request.params`, request document, `Cache-Control: private` tainting |
| liquid | rendering of Sessel values (Floats, Elements, Lists, Dictionaries) in templates |
| reactions | trigger/processor/handler contexts, `HTTPResponse` handling, `Context.response` read-back (SC-7), `Pagelove.PUT` body replacement, `HttpRequest` dynamic properties |
| js runtime | shared TransactionBudget; cross-language method calls (arguments positional, `Context` shared); `BindingFailure` envelope |

---

## 20. Contradictions and compatibility decisions

| # | Topic | Claims | Decision |
|---|---|---|---|
| SC-1 | `.Integer()`/`.Float()` on unparseable text | [TYP] §Type coercion: return `null`, "never throw" (`"5.0"` → `null`); [NUM] §How numbers arise and [STR] §Type coercion methods (three places): throw a type error | Throw `TypeError` (more specific, repeated). Losing claim kept as a `status: disputed` expression test. SP-1 |
| SC-2 | Unquoted values in selectors/construction | [SYN]/[SES-K]: always an expression; [SYN] example `new input[type=checkbox][checked] {}`, [SXT]/[RSB] CSS with `[itemprop=hostname]` | Expression first; fall back to raw CSS when an identifier is unbound or the text is not an expression (R-SESSEL-202). SP-2 |
| SC-3 | Shape of `.microdata()` values | [ELM]: scalars (`"name": "Widget"`); [SCH] and [RSV]: `md["email"].first()`, `.microdata()["name"].first()` | Scalars for single values, List for repeated ones (matches [PRP] "one route" and JSON-LD); `.first()`/`.last()` on a scalar return it. SP-3, SP-4 |
| SC-4 | Scope of `Selector.execute()` with no argument | [SES-Q], [TYP]: same as `from self`; [MEL]: the site-wide `r:` binding is implemented as `new Selector { selector: parameters[0] }.execute()` | `from self` (the language reference); resource bindings are the composition area's own site-wide query. SP-12 |
| SC-5 | Grammar vs documented programs | [SYN] EBNF: `let` only as a statement, lambdas only as arguments, construction `text:` XOR children, no assignment statement | Extended grammar R-SESSEL-30 (every doc example parses) |
| SC-6 | Methods used by docs but absent from the reference | [RSV]/[RTD]: `set_text`, `set_attr`, `lowercase`, `slugify`, `parseDateTime`; [ELM]: setters only on constructed elements | Provide them (R-SESSEL-113, 123, 241) and make pipeline `self` elements mutable working copies (R-SESSEL-242). SP-5 |
| SC-7 | Writing response headers from a processor | [PRC]: actions may write `Context.response.headers`; [BJS-CORS]:21-25: `headers.set` "is not a Sessel function, index assignment is not a valid assignment target, and header writes are dropped" | Grammar rejects `Context.response.headers["X"] = v` (R-SESSEL-35); whether whole-map assignment affects the response is decided by reactions (recommend: apply it) |
| SC-8 | Mixing `text:` and children | [SYN] construction grammar forbids; [SES-K] "You can mix both", [ELM] `new div { text: "hello", new p { … } }` | Allow mixing, in order (R-SESSEL-263) |
| SC-9 | Braces on construction | [SES-K]: braces "still required" for void elements; [SYN] grammar body optional; [ELM] `new p.text("Hello, world")` | Braces optional (R-SESSEL-37). SP-29 |
| SC-10 | `Instant.until` balancing | [TMP]: `PT3H30M`; TC39 default: `PT12600S` | Follow docs: hours (R-SESSEL-304) |
| SC-11 | QUERY examples | [QRY] request/response pairs mismatched | Owned by protocol (C-7) |
| SC-12 | Bare `prior` | [SYN]: `prior` is an identifier except after `from`; [SES-Q]: "Always check before using it: `prior != null`" | Bind the identifier `prior` to the pre-mutation root in write contexts as well (R-SESSEL-208) |
| SC-13 | `Context` without `@schema` | [PRC] AuditLog example reads `Context.response.status` with only `Pagelove`/`AuditLog` declared; everywhere else `@schema Context …` precedes use | Pre-bind `Context` (R-SESSEL-60) |
| SC-14 | Broken HTML in [EXB] | `e:total="${[itemprop="price"]}.sum()"` nests `"` in a `"`-attribute; `e:localcount="${div.item} from self).count()"` has an unbalanced `)` | No repair; cases use the corrected forms `'price'` and `(${div.item} from self).count()` |
| SC-15 | `new HTTPResponse(status, message)` in Sessel | [JSS] says this form "is Sessel's"; no Sessel page shows it; all Sessel examples use braces | Not supported (parse error). SP-31 |
| SC-16 | ZonedDateTime `equals` example | [TMP] says 14:30 New York and 19:30 London are "the same instant"; on 2026-03-23 London is on GMT, so they differ by an hour | Irrelevant to the result (`false` either way); tests use a pair that really is the same instant |
| SC-17 | `@validate` acceptance | [PRP] "must return a truthy value" vs error table "returns anything other than `true`"; [SCH] "All must return `true`" | Owned by modeling (R-MOD-45 chose non-`true` fails) |
| SC-18 | String quote styles | [SYN] grammar shows only `"`; [STR] "Both quote styles are equivalent" | Both, including interpolation. SP-18 |
| SC-19 | QUERY error mapping | [QRY]: "fails to parse or evaluate" → 400 vs "raised a runtime error" → 500; protocol.md R-PROTO-75 table puts `.Integer()` failures at 500 while its edge-case note puts core `TypeError`s at 400 | Recommend R-SESSEL-344 to the protocol owner. SP-15 |

---

## 21. Open questions for live probing

Common setup **S0** (authoring key over WebDAV, then public-plane requests as
anonymous):
1. `PUT dav:/_pl/sp/doc.html` with
   `<!DOCTYPE html><html lang="en"><body><h1 id="t">A</h1><ul id="main"><li>a</li><li>b</li></ul><p itemscope itemtype="https://example.com/T"><meta itemprop="k" content="v1"><meta itemprop="k" content="v2"><span itemprop="one">x</span></p></body></html>`.
2. `PUT dav:/_pl/sp/_rules.html` with an AuthorizationRule `actor * ,
   resource /_pl/sp/*, method GET, method QUERY, action allow`.
3. Each probe below is `QUERY /_pl/sp/doc.html` with
   `Content-Type: text/sessel` and the given body; record status,
   `Content-Type` and body.

| # | Question | Bodies (one request each) | Settles |
|---|---|---|---|
| SP-1 | `.Integer()` failure | `try { "5.0".Integer() } catch (e) { e.type }` ; `"abc".Float()` | SC-1 (`"TypeError"` vs `null`) |
| SP-2 | Unquoted identifier fallback | `(new input[type=checkbox] {}).attr("type")` ; `(${[id=t]} from self).count()` ; `let t = "main"; (${[id=t]} from self).first().attr("id")` | SC-2 |
| SP-3 | `.microdata()` value shape | `(${[itemscope]} from self).first().microdata()` | SC-3 (`k` list vs scalar, `one` scalar, `@type` short/full, `@id`) |
| SP-4 | `.first()` on a String | `try { "abc".first() } catch (e) { e.type }` | SC-3 leniency |
| SP-5 | Resolver-only methods | `try { "A b".slugify() } catch (e) { e.message }` ; same for `"X".lowercase()`, `"2026-04-08T09:15:00Z".parseDateTime().format("d MMMM yyyy")`, `(new p {}).set_text("x")`, `(new p {}).set_attr("id", "y")` | SC-6 |
| SP-6 | `replace` literal vs regex | `"a.b.c".replace(".", "-")` | R-SESSEL-114 (`"a-b-c"` vs `"-----"`) |
| SP-7 | Float formatting | `[100.0, 0.1 + 0.2, 1.0 / 3, 10000000000000000.0, 0.000001].map(x => x.String())` ; `100.0` | R-SESSEL-92/97 |
| SP-8 | Null ordering | `[null > 1, null < 1, null == null, null <=> 1]` | R-SESSEL-84 |
| SP-9 | Cross-type equality | `[1 == 1.0, 1 == "1", null == false, [1] == [1], {a: 1} == {a: 1}]` | R-SESSEL-83 |
| SP-10 | `&&`/`||` result type | `["a" || "b", 0 || "x", "a" && 2]` | R-SESSEL-85 |
| SP-11 | Element text representation | `"#{(${h1} from self).first()}"` ; `(${li} from self).join(",")` ; `(${li} from self).contains("a")` ; `(${li} from self).first().String()` | R-SESSEL-89 |
| SP-12 | `Selector.execute()` scope | with a second doc `/_pl/sp/other.html` containing `<h1>B</h1>`: `(new Selector { "h1" }).execute().count()` | SC-4 (1 vs ≥ 2) |
| SP-13 | Site scope order | `${h1}.filter(e => e.path().startsWith("/_pl/sp/")).map(e => e.path())` after also creating `/_pl/sp/a.html` and `/_pl/sp/z.html` (created in the order z, a) | R-SESSEL-204 |
| SP-14 | User throw shape | `try { throw "x" } catch (e) { e }` ; `try { throw { message: "m", type: "T" } } catch (e) { e }` | R-SESSEL-342 |
| SP-15 | Error categories and QUERY statuses | `try { 1 / 0 } catch (e) { e }` ; `try { [].reduce((a, b) => a) } catch (e) { e.type }` ; uncaught: `"a" + 1`, `nope`, `1 / 0`, `throw "x"` | R-SESSEL-340, SC-19 |
| SP-16 | `=> {}` | `[1].map(x => {})` | R-SESSEL-33 |
| SP-17 | Unknown escapes | `"\d".count()` ; `"A"` | R-SESSEL-14 |
| SP-18 | Single-quote interpolation | `let n = 1; '#{n}'` | SC-18 |
| SP-19 | Block comments | `1 /* c */ + 1` | R-SESSEL-10 |
| SP-20 | Missing operators and literals | `7 % 2` ; `1e3` ; `.5` | R-SESSEL-13, 17 |
| SP-21 | Integer limits | `9223372036854775807 + 1` ; `try { 9223372036854775807 * 2 } catch (e) { e.type }` ; `9223372036854775808` | R-SESSEL-91 |
| SP-22 | `until` balancing | `Temporal.PlainDate.from("2026-03-23").until(Temporal.PlainDate.from("2026-12-31")).toString()` ; same for PlainDateTime (`2026-03-23T14:30` → `2026-03-30T09:00`) and ZonedDateTime (`[UTC]`, two days apart) | R-SESSEL-301/303/305 |
| SP-23 | Locale output bytes | `Temporal.PlainTime.from("14:30:00").toLocaleString("en-US").count()` (10 vs 10 with U+202F — compare `.contains(" ")`) | R-SESSEL-311 |
| SP-24 | Temporal parse errors | `try { Temporal.PlainDate.from("2026-02-30") } catch (e) { e.type }` ; `Temporal.PlainDate.from({ year: 2026, month: 2, day: 30 }).toString()` | R-SESSEL-300 |
| SP-25 | Comma after a from-source inside arguments | `[${li} from self, 2].count()` | R-SESSEL-36 (2 vs error) |
| SP-26 | `from` edge sources | `(${li} from null).count()` ; `(${li} from "/_pl/sp/missing.html").count()` ; `(${li} from prior).count()` ; `(${li} from "doc.html").count()` | R-SESSEL-205 |
| SP-27 | `.microdata()` `@id` | `(${[itemscope]} from self).first().microdata()["@id"]` after adding `id="it"` to the item | R-SESSEL-233 |
| SP-28 | `.selector()` without id ancestors | `(${li} from self).at(1).selector()` after removing `id="main"` | R-SESSEL-225 |
| SP-29 | Brace-less `new` | `new p` ; `new br` | SC-9 |
| SP-30 | `name:` on a plain element | `new div { title: "x" }` | R-SESSEL-263 |
| SP-31 | HTTPResponse in QUERY and paren form | `@schema R url("https://pagelove.org/HTTPResponse"); throw new R { status: 418, body: "t" }` ; `@schema R url("https://pagelove.org/HTTPResponse"); new R(418, "t")` | R-SESSEL-294, SC-15 |
| SP-32 | Dictionary aliasing | `let a = {}; let b = a; b.x = 1; a` | R-SESSEL-96 |
| SP-33 | `.text()` whitespace | after `PUT` of `<h2>  a\n b </h2>`: `(${h2} from self).first().text()` | R-SESSEL-221 |
| SP-34 | Unknown method on a value | `try { 5.upper() } catch (e) { e }` | R-SESSEL-76 |
| SP-35 | `Context` without declaration (processor) | Processor on `/_pl/sp/*` GET status 2xx, action `Context.response.status = 299` with no `@schema` line; `GET /_pl/sp/doc.html` | SC-13 |
| SP-36 | Header name case | Trigger on `/_pl/sp/*` PUT with `when` `@schema Context url("https://pagelove.org/Context"); Context.request.headers["X-Probe"] == "1"` and `otherwise` throwing HTTPResponse 403; `PUT` with header `x-probe: 1` (lower case) | R-SESSEL-293 |
| SP-37 | `symbols` alphabet | `String.random(200, { symbols: true })` | R-SESSEL-122 |
| SP-38 | Hash verification helpers | `try { "p".bcrypt_verify("x") } catch (e) { e.type }` | R-SESSEL-120/121 |
| SP-39 | Namespaced construction | `@namespace svg url("http://www.w3.org/2000/svg"); new svg|svg[width="2"] { new svg|circle {} }` | R-SESSEL-265 |
| SP-40 | `new` with an undeclared capitalised name | `new Widget { a: 1 }` | R-SESSEL-285 |

---

## 22. Harness cases

HTTP cases live in `harness/cases/sessel/` (all `requires: [sessel]`; every
QUERY case grants `GET`+`QUERY` to `*` on `${P}/*` because a Sessel QUERY is not
default-granted, R-PROTO-74). Selector literals `${…}` pass through the runner
untouched because only `${P}`, `${HOST}` and captured names are substituted;
cases never use those names as selectors. Bare site-wide selectors are avoided
in live-capable cases (a live host holds other documents); they use
`from self` / `from "${P}/…"`.

| File | Covers |
|---|---|
| `query-values.yaml` | end-to-end encoding of numbers, strings, lists, dictionaries, floats; interpolation; try/catch; errors → status (R-SESSEL-80..97, 340..344) |
| `query-selectors.yaml` | selector literals, `from` forms, globs, multi-source, block-from, embedding, pseudo-classes, sub-select, provenance, Selector type, template content (R-SESSEL-200..233) |
| `query-construction.yaml` | construction results as `text/html` and inside JSON (R-SESSEL-247, 260..265) |
| `query-schema.yaml` | `@schema`, `search()`, `construct()`, instances, reflection (R-SESSEL-280..289) |
| `bindings.yaml` | `e:` bindings rendered through `<p:stamp>` and Liquid (R-SESSEL-1, 371, 372) |
| `reactions.yaml` | Sessel trigger/processor gates, `HTTPResponse`, header lookup (R-SESSEL-290..294) |
| `expressions.yaml` | ≥ 300 pure expression tests (format documented in the file header) covering every method and syntax form |
