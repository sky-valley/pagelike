# research/

Reference material pagelike was built and tested against. Only
`COMMITS.txt` is committed; the rest is fetched locally and git-ignored.

| Path | What | How to get it |
|---|---|---|
| `COMMITS.txt` | The public PageLove repositories (github.com/pagelove) and the commits pagelike was tested against | committed |
| `upstream/<repo>/` | Checkouts of those repositories, used by the release acceptance suite (`e2e/tests/apps`) | `tools/research/clone_upstream.sh` |
| `docs/<date>/` | A snapshot of the public PageLove documentation, with a manifest (URL, ETag, Last-Modified, SHA-256 per page), used to derive `docs/spec/` | `python3 tools/research/fetch_docs.py` |

The upstream repositories and documentation belong to their authors; they are
fetched from their public sources, not redistributed here.
