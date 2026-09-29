# Migration from PageLove to pagelike — demonstration record

Date: 2026-09-29. Source: a disposable PageLove host created for this
project (`live-test-host` in the records). App: the official, unmodified
[pagelove-polls](https://github.com/pagelove/pagelove-polls) @ `c9270e5`,
installed at the host root over WebDAV. Automated as
`e2e/tests/migration.spec.mjs` (Playwright + system Chrome); all three phases
pass. It talks to live PageLove and creates one poll per run, so it runs only
when asked:

```bash
cd e2e && PAGELIKE_LIVE_E2E=1 npx playwright test tests/migration.spec.mjs
```

## Phase 1 — live participation on PageLove

1. A poll is created exactly as the app's form does it: an urlencoded
   `POST /templates/new-poll.html` → `30x` with `Location: /polls/<id>.html`
   (PageLove templated resource creation).
2. Two independent browser sessions open the poll. Session A saves a
   response through the app's UI; session B sees the row appear **live**
   (PageLove SSE, echo-suppressed for A). B responds; A sees it live.

## Phase 2 — migrate

```sh
pagelike migrate --from-dav https://dav-<your-host>.onpagelove.com/ \
  --key-file .secrets/pagelove.env --exclude /_pl/ \
  --out ./polls-export --import --site polls
```

- Every stored file under the host is listed with `PROPFIND Depth: 1`
  (recursively) and fetched over the authoring plane, so HTML arrives as the
  **stored source**, not a composed page. 8 files copied (app pages, assets,
  template, both polls). `/_pl/` (harness scratch space) excluded.
- Verified: the migrated poll document is byte-identical to PageLove's
  stored copy.
- The export directory (`pagelike-export.json` + `files/`) is plain files and
  can be inspected, versioned or re-imported elsewhere.

## Phase 3 — continue on pagelike

- Both PageLove participants' rows are present.
- A new participant's response shows up **live** in another session.
- The app's own server-side rules still apply unchanged:
  whole-document overwrite of an existing poll → `409` (Sessel trigger),
  a row with a `<script>` → `422` (closed ShapeConstraint), writing the
  title → refused (AuthorizationRules).
- New polls are created through the same template on pagelike.

## Configuration and identity mapping

| Item | Carried over | How |
|---|---|---|
| Documents, assets, uploads | yes, byte-for-byte | WebDAV copy |
| Directories | yes | PROPFIND collections |
| AuthorizationRules, schemas, shapes, triggers, templates | yes (they are documents) | WebDAV copy |
| Host default-GET mode | yes if passed | `--default-get` (read it from the console's `urn:Host` item) |
| End-user identities | **no** | pagelike sites use local accounts or their own OIDC provider (`pagelike identity set`). Rules that name users by OIDC `sub` keep working when the new provider's subject is linked to the old one: `pagelike user link --site S --issuer <new> --idp-sub <new sub> <old PageLove sub>` (docs/identity.md). Emails and roles/groups need no mapping if the new provider asserts the same verified emails/roles. |
| OIDC client id/secret | **no** | configure afresh; secrets are never exported |
| API keys | **no** | `pagelike key create` |
| Session state, transient elements | **no** | sessions restart |
| SSE history | **no** | clients reconnect and resynchronise with a fresh read |
| Pending outbound HTTP | **no** | — |
| Free-plan "Powered by Pagelove" footer | not reproduced | it is injected by PageLove at serve time, not stored |

The polls app identifies participants client-side (`localStorage`), so it
needs no identity mapping; its rows remain editable by anyone the app's own
rules allow, exactly as on PageLove.
