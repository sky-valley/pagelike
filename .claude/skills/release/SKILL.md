---
name: release
description: Cut a pagelike release — changelog, checks, public-content scan, tag, verify the published binaries and go install. Use when the user asks to release or publish a version.
---

# Release pagelike

The repository is public. Nothing private may ship: run the scan.

1. **Choose the version.** Use semver: while 0.x, a minor bump for features
   or behaviour changes, a patch for fixes. In CHANGELOG.md, turn
   "Unreleased" into `## vX.Y.Z — YYYY-MM-DD`, with user-facing bullets,
   compatibility numbers if they changed, and known limits.
2. **Check:**
   - `scripts/check.sh` (full, including e2e);
   - `scripts/scan-public.sh`. It must say "clean". It also reads the local,
     git-ignored `.private-terms`, which should exist on the maintainer's
     machine.

   If compatibility changed, run `scripts/matrix.sh`. Refresh performance
   (`scripts/perf.sh`) only on the reference machine.
3. **Commit** the changelog, then tag and push:
   `git tag vX.Y.Z && git push origin main vX.Y.Z`.
   - `.github/workflows/release.yml` runs the tests, builds
     `scripts/release.sh` archives (linux, darwin amd64/arm64, windows
     amd64; static, `-trimpath`, version-stamped), and creates the GitHub
     release with `SHA256SUMS`.
   - It leaves an existing release alone.
4. **Verify from the outside:**
   - `gh release view vX.Y.Z` lists the 5 archives and `SHA256SUMS`;
   - download one archive, check it against `SHA256SUMS`, and run
     `pagelike version`;
   - `go install github.com/sky-valley/pagelike/cmd/pagelike@vX.Y.Z`, then
     `pagelike version`.
5. **Edit the release notes** if the generated ones are thin:
   `gh release edit vX.Y.Z --notes-file …`. Include install lines, the
   highlights, and the PageLove attribution line: "an independent
   reimplementation, not affiliated with PageLove".
