# 0005: How pagelike resolves differences from PageLove

- **Status:** accepted
- **Date:** 2026-09-28, refined 2026-09-29
- **Applied in:** `docs/compat/decisions.md` (the 69 divergences of
  2026-09-28), `docs/compat/decisions-2026-09-29/*.md` (40 more, plus the
  document model and integration)

## Context

Specifications derived from documentation, client code and demo apps
disagree with the live service in hundreds of small ways. Some of those are
PageLove bugs, some are documentation drift, and some are harness mistakes.
Without a rule, every case becomes an argument.

## Decision

Every divergence gets exactly one class:

| Class | When | What changes |
|---|---|---|
| **harness artifact** | The case or harness caused the failure (wrong input, footer, timing) | Fix the case or harness; pagelike is unchanged unless the fixed case exposes a real difference, which is then classified |
| **adopt-live** (the default) | PageLove behaves differently from the spec | The case follows PageLove (`evidence: live-observed`, `source:` citing the observation); pagelike changes; the spec gets a "live YYYY-MM-DD" supersession note. PageLove bugs that produce no incorrect data are adopted too |
| **keep-documented-security** | Following PageLove would weaken a security property (see 0006) | pagelike keeps its documented behaviour; a sibling `<id>.live` (`status: live-divergence`) asserts PageLove's |
| **keep-standard** | PageLove's behaviour is plainly incorrect: bad data, broken navigation, a crash on valid input, or a broken standard every client relies on | Same as above: keep, and add a `.live` sibling |

Supporting rules:
- **Minimize before deciding.** When the cause is unclear, write a probe
  case (`*.probe-MMDD.*`) that isolates one behaviour, and run it live.
- **Confirm the result.** After changing expectations without a live run,
  do a final confirmation run.
  `harness/observations/live-2026-09-29-final/` exposed one more bug that
  way.
- **Disputed claims.** The losing side of a documented contradiction is
  `status: disputed`: XFAIL live, and an XPASS forces a review.
- **Integrator's check.** Decisions made in parallel are reviewed against
  this policy when they're merged. On 2026-09-29, two adopt-live decisions
  were reclassified: WebDAV validation and pagination parameters.
- **The live budget:**
  - only disposable hosts;
  - 3 requests/s;
  - bounded runs (`scripts/live-run.sh`);
  - every run's observations committed as evidence.

## Consequences

- Every deliberate difference is a passing live test (its `.live` sibling),
  so a change on PageLove's side shows up as a failure.
- The matrix separates match, kept and unexplained differences, and the
  target for unexplained is 0.
