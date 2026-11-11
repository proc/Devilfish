#!/usr/bin/env python3
"""SPSA tuner for Devilfish search parameters.

Each iteration perturbs every tunable by +/- c_k, plays a small batch of
paired games (theta+ as one side, theta- as the other, colours swapped
within each pair), and nudges theta toward whichever side scored better.
Same schedule as fishtest: a_k = a/(A+k)^0.602, c_k = c/k^0.101.

State is saved to --state after every iteration, so the run can be
interrupted and resumed.

    python3 -I scripts/spsa.py --engine ./devilfish --iters 300 \
        --pairs 8 --concurrency 8 --movetime 0.1

Requires python-chess.
"""
import argparse
import json
import math
import os
import random
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor

import chess
import chess.engine

ALPHA, GAMMA = 0.602, 0.101


def uci_tunables(engine_path):
    """Read the engine's tunable options (name -> (default, min, max))."""
    out = subprocess.run([engine_path], input="uci\nquit\n", capture_output=True,
                         text=True, timeout=30).stdout
    # The engine lists its search tunables as a block after BookFile; skip
    # everything before it and the standard options python-chess manages.
    managed = {"Hash", "Threads", "MultiPV", "Ponder", "Move", "UCI_Chess960"}
    opts = {}
    seen_bookfile = False
    for line in out.splitlines():
        t = line.split()
        if len(t) >= 3 and t[0] == "option" and t[2] == "BookFile":
            seen_bookfile = True
            continue
        if not seen_bookfile:
            continue
        if len(t) >= 11 and t[0] == "option" and t[4] == "spin" and t[2] not in managed:
            opts[t[2]] = (int(t[6]), int(t[8]), int(t[10]))
    return opts


def material(b):
    v = {1: 1, 2: 3, 3: 3, 4: 5, 5: 9, 6: 0}
    return sum(v[p.piece_type] * (1 if p.color else -1) for p in b.piece_map().values())


def random_opening(rng, plies=8):
    while True:
        b = chess.Board()
        for _ in range(plies):
            ms = list(b.legal_moves)
            if not ms:
                break
            b.push(rng.choice(ms))
        if not b.is_game_over() and abs(material(b)) <= 1:
            return b.fen()


def play_game(engine_path, fen, white_opts, black_opts, movetime):
    es = []
    try:
        for opts in (white_opts, black_opts):
            e = chess.engine.SimpleEngine.popen_uci([engine_path, "-t", "1"])
            es.append(e)  # register before configure so a failure still quits it
            cfg = {"OwnBook": False, "Hash": 16}
            cfg.update(opts)
            e.configure(cfg)
        b = chess.Board(fen)
        while not b.is_game_over(claim_draw=True) and b.ply() < 300:
            r = es[0 if b.turn else 1].play(b, chess.engine.Limit(time=movetime))
            b.push(r.move)
        return {"1-0": 1.0, "0-1": 0.0}.get(b.result(claim_draw=True), 0.5)
    finally:
        for e in es:
            try:
                e.quit()
            except Exception:
                pass


def play_pair(engine_path, fen, plus, minus, movetime):
    """Returns theta+'s score over the pair, in [0, 2]."""
    s1 = play_game(engine_path, fen, plus, minus, movetime)
    s2 = 1 - play_game(engine_path, fen, minus, plus, movetime)
    return s1 + s2


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--engine", required=True)
    ap.add_argument("--state", default="spsa_state.json")
    ap.add_argument("--iters", type=int, default=300)
    ap.add_argument("--pairs", type=int, default=8, help="game pairs per iteration")
    ap.add_argument("--concurrency", type=int, default=8)
    ap.add_argument("--movetime", type=float, default=0.1)
    ap.add_argument("--params", default="", help="comma-separated subset of tunables")
    ap.add_argument("--c-frac", type=float, default=0.10,
                    help="final perturbation as a fraction of each parameter's range")
    ap.add_argument("--r-end", type=float, default=0.30,
                    help="final step per unit result, as a fraction of c_end")
    ap.add_argument("--seed", type=int, default=1)
    args = ap.parse_args()

    tun = uci_tunables(args.engine)
    names = [n for n in args.params.split(",") if n] or list(tun)
    for n in names:
        if n not in tun:
            sys.exit(f"unknown tunable {n}; engine offers: {', '.join(tun)}")

    if os.path.exists(args.state):
        st = json.load(open(args.state))
        theta = st["theta"]
        k0 = st["k"]
        print(f"resuming from iteration {k0}", flush=True)
    else:
        theta = {n: float(tun[n][0]) for n in names}
        k0 = 0
        st = {"theta": theta, "k": 0, "start": dict(theta), "history": []}

    rng = random.Random(args.seed + k0)
    N = args.iters
    A = 0.1 * N
    c_end = {n: max(1.0, args.c_frac * (tun[n][2] - tun[n][1])) for n in names}
    c0 = {n: c_end[n] * N ** GAMMA for n in names}
    # a chosen so that the step at k=N equals r_end * c_end per unit result
    a0 = {n: args.r_end * c_end[n] ** 2 * (A + N) ** ALPHA for n in names}

    t_all = time.time()
    for k in range(k0 + 1, N + 1):
        ck = {n: c0[n] / k ** GAMMA for n in names}
        ak = {n: a0[n] / (A + k) ** ALPHA for n in names}
        delta = {n: rng.choice((-1, 1)) for n in names}

        def clamp(n, v):
            return max(tun[n][1], min(tun[n][2], v))

        plus = {n: int(round(clamp(n, theta[n] + ck[n] * delta[n]))) for n in names}
        minus = {n: int(round(clamp(n, theta[n] - ck[n] * delta[n]))) for n in names}

        fens = [random_opening(rng) for _ in range(args.pairs)]
        t0 = time.time()
        with ThreadPoolExecutor(args.concurrency) as ex:
            scores = list(ex.map(lambda f: play_pair(args.engine, f, plus, minus, args.movetime), fens))
        games = 2 * args.pairs
        # result in [-1, 1]: +1 means theta+ won every game
        result = (sum(scores) - args.pairs) / args.pairs

        for n in names:
            grad = result / (2 * ck[n] * delta[n])
            theta[n] = clamp(n, theta[n] + ak[n] * grad)

        st["theta"] = theta
        st["k"] = k
        st["history"].append({"k": k, "result": result, "theta": {n: round(theta[n], 2) for n in names}})
        tmp = args.state + ".tmp"
        json.dump(st, open(tmp, "w"), indent=1)
        os.replace(tmp, args.state)

        el = time.time() - t0
        print(f"iter {k}/{N} result {result:+.3f} ({games} games, {el:.0f}s) "
              + " ".join(f"{n}={theta[n]:.1f}" for n in names), flush=True)

    print(f"done in {(time.time() - t_all) / 60:.1f} min", flush=True)
    print("final:", {n: int(round(theta[n])) for n in names})
    print("start:", {n: int(round(v)) for n, v in st["start"].items()})


if __name__ == "__main__":
    main()
