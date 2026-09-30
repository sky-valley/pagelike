# Managed hosting bridge

These are native extensions, not PageLove compatibility claims. Evidence is
inferred from the managed hosting contract in `docs/hosting.md`; HTTP harness
cases and browser tests verify the contracts.

## R-HOST-1 — Restore identity without enrolling a visitor

`/-/client.js` exposes `pagelike.identity(): Promise<string | null>`. It checks
the existing runtime session, then asks the approved parent to restore an
existing site identity. The parent decides eligibility and supplies a one-use
site-scoped ticket or an anonymous response. This call never requests a sign-in
prompt or writes a contribution. Missing, refused or unavailable identity returns
null, including when the parent does not answer. `participate()` remains the
explicit participation flow. Neither result is cached beyond an in-flight call;
logout must take effect on the next lookup.

Messages bind the exact parent window, configured parent origin and fresh request
ID. The exchange uses the existing secure partitioned session cookie. Concurrent
lookups share one restoration; deliberate participation waits for restoration
before opening a separate request. See `docs/hosting.md` for the wire protocol.
