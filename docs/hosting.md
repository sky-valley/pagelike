# Managed hosting

Managed hosting keeps an immutable authored version separate from live documents
under `data/` and uploads under `uploads/`. A publication contains an increasing
generation, a version identifier and its complete files. Replaying an older
generation cannot change the current application. Reusing a version identifier
with different bytes is refused. Each authored data seed is applied once, even
if a participant subsequently deletes its document. All publication changes
commit together in SQLite. Historical authored reads use the selected version;
authorization and shared participation use current rules.

The hosting HTTP boundary accepts separate control and edge credentials. Only
the control credential can install or disable a site. The edge can reach only
the public application plane; it cannot reach WebDAV, instance administration,
development impersonation or local login. Neither credential becomes app data.

The identity authority redeems a single-use site-scoped pass into a minimal
subject and a server-only session reference. Cookies are secure, host-only,
HttpOnly and partitioned. Authenticated requests revalidate the session reference
with the authority. Public reads continue anonymously when the authority is
unavailable; writes fail. The authority owns logout and revocation. No external
account claims are disclosed by this protocol.

Removal ends serving and participation while retaining stored documents.
Temporary sites have a deadline that also bounds open streams. Browser copies
already delivered are outside the server's removal guarantee.

These are native hosting extension contracts, not claims about another server's
behavior. The local harness and hosting HTTP tests are their evidence.

Run `pagelike host --data /durable/path --listen 127.0.0.1:8788` behind a TLS
edge. Set `PAGELIKE_DOMAIN`, `PAGELIKE_ADMIN_TOKEN`, `PAGELIKE_EDGE_TOKEN`,
`PAGELIKE_IDENTITY_URL`, `PAGELIKE_IDENTITY_TOKEN`, and space-separated
`PAGELIKE_FRAME_ORIGINS`. All three credentials must differ. The identity URL
requires HTTPS except on loopback; parent origins may use loopback HTTP for
development. Public post origins always use HTTPS. The trusted edge sends the
original public authority, including an explicit port, in `X-Pagelike-Host`.

The parent bridge is served at `/-/client.js`. A post calls
`pagelike.identity()` on load to restore an existing participant, and
`pagelike.participate()` when someone chooses to contribute. Only the runtime
can read the resulting session cookie. `/-/manage` exposes the caller's own
contributions, removal and reports. Creators may moderate all contributions.
Reports go to the configured authority's `/report` endpoint. Authentication
and instance administration routes from standalone mode are unavailable.

## Restoring a returning participant

```html
<script src="/-/client.js"></script>
<script type="module">
  const person = await pagelike.identity();
  if (person) {
    const response = await fetch('/-/contributions', { cache: 'no-store' });
    if (response.ok) console.log('Your existing contributions:', await response.json());
  }
</script>
```

`identity()` returns the opaque site identity or `null`. It checks `/-/me`
first; without a valid session it asks the approved parent to restore an
existing identity. The parent must check its signed-in person has previously
joined this site before issuing a ticket. A first-time or signed-out visitor
stays anonymous. No sign-in UI or contribution write happens during restoration.
A session is authority to act under the site's rules, not evidence of a saved
contribution; the app must read its own data or `/-/contributions` to show that.

The SDK returns `null` on refusal, network failure or a parent that does not
answer within five seconds. The initial session check is also bounded to five
seconds. Browsing remains usable. Results are not cached across calls, so a
later lookup observes logout. Simultaneous lookups share a request; deliberate
participation waits for an in-progress restoration. Missing browser support for
partitioned cookies is not bypassed by exposing the session reference to script.

### Parent bridge protocol

The parent validates the frame's exact window, origin and the exact request
shape before acting:

| Request from frame | Parent behavior |
|---|---|
| `{type: 'pagelike:ready'}` | Optional readiness notification; does not request identity |
| `{type: 'pagelike:identity', id}` | Quietly restore an existing site identity; never enroll or prompt |
| `{type: 'pagelike:participate', id}` | Deliberate participation, including sign-in when needed |

`id` is a fresh UUID. Reply to the requesting frame's exact origin with
`{type: 'pagelike:participation', id, ticket}` or
`{type: 'pagelike:participation', id, error: 'anonymous'}` when restoration is
inapplicable (`'unavailable'` for a failed lookup). The SDK accepts only its
configured parent origin, parent window and matching request ID. It redeems
the ticket with `POST /-/session` and returns the participant string, never
the server-only session reference. Deliberate participation rejects missing
identity; restoration resolves to `null`. Parent navigation/unmount must cancel
pending work, while the initial iframe `load` event must not cancel a request
its startup script already sent.

See the [managed identity tutorial](../site/content/build/managed-identity.md),
the [native contract](spec/hosting.md), and `e2e/tests/hosting*.spec.mjs` for
executable parent, two-browser, revocation and message-boundary examples.

Each site allows 10,000 live documents and 256 MiB of live bytes, with 100,000
contribution records. Removals and size reductions work at capacity. HTML data
is inert: scripts, event handlers, runtime configuration and template execution
are refused, including writes produced by processors and schemas. HTML
contributions must wrap text in elements so each can be removed independently.
Other data is validated JSON or plain text, bounded at 512 KiB. JPEG, PNG and
GIF uploads are decoded and re-encoded without metadata, bounded at 10 MiB
and 16 million pixels; GIF uploads retain their first frame as a still image.

The process admits at most 32 ordinary requests, four writes and 64 streams
concurrently. Excess work receives 503 and `Retry-After`. Deploy with bounded
disk and process resources. To make a complete backup, stop the runtime,
run `pagelike backup --data /durable/path --out /new/snapshot`, restart it,
and copy the snapshot off-host. Restore using the same binary, reconcile the
authority's current installations and removals, then reopen the edge.
