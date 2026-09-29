# Example experiences

Plain HTML + CSS + JavaScript experiences that use only PageLove-documented
runtime features (authorization rules, triggers, closed shapes, schemas,
transition constraints, resource bindings, Liquid templates, includes, SSE),
plus pagelike's own participation views and remix. They are verified on
pagelike (`e2e/tests/examples.spec.mjs`). They have not been run on PageLove:
their participants sign in, and a PageLove host needs its own OIDC provider
for that, which the disposable test host does not have.

| Example | Shows |
|---|---|
| `sky/` — “Show me your sky” | media submissions (image uploads into a per-user folder), server-enforced ownership (templated selector rules + a Sessel trigger + closed shapes), live updates, shareable participation views (pagelike), remix into “show me your dog” |
| `poll/` — shared poll | one vote per signed-in person (trigger), change/withdraw own vote only, server-rendered tallies (binding + Liquid), live tallies |
| `board/` — team board | composition (include + binding + Liquid lanes), a TransitionConstraint state machine (illegal moves → 422), schema validation, live refresh of composed lanes |
| `feed/` — iframe host | experiences embedded cross-origin in iframes |

```sh
go build -o bin/pagelike ./cmd/pagelike
./bin/pagelike serve &                               # http://*.localhost:8787
examples/install.sh sky sky
./bin/pagelike user add --site sky alice --name Alice --password-stdin <<< 'correct horse'
open http://sky.localhost:8787/
# remix: independent state, no participants, lineage recorded
./bin/pagelike fork --from sky --to dog --note "show me your dog"
curl -X PUT http://dav-dog.localhost:8787/index.html -H "Authorization: Bearer $DOGKEY" \
     -H 'Range: selector=#invitation' --data-binary '<h1 id="invitation">Show me your dog</h1>'
```

For iframes across `*.localhost` origins, give the embedded sites
partitioned cookies: `pagelike identity set --site sky --cookies partitioned`
(see docs/identity.md).
