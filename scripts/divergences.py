#!/usr/bin/env python3
"""List the cases that differed from PageLove in a live run, grouped for
reconciliation.

    scripts/divergences.py harness/observations/live-2026-09-29 [--max-group 10]

Prints JSON: {"dir": ..., "groups": [{"area": ..., "ids": [...], "first_failure": {id: text}}]}.
XFAIL (disputed) and passing cases are left out; large areas are split into
groups of at most --max-group ids, so each reconciliation agent gets a
bounded slice (see .claude/skills/reconcile-live).
"""
import argparse, glob, json, os, subprocess, sys

ap = argparse.ArgumentParser()
ap.add_argument("dir")
ap.add_argument("--max-group", type=int, default=10)
a = ap.parse_args()

root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
listing = subprocess.run(["go", "run", "./harness/cmd/harness", "list"], cwd=root,
                         capture_output=True, text=True, check=True).stdout
area_of = {}
for line in listing.splitlines():
    cols = line.split("\t")
    if len(cols) > 1:
        area_of[cols[0]] = cols[1]

failing = {}
for f in sorted(glob.glob(os.path.join(a.dir, "*.json"))):
    try:
        r = json.load(open(f))
    except Exception:
        continue
    if r.get("outcome") != "fail" or not r.get("case"):
        continue
    if r["case"] not in area_of:
        continue  # a case that has since been renamed or removed
    failing[r["case"]] = (r.get("failures") or [""])[0][:300]

by_area = {}
for cid in sorted(failing):
    by_area.setdefault(area_of[cid], []).append(cid)
groups = []
for area, ids in sorted(by_area.items()):
    for i in range(0, len(ids), a.max_group):
        chunk = ids[i:i + a.max_group]
        groups.append({"area": area if len(ids) <= a.max_group else f"{area}-{i // a.max_group + 1}",
                       "ids": chunk, "first_failure": {c: failing[c] for c in chunk}})
json.dump({"dir": a.dir, "count": len(failing), "groups": groups}, sys.stdout, indent=1)
print()
