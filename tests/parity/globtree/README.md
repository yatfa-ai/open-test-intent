# `tests/parity/globtree` — the recursive-glob fixture tree

Read this before tidying anything in here. Every file, and both of the two odd
things in this tree, are load-bearing fixtures for
`tests/parity/run_parity.sh` section 5 ("recursive `**` — the surface a file
COUNT cannot tell apart from a bug") and are compared byte-for-byte against
`python3 bin/validate-intent`.

The tree exists because `glob.glob(pattern, recursive=True)` differs from every
obvious Go spelling of `**` in ways that a *file count* cannot see:

| path | what it proves |
|---|---|
| `spec/a.json` | `**` matching **zero** path segments — `spec/**/a.json` must find it |
| `spec/models/b.json` | depth 1 |
| `spec/requests/c.json` | depth 1 in a sibling tree |
| `spec/requests/admin/d.json` | depth 2, **and the only invalid document here** |
| `spec/real/b.json` | the symlink target's real path |
| `spec/linked` → `real` | a **symlinked directory**, which Python descends into |
| `spec/.secret/f.json` | a **hidden directory**, which Python never descends into |
| `spec/models/order_spec.rb` | `--source` reaches through `**` too — it is the same expander |
| `spec/real/capture_spec.rb` | reported **twice**: by its real path and through `spec/linked` |

Two of those are the whole point:

* **`spec/linked` must stay a symlink.** `filepath.WalkDir` does not follow
  symlinks and Python does, so a port built on it returns the *same number of
  paths* as the reference over this tree while silently dropping everything
  under `spec/linked`. Replacing the symlink with a copied directory would make
  that bug pass.

* **`spec/requests/admin/d.json` must stay invalid.** It is the deepest file, so
  a port that fails to descend two levels exits 0 where the reference exits 1 —
  a wrong *answer*, not merely a shorter list of passes. Making it valid would
  downgrade that signal to a diff nobody's eye is guaranteed to catch.

`spec/.secret/` is deliberately hidden: `**` must not enter it, while
`spec/**/.secret/*.json` — naming the component literally — still must.
