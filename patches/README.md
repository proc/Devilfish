# Search patches awaiting a match verdict

Each patch applies to the current tree with `git apply patches/<name>.patch`.
None of them is merged: each changes which moves the engine plays, so each has
to win a match first. `scripts/sprt.py` runs that match (see its `--help`).

Results so far come from a 2-core machine at 3+0.03 (most moves reach depth
7-9), one thread and 16 MB hash per engine unless noted, SPRT elo0=0 elo1=10.

| Patch | What it does | Result so far |
|---|---|---|
| `multicut.patch` | When the singular-extension search shows another move also beats beta, return beta instead of searching the node. | 640 games: +3.8 ± 17.5 Elo, LLR -0.15. Inconclusive. |
| `tt-buckets.patch` | Transposition table of 4-entry buckets (one cache line), replacing the shallowest or oldest entry, instead of one entry per index. Adds `internal/tt/bucket_test.go`. | 640 games with Hash=4: -4.9 ± 16.7 Elo, LLR -1.37. Inconclusive, leaning no gain at this time control. |
| `fail-soft-multicut.patch` | Fail-soft search (return the best score found, not alpha/beta), multi-cut, and a cap of 6 double extensions per line. | **Rejected**: 428 games, -35.8 ± 23.3 Elo (H0). Without double extensions it was still -28 ± 41 after 160 games, so fail-soft itself loses with the current parameters, which were all tuned under fail-hard. Kept for reference only. |

Note on double extensions: in the current (fail-hard) search the
`extension = 2` branch of the singular extension can never run. A failing
singular search returns exactly `singularBeta - 1`, never 25 below it.

## Continuing the tests

On a machine with more cores, use a longer time control: the point of these
changes is deep search, and 3+0.03 tests depth 7-9.

    go build -o devilfish-base ./cmd/devilfish
    git apply patches/multicut.patch
    go build -o devilfish-new ./cmd/devilfish
    git apply -R patches/multicut.patch
    python3 -I scripts/sprt.py --new ./devilfish-new --base ./devilfish-base \
        --tc 10+0.1 --concurrency 8 --elo0 0 --elo1 5 --state multicut.json

For `tt-buckets.patch`, add `--option Hash=4` (or another small size) so the
table fills during the game as it does in long searches with a normal hash.

`--state` lets a run be stopped and resumed. The first run generates 400
openings from the base engine; `--save-openings openings.fen` keeps them and
`--openings openings.fen` reuses them, so later tests use the same set.
