# `.agents/` — CI workflow drafts awaiting human promotion

Files here are **drafts, not live CI**. Nothing in this directory runs.

GitHub Actions workflow files execute arbitrary code on runners with access to
`GITHUB_TOKEN` and repo secrets, so `.github/workflows/` is human-owned. Agents
draft workflows here instead; a human reviews a draft and promotes it when they
choose to.

## Promoting a draft

```sh
mkdir -p .github/workflows
cp .agents/ci.yml .github/workflows/ci.yml
git add .github/workflows/ci.yml && git commit && git push
```

The workflow starts running on the next `push` / `pull_request` after that.
Keep the draft in place so later agent-authored changes have a base to edit;
promote again to publish them.

### After promotion, the draft is no longer authoritative

Promotion is a copy, and nothing here detects drift. Once `.github/workflows/`
holds a copy, **that copy is what runs** — this directory is only a base to edit
from. An agent can change a draft and, until someone promotes it again, this
repo documents a CI configuration that is not the one checking it, with nothing
reporting the divergence. When the two differ, read `.github/workflows/`.

## Current drafts

| Draft | What it does |
|-------|--------------|
| `ci.yml` | Runs the two commands `README.md` designates as the gate — `python3 bin/validate-intent` (fixture conformance) and `python3 -m unittest discover -s tests` (validator logic) — on Python 3.10 and 3.13. Zero dependencies, so no install step. |
