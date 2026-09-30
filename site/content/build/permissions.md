---
title: Permissions
description: Declare who may do what with microdata AuthorizationRule items.
---

# Tutorial — Permissions

Permissions on pagelike are **declared as data**. An
`AuthorizationRule` item lives inside the document it applies to.
A request is allowed if at least one matching rule says `Allow` and
**no** matching rule says `Deny` (deny wins).

This isn't a tutorial pattern. It's a
[`/spec/permissions-identity/`](/spec/permissions-identity/) requirement.

## Declare a rule

The simplest rule: every actor can read the document.

```html
<table><tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
  <td itemprop="actor">*</td>
  <td itemprop="resource">/index.html</td>
  <td itemprop="method">GET</td>
  <td itemprop="action">Allow</td>
</tr></table>
```

Upload:

```sh
curl -X PUT http://dav-demo.localhost:8787/rules.html \
  -H "Authorization: Bearer $KEY" \
  --data-binary @rules.html
```

That alone is enough to **open up reads** for the `demo` site if
it was created with `--default-get deny`. The rule says "everyone on
`GET /index.html`". Once it's there, the page stops returning 403 on
public reads.

## Add a writable selector

Restrict signatures to authenticated users:

```html
<table>
  <tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
    <td itemprop="actor">*</td><td itemprop="resource">/index.html</td>
    <td itemprop="method">GET</td><td itemprop="action">Allow</td>
  </tr>
  <tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
    <td itemprop="actor">${request.auth.username}</td>
    <td itemprop="resource">/index.html</td>
    <td itemprop="method">POST</td>
    <td itemprop="selector">#entries</td>
    <td itemprop="action">Allow</td>
  </tr>
</table>
```

The `actor` value is templated. When the request hits, the literal
`${request.auth.username}` is replaced with the authenticated actor's
identifier; an anonymous request fails to match the templated rule
and is denied.

## Deny wins

A selector-scoped Deny always overrides a resource-scoped Allow:

```html
<table>
  <tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
    <td itemprop="actor">*</td><td itemprop="resource">/index.html</td>
    <td itemprop="method">POST</td><td itemprop="action">Allow</td>
  </tr>
  <tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
    <td itemprop="actor">*</td><td itemprop="resource">/index.html</td>
    <td itemprop="method">POST</td>
    <td itemprop="selector">#admin-only</td>
    <td itemprop="action">Deny</td>
  </tr>
</table>
```

`POST /index.html` succeeds in general, but every POST that touches
`#admin-only` is denied.

## Authenticated identity for testing

Local accounts and OIDC per-site are documented at
[`/identity/`](/identity/). For a local development session you can
use `--dev-insecure-auth` and an `X-Pagelike-Dev-User: alice` header;
the server only accepts those on loopback and bails loudly if you
try to start it on anything else.

## What to read next

- [`/spec/permissions-identity/`](/spec/permissions-identity/) —
  `R-PERM-*` rules, deny-wins, templated values.
- [`/examples/sky/`](/examples/sky/) — a per-user folder with
  templated selectors, a Sessel trigger and closed-shape constraints.
- [`/identity/`](/identity/) — identity models and per-site OIDC.
- [`/security/`](/security/) — security policy and reporting.
