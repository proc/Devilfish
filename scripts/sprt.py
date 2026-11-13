#!/usr/bin/env python3
"""A/B match between two Devilfish builds with a sequential stopping rule.

Plays the new build against the base build. Each opening is played twice
with colours swapped (a "pair"), and the two results are scored together,
which cancels most of the bias of an unbalanced opening. After every pair
the generalized SPRT (pentanomial model, logistic Elo, as fishtest and
fastchess use) decides between

    H0: new is no better than base by elo0     (reject the change)
    H1: new is better than base by elo1        (accept the change)

and the match stops as soon as the log-likelihood ratio leaves
[log(beta/(1-alpha)), log((1-beta)/alpha)], or at --max-games.

    python3 -I scripts/sprt.py --new ./devilfish-new --base ./devilfish \
        --tc 8+0.08 --concurrency 4 --elo0 0 --elo1 5

Openings: --openings FILE (one FEN per line), or a set generated from short
random playouts of the base engine, kept when the base engine's own eval
is close to level. --save-openings writes the set so later runs reuse it.

Each engine runs with one thread, a 16 MB hash and its own book off, and
never ponders. Clocks are kept by this script: an engine that exceeds its
time loses.

A progress line is printed every --report seconds (default 30): games played,
score, Elo estimate with its 95% interval, the LLR and how far it has moved
toward a decision, the pace, and the time left to the game limit.

--state FILE saves the tally after every pair; rerunning with the same
--state, engines and time control continues where the last run stopped.

Requires python-chess.
"""
import argparse
import json
import math
import os
import random
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor

import chess
import chess.engine

# Ponder is left to python-chess, which manages it (and never ponders here).
BASE_OPTIONS = {"Threads": 1, "Hash": 16, "OwnBook": False}


# ---------------------------------------------------------------- statistics

def expected_score(elo):
    return 1.0 / (1.0 + 10.0 ** (-elo / 400.0))


def pair_stats(penta):
    """Mean and variance of the per-pair score (0, .25, .5, .75, 1)."""
    n = sum(penta)
    if n == 0:
        return 0.5, 0.0, 0
    mu = sum(i / 4.0 * c for i, c in enumerate(penta)) / n
    var = sum(c * (i / 4.0 - mu) ** 2 for i, c in enumerate(penta)) / n
    return mu, var, n


def llr(penta, elo0, elo1):
    """GSPRT log-likelihood ratio (normal approximation on pair scores)."""
    mu, var, n = pair_stats(penta)
    if n < 2 or var <= 0:
        return 0.0
    s0, s1 = expected_score(elo0), expected_score(elo1)
    return n * (s1 - s0) * (2 * mu - s0 - s1) / (2 * var)


def elo_estimate(penta):
    """Elo difference and 95% interval from the pair scores."""
    mu, var, n = pair_stats(penta)
    if n < 2:
        return 0.0, float("inf")

    def to_elo(s):
        s = min(max(s, 1e-6), 1 - 1e-6)
        return -400.0 * math.log10(1.0 / s - 1.0)
    se = math.sqrt(var / n)
    lo, hi = to_elo(mu - 1.96 * se), to_elo(mu + 1.96 * se)
    return to_elo(mu), (hi - lo) / 2


# ------------------------------------------------------------------ openings

def generate_openings(engine_path, count, seed, plies=8, max_eval=120):
    rng = random.Random(seed)
    eng = chess.engine.SimpleEngine.popen_uci(engine_path)
    eng.configure(BASE_OPTIONS)
    seen, out = set(), []
    try:
        while len(out) < count:
            b = chess.Board()
            for _ in range(plies):
                moves = list(b.legal_moves)
                if not moves:
                    break
                b.push(rng.choice(moves))
            if b.is_game_over() or b.fen() in seen:
                continue
            info = eng.analyse(b, chess.engine.Limit(depth=7))
            score = info["score"].white().score(mate_score=100000)
            if abs(score) <= max_eval:
                seen.add(b.fen())
                out.append(b.fen())
    finally:
        eng.quit()
    return out


# --------------------------------------------------------------------- games

class Player:
    def __init__(self, path, options):
        self.path, self.options = path, options
        self.engine = None

    def start(self):
        self.engine = chess.engine.SimpleEngine.popen_uci(self.path)
        self.engine.configure(self.options)

    def restart(self):
        try:
            self.engine.quit()
        except Exception:
            pass
        self.start()

    def quit(self):
        try:
            self.engine.quit()
        except Exception:
            pass


def play_game(fen, white, black, base_s, inc_s, max_plies=400):
    """Returns (result for white: 1, 0.5 or 0; reason)."""
    board = chess.Board(fen)
    clocks = {chess.WHITE: base_s, chess.BLACK: base_s}
    players = {chess.WHITE: white, chess.BLACK: black}
    for p in players.values():
        p.engine.protocol.send_line("ucinewgame")
    big = {chess.WHITE: 0, chess.BLACK: 0}   # consecutive |eval| >= 1000 for side to move
    level = 0                                  # consecutive near-zero evals
    while True:
        if board.is_game_over(claim_draw=True):
            r = board.result(claim_draw=True)
            return {"1-0": 1.0, "0-1": 0.0}.get(r, 0.5), board.outcome(claim_draw=True).termination.name.lower()
        if board.ply() >= max_plies:
            return 0.5, "max plies"
        side = board.turn
        limit = chess.engine.Limit(white_clock=clocks[chess.WHITE], black_clock=clocks[chess.BLACK],
                                   white_inc=inc_s, black_inc=inc_s)
        t0 = time.monotonic()
        try:
            res = players[side].engine.play(board, limit, info=chess.engine.INFO_SCORE)
        except (chess.engine.EngineError, chess.engine.EngineTerminatedError) as e:
            players[side].restart()
            return (0.0 if side == chess.WHITE else 1.0), f"engine error: {e}"
        used = time.monotonic() - t0
        clocks[side] -= used
        if clocks[side] < 0:
            return (0.0 if side == chess.WHITE else 1.0), "time forfeit"
        clocks[side] += inc_s
        if res.move is None or res.move not in board.legal_moves:
            return (0.0 if side == chess.WHITE else 1.0), f"illegal move {res.move}"

        # Adjudication: both sides agree the game is decided, or long dead draw.
        score = res.info.get("score")
        if score is not None:
            cp = score.pov(side).score(mate_score=100000)
            big[side] = big[side] + 1 if abs(cp) >= 1000 else 0
            if big[chess.WHITE] >= 4 and big[chess.BLACK] >= 4:
                white_cp = score.white().score(mate_score=100000)
                return (1.0 if white_cp > 0 else 0.0), "adjudicated win"
            level = level + 1 if abs(cp) <= 10 and board.ply() >= 80 else 0
            if level >= 10:
                return 0.5, "adjudicated draw"
        board.push(res.move)


# ---------------------------------------------------------------------- main

def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--new", required=True, help="engine under test")
    ap.add_argument("--base", required=True, help="reference engine")
    ap.add_argument("--tc", default="8+0.08", help="base+increment in seconds (default 8+0.08)")
    ap.add_argument("--concurrency", type=int, default=2)
    ap.add_argument("--elo0", type=float, default=0.0)
    ap.add_argument("--elo1", type=float, default=5.0)
    ap.add_argument("--alpha", type=float, default=0.05)
    ap.add_argument("--beta", type=float, default=0.05)
    ap.add_argument("--max-games", type=int, default=20000)
    ap.add_argument("--openings", help="file with one FEN per line")
    ap.add_argument("--num-openings", type=int, default=400)
    ap.add_argument("--save-openings", help="write the opening set here")
    ap.add_argument("--seed", type=int, default=1)
    ap.add_argument("--option", action="append", default=[], help="NAME=VALUE for both engines")
    ap.add_argument("--new-option", action="append", default=[], help="NAME=VALUE for the new engine")
    ap.add_argument("--base-option", action="append", default=[], help="NAME=VALUE for the base engine")
    ap.add_argument("--state", help="save progress here and resume from it")
    ap.add_argument("--report", type=float, default=30.0,
                    help="print a progress line every this many seconds (default 30; 0: only every 20 games)")
    args = ap.parse_args()

    base_s, inc_s = (float(x) for x in args.tc.split("+"))

    def opts(extra):
        o = dict(BASE_OPTIONS)
        for kv in args.option + extra:
            k, v = kv.split("=", 1)
            o[k] = v
        return o
    new_opts, base_opts = opts(args.new_option), opts(args.base_option)

    if args.openings:
        with open(args.openings) as f:
            openings = [l.strip() for l in f if l.strip() and not l.startswith("#")]
    else:
        print(f"generating {args.num_openings} openings...", flush=True)
        openings = generate_openings(args.base, args.num_openings, args.seed)
    if args.save_openings:
        with open(args.save_openings, "w") as f:
            f.write("\n".join(openings) + "\n")
    random.Random(args.seed).shuffle(openings)

    lower = math.log(args.beta / (1 - args.alpha))
    upper = math.log((1 - args.beta) / args.alpha)
    penta = [0] * 5       # pairs scoring 0, 0.5, 1, 1.5, 2 for the new engine
    wdl = [0, 0, 0]       # new engine's wins, draws, losses
    reasons = {}
    lock = threading.Lock()
    stop = threading.Event()
    next_pair = [0]
    done = set()          # pair indices already played (pairs finish out of order)
    signature = {"new": os.path.abspath(args.new), "base": os.path.abspath(args.base), "tc": args.tc,
                 "elo0": args.elo0, "elo1": args.elo1, "new_opts": new_opts, "base_opts": base_opts,
                 "openings": len(openings), "seed": args.seed}
    if args.state and os.path.exists(args.state):
        with open(args.state) as f:
            saved = json.load(f)
        if saved.get("signature") != signature:
            sys.exit(f"{args.state} belongs to a different match; remove it or use another --state")
        penta, wdl, reasons = saved["penta"], saved["wdl"], saved["reasons"]
        done.update(saved["done"])
        print(f"resuming after {sum(wdl)} games", flush=True)

    def save_state():
        if not args.state:
            return
        tmp = args.state + ".tmp"
        with open(tmp, "w") as f:
            json.dump({"signature": signature, "penta": penta, "wdl": wdl, "reasons": reasons,
                       "done": sorted(done)}, f)
        os.replace(tmp, args.state)
    verdict = [None]
    started = time.monotonic()
    games_at_start = sum(wdl)

    def clock(seconds):
        seconds = int(seconds)
        h, m, sec = seconds // 3600, seconds // 60 % 60, seconds % 60
        return f"{h}:{m:02d}:{sec:02d}" if h else f"{m}:{sec:02d}"

    def status():
        """One progress line. Call with the lock held."""
        games = sum(wdl)
        elapsed = time.monotonic() - started
        line = f"[{clock(elapsed)}] games {games:5d}/{args.max_games}  W {wdl[0]} D {wdl[1]} L {wdl[2]}"
        if games:
            elo, err = elo_estimate(penta)
            l = llr(penta, args.elo0, args.elo1)
            # How far the LLR has travelled toward the bound it is heading for.
            toward = f"{100 * l / upper:.0f}% to accept" if l >= 0 else f"{100 * l / lower:.0f}% to reject"
            line += (f"  score {100 * (wdl[0] + 0.5 * wdl[1]) / games:.1f}%  elo {elo:+.1f} +/- {err:.1f}"
                     f"  LLR {l:+.2f} [{lower:.2f}, {upper:.2f}] ({toward})  penta {penta}")
        played = games - games_at_start
        if played and elapsed > 0:
            rate = played / elapsed
            line += f"  {60 * rate:.1f} games/min, game limit in {clock((args.max_games - games) / rate)}"
        return line

    finished = threading.Event()

    def reporter():
        while not finished.wait(args.report):
            with lock:
                print(status(), flush=True)

    print(f"new {args.new}\nbase {args.base}\ntc {args.tc}, {len(openings)} openings, "
          f"SPRT elo0={args.elo0} elo1={args.elo1} alpha={args.alpha} beta={args.beta} "
          f"bounds [{lower:.2f}, {upper:.2f}]", flush=True)

    def worker():
        new, base = Player(args.new, new_opts), Player(args.base, base_opts)
        new.start()
        base.start()
        try:
            while not stop.is_set():
                with lock:
                    while next_pair[0] in done:
                        next_pair[0] += 1
                    i = next_pair[0]
                    next_pair[0] += 1
                    if sum(wdl) >= args.max_games or 2 * i >= 2 * len(openings) * 50:
                        return
                fen = openings[i % len(openings)]
                r1, why1 = play_game(fen, new, base, base_s, inc_s)          # new as white
                r2, why2 = play_game(fen, base, new, base_s, inc_s)          # new as black
                s1, s2 = r1, 1.0 - r2
                with lock:
                    if stop.is_set():
                        return
                    done.add(i)
                    penta[int(round((s1 + s2) * 2))] += 1
                    for s in (s1, s2):
                        wdl[0 if s == 1 else 1 if s == 0.5 else 2] += 1
                    for why in (why1, why2):
                        reasons[why] = reasons.get(why, 0) + 1
                    save_state()
                    l = llr(penta, args.elo0, args.elo1)
                    games = sum(wdl)
                    if (args.report <= 0 and games % 20 == 0) or l <= lower or l >= upper:
                        print(status(), flush=True)
                    if l >= upper:
                        verdict[0] = "H1 accepted: the new build is stronger"
                        stop.set()
                    elif l <= lower:
                        verdict[0] = "H0 accepted: no improvement shown"
                        stop.set()
        finally:
            new.quit()
            base.quit()

    if args.report > 0:
        threading.Thread(target=reporter, daemon=True).start()
    with ThreadPoolExecutor(args.concurrency) as ex:
        for _ in range(args.concurrency):
            ex.submit(worker)
    finished.set()

    elo, err = elo_estimate(penta)
    games = sum(wdl)
    print(f"\nfinal: {games} games  W {wdl[0]} D {wdl[1]} L {wdl[2]}  penta {penta}  "
          f"elo {elo:+.1f} +/- {err:.1f}  LLR {llr(penta, args.elo0, args.elo1):+.2f}")
    print("result: " + (verdict[0] or "inconclusive (game limit reached)"))
    print("terminations: " + ", ".join(f"{k} {v}" for k, v in sorted(reasons.items(), key=lambda kv: -kv[1])))
    forfeits = reasons.get("time forfeit", 0)
    if forfeits:
        print(f"WARNING: {forfeits} games lost on time", file=sys.stderr)


if __name__ == "__main__":
    main()
