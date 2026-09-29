---
name: implement-behaviour
description: Add or change a pagelike behaviour the evidence-first way — find the requirement, write the harness case, implement, verify. Use for any feature work or bug fix that changes what the server does.
---

# Implement a behaviour

1. **Find the requirement.** Search `docs/spec/<area>.md` for the `R-…`
   requirement and its evidence level, and check
   `docs/compat/live-observations.md` and the decision files for anything
   observed live that supersedes it.
   - If nothing covers the behaviour, add a requirement to the spec. Give it
     an evidence level: documented (docs page and section), client-source or
     demo-source (repo@commit:line), live-observed (an observation file), or
     inferred (say from what).
   - If the evidence is only inferred and the behaviour matters, consider
     `probe-pagelove` first.
2. **Write the case first**, in `harness/cases/<area>/<topic>.yaml`. The
   format is in `harness/README.md` and the rules in `harness/AGENTS.md`:
   - unique id;
   - `evidence` and `source`;
   - paths under `${P}`;
   - `live: false` if it can't run on PageLove;
   - `requires: [slow]` for long waits.

   Run it and watch it fail:
   `go run ./harness/cmd/harness run --ids <id> -v`.
3. **Implement** in the owning package (the map is in
   `docs/architecture.md`):
   - features register through `server.Extend` from
     `internal/features/features.go`;
   - all HTML goes through `internal/dom`;
   - errors are `internal/errdoc` documents;
   - cite the requirement in comments.
4. **Verify outward:**
   - the case;
   - its area (`--filter <area>`);
   - `scripts/check.sh --quick`;
   - `scripts/check.sh` if a browser could see the change.

   A change to parsing, serialization, authorization or SSE needs the full
   harness and the e2e suites, because they're cross-cutting.
5. **Record.**
   - A deliberate difference from PageLove follows
     docs/decisions/0005 (a `.live` sibling plus a decision entry).
   - A security-relevant difference is added to
     docs/decisions/0006.
   - Add a CHANGELOG.md line under "Unreleased".
