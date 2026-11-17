# Evidence

Each `M<N>/` folder holds only `EVIDENCE.md`, the committed record of a milestone: exact excerpts
(at most ~20 lines) from the logs of the run under review, and a one-line description of every
screenshot (page, width, theme, what it shows) with its local path. New milestones follow
[TEMPLATE.md](TEMPLATE.md).

Raw artifacts (logs, screenshots, per-commit check logs, plan files) live only in the gitignored
`tmp/evidence/<milestone>/` on the machine that produced them, while the milestone is under review.
They are never committed (DEC-26; `scripts/check-policy` enforces it).

The raw artifacts of M0–M2 and M3a were removed on 2026-11-17 after the owner's review. They remain
in git history (any commit before that change). Links in the older `EVIDENCE.md` files to `logs/…`
files or screenshots point to those removed files.
