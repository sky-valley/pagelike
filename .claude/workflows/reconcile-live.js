export const meta = {
  name: 'reconcile-live',
  description: 'Reconcile live PageLove divergences: one worktree agent per area group diagnoses, probes within a live budget, adopts or records each case, and commits a decisions file',
  whenToUse: 'After scripts/divergences.py has grouped the failing cases of a live sample (see .claude/skills/reconcile-live). Needs args {repoRoot, date, sampleDir, groups:[{area, ids}]}.',
  phases: [{ title: 'Reconcile', detail: 'one worktree agent per area group' }],
}

// args: {
//   repoRoot:  absolute path of the main checkout (holds .secrets/),
//   date:      "YYYY-MM-DD" of the sample,
//   sampleDir: "harness/observations/live-YYYY-MM-DD-sample",
//   groups:    [{ area: "modeling", ids: ["modeling.x.y", …] }, …]   // from scripts/divergences.py
// }
if (!args || !args.repoRoot || !args.groups || !args.groups.length) {
  throw new Error('reconcile-live needs args {repoRoot, date, sampleDir, groups}')
}

const REPORT = {
  type: 'object',
  properties: {
    area: { type: 'string' },
    cases: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          id: { type: 'string' },
          decision: { type: 'string', enum: ['adopt-live', 'keep-documented-security', 'keep-standard', 'harness-artifact', 'unresolved'] },
          summary: { type: 'string' },
        },
        required: ['id', 'decision', 'summary'],
      },
    },
    branch: { type: 'string' },
    commits: { type: 'array', items: { type: 'string' } },
    checks: { type: 'string', description: 'result of scripts/check.sh --quick' },
    liveRuns: { type: 'number' },
    liveCasesRun: { type: 'number' },
    newLiveSiblings: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string', description: 'shared files touched, risky or behaviour-wide changes, open questions, anything the integrator must re-check against docs/decisions/0005 and 0006' },
  },
  required: ['area', 'cases', 'branch', 'commits', 'checks', 'liveRuns', 'liveCasesRun', 'notes'],
}

function prompt(g) {
  const label = 'reconcile-' + g.area
  return [
    'You are reconciling pagelike (a Go, PageLove-compatible server; your current directory is an isolated git worktree of ' + args.repoRoot + ' on a fresh branch) with LIVE PageLove behaviour for the "' + g.area + '" area.',
    '',
    'The live sample of ' + args.date + ' (' + args.sampleDir + ') found these cases diverging:',
    g.ids.map((i) => '- ' + i).join('\n'),
    '',
    'READ FIRST: AGENTS.md, harness/AGENTS.md, docs/decisions/0005-divergence-policy.md, docs/decisions/0006-security-divergences.md, and the observation JSON of each case in ' + args.sampleDir + ' (file = id with dots as dashes, cut to 40 chars). The case files are under harness/cases/, the specs under docs/spec/, earlier decisions in docs/compat/decisions*.md and docs/compat/live-observations.md.',
    '',
    'SETUP: ln -s ' + args.repoRoot + '/.secrets .secrets   then confirm `git status --short` does not list .secrets (it is ignored as /.secrets). Never read, print or copy anything under .secrets/.',
    '',
    'LIVE BUDGET (hard): live requests only via `scripts/live-run.sh ' + label + ' --ids <comma list>`. At most 4 live runs, at most 15 cases per run, only the listed ids, their .live siblings and probes you write (ids <area>.probe-MMDD.<name>, distinct within their first 40 characters). Never --root, never --slow live.',
    '',
    'METHOD per case: (1) harness artifact or real difference? (2) if unclear, a minimised probe, confirmed live; (3) classify per 0005 — adopt-live (case follows live with evidence: live-observed and source: citing the observation; change pagelike in Go with unit tests; add a "live ' + args.date + ': ..." note to the docs/spec requirement), keep-documented-security or keep-standard (keep the case, add <id>.live with status: live-divergence asserting PageLove), harness-artifact (fix the case). A live 5xx on valid input is keep-standard; a live rejection of the case input usually means the case input is wrong — learn the accepted shape from the error body.',
    '',
    'DO NOT EDIT: docs/compat/decisions.md, README.md, CHANGELOG.md, e2e/, examples/, harness/observations/ORDER (the integrator merges those). Write your decisions to a NEW file docs/compat/decisions-' + args.date + '/' + g.area + '.md: per case the id, class, observation files, what PageLove does, what pagelike does now and a short rationale; plus a summary table.',
    '',
    'VERIFY: your cases (and siblings/probes) pass in your final live run (XFAIL is fine for disputed); scripts/check.sh --quick passes (gofmt, vet, go test ./..., public-content scan).',
    '',
    'COMMIT everything (code, cases, decisions file, harness/observations/live-*-' + label + '/) on your branch with a message ending in the attribution lines the session requires. Do not merge. Report the branch name (git rev-parse --abbrev-ref HEAD) and commit hashes.',
  ].join('\n')
}

phase('Reconcile')
const results = await parallel(args.groups.map((g) => () =>
  agent(prompt(g), { label: 'reconcile:' + g.area, phase: 'Reconcile', isolation: 'worktree', schema: REPORT })))
const done = results.filter(Boolean)
log(done.length + ' of ' + args.groups.length + ' area agents reported; merge their branches least-overlapping first and re-check every adopt-live against 0005/0006')
return done
