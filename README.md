<p align="center">
  <img src="devilfish-logo.svg" alt="Devilfish Logo" width="220" />
</p>

# Devilfish ♟️🦈

**Devilfish** is a modern, high-performance, UCI-compliant chess engine written in Golang. It features 64-bit magic bitboard move generation, dual NNUE and tapered classical evaluation, an advanced search algorithm (PVS with aspiration windows, aggressive Late Move Reductions, and forward pruning), and multithreading powered by **Lazy Symmetric Multiprocessing (Lazy SMP)** over a lockless shared Transposition Table.

---

## Key Features

### 1. Board Representation & Move Generation
- **64-bit Bitboards (`uint64`)** with hardware-accelerated popcount and trailing zeros (`math/bits`).
- **Magic Bitboards** for sliding pieces (Bishops, Rooks, Queens) computing attacks with verified magic multipliers.
- **Pseudo-legal move generation with fast check detection**, achieving **> 43,000,000 NPS** on single-core perft.
- 100% verified correctness on standard perft suites (Startpos, Kiwipete, Position 3, 4, 5).
- **Zobrist Hashing** for positions, castling rights, and en passant files with full 3-fold repetition detection.
- Fast move make/unmake with delta undo stack and zero-allocation check validation (`b.GivesCheck(m)`).

### 2. Evaluation: NNUE & Handcrafted Classical Fallback
- **NNUE (Efficiently Updatable Neural Network)**:
  - **Dual Architecture Engine Support**:
    - **8Chess 1024-wide Architecture** (`eightchess_1024_tuned768.nnue`, 80.4 MB, 84,349,504 bytes):
      - 7,680 piece features ($10$ king input buckets $\times 768$ features) + 66,864 occupancy-aware threat features ($1024$-wide FT).
      - Clipped-pair squaring activation, 8 output buckets (by occupied square count), $1024 \to 16 \to 32 \to 1$ affine layers.
      - ARM64 NEON vectorization (`addVector1024NEON`) and 16k-entry thread-local evaluation cache.
      - Automatically detected and preferred when `eightchess_1024_tuned768.nnue` is present in the workspace or `~/Downloads/`.
    - **Native 768->256 Direct Architecture** (`default.nnue`, 410 KB):
      - Perspective architecture $768 \to 256 \times 2 \to 32 \to 1$ with embedded default weights.
      - Incremental accumulator updates during search with 128-bit ARM64 NEON vectorization.
  - **Runtime Model Switching**: Switch dynamically between networks via `--eval-file <path>` or UCI `setoption name EvalFile value <path>`.
  - **Stockfish networks**: Stockfish network files of version `0x6A448AFA` (threat, pawn-pair and king-relative piece-square inputs, 1024-wide, e.g. `nn-134a887f4c8f.nnue`) load the same way: `./devilfish --eval-file nn-134a887f4c8f.nnue`, `"eval_file"` in `config.json`, or `setoption name EvalFile value <path>`. They are evaluated incrementally with the same kernels as the built-in network, give the same outputs as Stockfish for the same file, and are converted to centipawns with Stockfish's own scaling. Older Stockfish formats are refused with a message. The file is not embedded (about 100 MB).
  - **Classical Fallback**: Toggle NNUE on/off via `--no-nnue` or UCI `setoption name UseNNUE value false`.
- **Classical Tapered Evaluation (HCE)**:
  - Smoothly blends Midgame (MG) and Endgame (EG) scores based on non-pawn material phase.
  - PeSTO-style Piece-Square Tables (PSQT) for all piece types.
  - Doubled, isolated, and protected passed pawns with exponential advance bonuses.
  - Bishop pair, open/semi-open files, 7th rank rooks, and safe minor piece mobility.
  - Midgame pawn shields and open file penalties near kings.
  - **Static Exchange Evaluation (SEE)**: Exact net-gain calculation via swap algorithms for move ordering and pruning.

### 3. Search Engine
- **Principal Variation Search (PVS)** / Negamax with Alpha-Beta pruning.
- **Iterative Deepening** from depth 1 to maximum search depth.
- **Aspiration Windows**: Delta-narrowed search windows centered on previous iteration scores with fail-high/fail-low recovery.
- **Quiescence Search**:
  - Stand-pat score with alpha-beta.
  - Delta pruning.
  - Dynamic evasion search when in check.
  - SEE pruning to eliminate losing captures.
- **Advanced Late Move Reductions (LMR)**:
  - **Precomputed Logarithmic Reduction Curve**: $R = 0.4 + \frac{\ln(\text{depth}) \times \ln(\text{movesSearched})}{2.3}$.
  - **Quiet Move Reductions**: Activated for $\text{depth} \ge 3$ and $\text{movesSearched} \ge 2$.
  - **Dynamic Reduction Scaling**:
    - **Cut-Nodes vs PV-Nodes**: Non-PV nodes increase reduction ($R+1$); PV nodes decrease reduction ($R-1$).
    - **Improving Flag**: Reduces deeper ($R+1$) when static evaluation fails to improve over 2 plies ago, and reduces less ($R-1$) when improving.
    - **Killer & Countermove Protection**: High-likelihood refutations are protected ($R-2$ for killers, $R-1$ for countermoves).
    - **History & Continuation History**: Dynamically scaled by quiet history scores ($\pm 1$ or $\pm 2$).
    - **Check-Giving Moves**: Moves delivering check are reduced less ($R-1$).
  - **Noisy LMR (Late Capture Reductions)**: Reduces late non-quiet moves ($d \ge 4, m \ge 4$) by half base reduction ($R/2$), dynamically adjusted by capture history and Static Exchange Evaluation ($\text{SEE} < 0 \implies R+1$).
  - **Two-Stage Re-Search**: Reduced searches that fail high are immediately re-searched at full depth with a null window, and full-window re-searched in PV nodes.
- **Forward Pruning Techniques**:
  - **Razoring (Pre-Move Pruning)**: At $\text{depth} \le 3$, if $\text{staticEval} + 150 + 125 \times d \le \alpha$, immediately drops into quiescence search; if $q\text{Score} \le \alpha$, the node is razored and returned without generating moves.
  - **Futility Pruning (Move-Loop Pruning)**: At $\text{depth} \le 6$, prunes quiet moves when $\text{staticEval} + 80 \times d \le \alpha$ ($60 \times d$ if `!improving`), safeguarding checks (`b.GivesCheck(m)`), killers, and countermoves.
  - **Reverse Futility Pruning (Static Null Move Pruning)**: At $\text{depth} \le 6$, returns early when $\text{staticEval} - 80 \times d \ge \beta$ ($60 \times d$ if `!improving`).
  - **Null Move Pruning (NMP)**: Adaptive reduction $R = 3 + \lfloor\text{depth}/4\rfloor$ ($+1$ if `!improving`) with zugzwang safeguards.
  - **ProbCut (Probability Cut)**: At $\text{depth} \ge 5$, quickly prunes CUT nodes when a shallow capture search beats $\beta + 200$ at reduced depth ($d - 4$).
  - **Multi-Cut Pruning (MPC)**: In expected CUT nodes ($\text{depth} \ge 6$), prunes when $M \ge 3$ out of $C = 8$ candidate moves produce a beta cutoff at reduced depth ($d - 4$).
  - **Singular Extensions (SE)**: Verifies if the TT move is uniquely superior to all other alternatives; extends depth by +1 (or +2) ply on critical forced lines.
  - **Late Move Pruning (LMP)**: Prunes quiet moves past $3 + 2 \times d^2$ move count at $\text{depth} \le 5$.
  - **History Pruning**: Prunes moves with negative history heuristic scores at shallow depths ($d \le 4$).
  - **SEE Pruning**: Prunes quiet moves with $\text{SEE} < -35 \times d$ and losing captures with $\text{SEE} < -90 \times d$.
  - **Internal Iterative Reduction (IIR)**: At depth $\ge 4$, when no TT move is found in the Transposition Table, reduces the search depth by 1 ply ($d - 1$).
  - **Correction History (`CorrHist`)**: Tracks systematic error between NNUE static evaluation and actual search scores indexed by `PawnHash` (`[2][16384]int16`). Adjusts static evaluation across all pruning decisions and interior node evaluations.
  - **Check Extensions & Mate Distance Pruning**.

### 4. Staged Move Generation & Move Ordering
Devilfish uses a staged (lazy) move picker that yields moves on-demand phase-by-phase, avoiding quiet move generation and scoring entirely whenever a cutoff occurs earlier:
1. **Transposition Table Best Move** (PV move).
2. **Winning & Equal Captures / Queen Promotions** (MVV-LVA + Capture History + SEE $\ge 0$).
3. **Killer Moves** (2 killer slots per ply).
4. **Countermove Heuristic** (response to opponent's immediate previous move).
5. **Quiet Moves**:
   - Check-delivering quiets (`b.GivesCheck(m)`).
   - Quiet history heuristic with gravity damping.
   - 1-ply Continuation History (`prevMove`).
   - 2-ply Continuation History (`prevMove2`).
   - 4-ply Continuation History (`prevMove4`).
6. **Losing Captures** (SEE $< 0$).
7. **Capture History Malus**: Penalizes preceding captures that failed to achieve a beta cutoff.

### 5. Dynamic Time Management
- **Node-Based Root Move Dominance**: Measures the proportion of search nodes spent on the best move at the root. If the top candidate consumes $\ge 65\%$ of nodes, allocated time is reduced by $0.75\times$ to preserve clock; if below $35\%$ (disputed position), time is extended by $1.30\times$.
- **Best Move Stability**: Dynamically tracks whether the root move remains stable across multiple iterative deepening iterations ($\ge 4$), scaling back time consumption to save clock for complex positions.
- **Fail-Low Recovery**: Automatically grants search time extensions when the root evaluation drops unexpectedly ($\Delta > 40$ cp), enabling recovery against hidden tactical refutations.
- **25ms Move Overhead Cushion**: Accounts for IPC pipe and GUI communication latency to guarantee zero loss-on-time forfeits in ultra-fast bullet/blitz scrambles.

### 6. Multithreading: Lazy Symmetric Multiprocessing (Lazy SMP)
- Multiple worker goroutines search the tree concurrently.
- Workers share a unified **Transposition Table** using lockless XOR checksum verification:
  $$\text{Key} = \text{Hash} \oplus \text{Data}$$
  Ensuring 100% thread safety without lock contention or mutex bottlenecks.
- **Engine Search Mutex & Instantaneous Stop**: `Engine.Search` synchronizes search lifecycles via `searchMu`. Worker threads inspect `Stopped` at entry to `Search` and `Quiescence`, guaranteeing sub-millisecond thread termination and 100% responsive `readyok` ping handshakes in tournament managers like Cutechess.
- The master thread drives iterative deepening and reports UCI `info` and `bestmove`.
- Helper threads execute varied iterative deepening iterations, priming the shared TT with deep cutoffs and branch evaluations.

### 7. Syzygy Endgame Tablebases (WDL & DTZ)
Devilfish includes native support for **Syzygy endgame tablebases** (up to 7 pieces) powered by an integrated Fathom probing library:
- **Root Probing (`tb_probe_root`)**: Directly accesses Distance-to-Zero (DTZ) tables at the root position. When tablebase positions are reached at the root, the engine plays the mathematically optimal winning or drawing move instantly with exact mate-distance metrics.
- **Interior Search Probing (`tb_probe_wdl`)**: Probes Win-Draw-Loss (WDL) tables during alpha-beta search at depths $\ge \text{SyzygyProbeDepth}$. Prunes entire game trees immediately upon resolving to tablebase positions, storing exact cutoff scores into the Transposition Table.
- **50-Move Rule Compliance (`Syzygy50MoveRule`)**: Correctly differentiates between unconditional wins/losses and cursed wins / blessed losses drawn by the 50-move rule.
- **UCI Reporting (`tbhits`)**: Tracks tablebase lookups across all worker threads and reports `tbhits <count>` in standard UCI `info` strings.

### 8. Universal Chess Interface (UCI) Compatibility & Configuration
Fully compliant with modern chess GUIs and tournament runners (Cutechess, Arena, Banksia GUI, Fritz, LiChess bot frameworks).

#### Configuration Methods:
1. **JSON Configuration File**:
   Devilfish automatically creates and loads settings from `~/.devilfish/config.json` on startup if it does not exist. All UCI options are fully configurable:
   ```json
   {
     "threads": 10,
     "hash": 2000,
     "ponder": true,
     "use_nnue": true,
     "eval_file": "<embedded>",
     "syzygy_path": "/path/to/syzygy",
     "syzygy_probe_depth": 1,
     "syzygy_50_move_rule": true,
     "syzygy_probe_limit": 6,
     "own_book": true,
     "book_file": "<embedded>"
   }
   ```
   `book_file` selects the opening book:
   - `"<embedded>"` (default): the built-in Polyglot book of mainline theory.
   - `"<pohl>"`: the embedded **Pohl.epd** opening suite (223,081 positions after 8 moves). The engine steers each game toward a randomly chosen suite position that is still reachable, reconstructing a legal move order on the fly (preferring mainline move orders), and leaves the book once that position is reached or the opponent deviates from every suite line.
   - A path to a Polyglot `.bin` file, or to any EPD suite (`.epd` / `.epd.gz`), which is used the same way as `<pohl>`.

   The embedded suites are stored packed (`internal/book/*.epdb.gz`) and decoded only when selected. To embed another suite, pack it with `go run ./cmd/epdpack -in suite.epd -out internal/book/suite.epdb.gz` and register it in `internal/book/epdbook.go`.

   Every UCI option can be set in `config.json`. The settings above cover the standard ones (`Hash`, `Threads`, `Ponder`, `UseNNUE`, `EvalFile`, the `Syzygy*` options, `LogFile`, `Move Overhead`, `MultiPV`, `OwnBook`, `BookFile`). The search tunables (`LMRBase`, `LMRDivisor`, `LMRHistDiv`, `RFPMargin`, `RFPNonImpr`, `RazorMargin`, `NMPEvalDiv`, `LMPMultImpr`, `LMPMultNonImp`, `FutMargin`, `HistPrImpr`, `HistPrNonImp`, `SEEQuietMul`, `SEENoisyMul`, `QSFutMargin`, `CheckExtSEE`, `QSChecks`, `RFPDepth`, `LMPDepth`, `FutDepth`, `HistDepth`, `SEEDepth`, `RFPGuard`, `NMPGuard`, `CheckExt`, `LMRChecks`) go in a `search_params` object or directly at the top level, by their UCI name (case, spaces, `_` and `-` are ignored):
   ```json
   {
     "hash": 256,
     "search_params": { "RFPDepth": 6, "LMRBase": 45 },
     "FutMargin": 100
   }
   ```
   Values are clamped to each option's range (see the `uci` command's output), and a name that matches no option is reported on stderr and ignored. Tunables left out keep their built-in defaults; a GUI's `setoption` still overrides the file.
2. **Command-Line Arguments** (override config file):
   - `-t`, `--threads <N>`: Set number of Lazy SMP search threads.
   - `-hash`, `--hash <MB>`: Set Transposition Table size in MB.
   - `-c`, `--config <path>`: Specify custom config file path.
   - `--eval-file <path>`: Specify external `.nnue` network file.
   - `--no-nnue`: Disable NNUE evaluation and use classical HCE.
   - `--syzygy-path <path>`: Specify directory containing Syzygy `.rtbw` and `.rtbz` files.
   - `--book <path|<embedded>|<pohl>>`: Select the opening book; `--no-book` disables it.
3. **Runtime UCI Options**:
   - `setoption name Threads value <N>`
   - `setoption name Hash value <MB>`
   - `setoption name Ponder value <true|false>`
   - `setoption name UseNNUE value <true|false>`
   - `setoption name EvalFile value <path>`
   - `setoption name SyzygyPath value <path>`
   - `setoption name SyzygyProbeDepth value <depth>`
   - `setoption name Syzygy50MoveRule value <true|false>`
   - `setoption name SyzygyProbeLimit value <limit>`
   - `setoption name OwnBook value <true|false>`
   - `setoption name BookFile value <path|<embedded>|<pohl>>`

#### Protocol Support & Robustness:
- Standard UCI Commands: `uci`, `isready`, `ucinewgame`, `position`, `go`, `stop`, `ponderhit`, `quit`, `ping`.
- **Thread-Safe Stdout**: All engine outputs are synchronized via write mutexes to prevent interleaved or corrupted responses.
- **Airtight Ping Timer Compliance**: Guaranteed immediate `readyok` and `bestmove` responses to satisfy GUI ping timeouts under all race conditions.
- **UCI Pondering**: Full compliance with GUI `go ponder` instructions, cleanly waiting for `ponderhit` or `stop` without premature exits.
- **Continuous Background Auto-Pondering**: When not explicitly commanded by a GUI, the engine calculates the predicted opponent continuation silently in the background, pre-warming the transposition table for instant tactical responses.
- Diagnostic commands: `d` / `display`, `eval`, `perft <depth>`, `bench [depth]`, `bench time|threads|perft|eval|all` (see Performance & Benchmarks), `wac [ms]`, `testsuite <wac|file.epd> [ms]`.

### 9. Performance & Benchmarks

Devilfish includes a built-in tactical benchmark suite covering openings, middlegames, endgames, tactical sacrifices, and promotion tactics:

```bash
./devilfish bench 8
```

```text
Running benchmark at depth 8 with 10 threads...
Position 1/6: bestmove e2e3 | nodes 51200 | time 14.7ms
Position 2/6: bestmove e2a6 | nodes 253952 | time 41.7ms
Position 3/6: bestmove b4f4 | nodes 47104 | time 7.8ms
Position 4/6: bestmove c4c5 | nodes 110592 | time 20.1ms
Position 5/6: bestmove d7c8q | nodes 65536 | time 15.8ms
Position 6/6: bestmove g5f6 | nodes 231424 | time 41.6ms
===============================================
Total nodes: 759808
Total time : 142.2ms
Nodes/sec  : 5342663
===============================================
```

Further benchmark modes, available both from the command line and as commands inside the UCI loop:

```bash
./devilfish bench time 2000      # fixed time per position: nodes/sec and depth reached at the current settings
./devilfish bench threads        # search speed at every thread count, and the fastest setting
./devilfish bench threads 3000 8 # the same with 3 s per position, up to 8 threads
./devilfish bench perft 5        # move generator speed
./devilfish bench eval           # static evaluations per second (NNUE and classical)
./devilfish bench stats 12 -t 1 # search statistics: tree growth, move ordering, what each pruning rule did
./devilfish bench all            # fixed-depth, fixed-time, perft and eval in turn
```

`bench threads` searches the bench positions for a fixed time at each thread count and prints nodes/sec, the speedup over one thread, the efficiency per thread and the average depth reached. It recommends the fewest threads within 5% of the fastest count, since extra threads that add no speed only take cores from other programs. `--threads` and `--hash` apply to every mode (`./devilfish bench time 2000 -t 4 --hash 256`).

```text
Threads     Nodes/sec   Speedup   Per thread   Avg depth
      1        751274     1.00x         100%         9.8
      2       1472033     1.96x          98%         9.7
      4       2952613     3.93x          98%        10.0
      8       5959333     7.93x          99%        10.5
     10       6280648     8.36x          84%        10.2
```

---

## Building & Running

#### Tactical strength: Win At Chess (WAC)

The 300-position **Win At Chess** suite (Fred Reinfeld, in the 2018 revision distributed with the Arasan engine, which fixes broken positions and lists alternative solutions) is embedded for measuring tactical strength:

```bash
./devilfish wac            # 1000 ms per position
./devilfish wac 5000       # 5 s per position
./devilfish testsuite my_suite.epd 2000   # any EPD suite with bm/am operations
```

The same commands work inside the UCI loop (`wac 1000`, `testsuite wac 1000`). Each position is searched from a cleared hash table with the opening book off, using the configured threads and hash. A position counts as solved when the final best move is one of its `bm` moves (and none of its `am` moves); the time shown is when the engine settled on the right move for good. The run ends with the score, nodes/sec and the ids of failed positions.

```text
Running test suite wac: 300 positions, 1000 ms each, 2 threads, 64 MB hash
  1/300 WAC.001  ok   played Qg6     expected bm Qg6 (12 ms)
  2/300 WAC.002  ok   played Rxb2    expected bm Rxb2 (791 ms)
...
===============================================
Solved     : 284/300 (94.7%)
Total time : 3m43.146s
Nodes/sec  : 1055950
Failed     : WAC.074 WAC.087 WAC.089 WAC.131 WAC.141 WAC.145 WAC.157 WAC.163 WAC.193 WAC.208 WAC.222 WAC.230 WAC.241 WAC.268 WAC.270 WAC.291
===============================================
```

Compare scores at the same time per position, thread count and hardware; WAC is easy for modern engines, so the interesting signal is the last ~5% and the time to solution.

Two experimental search options, both off by default, target WAC-style sacrifices: `setoption name CheckExtSEE value <n>` also extends checks that lose up to `n` centipawns of material (2000 extends every check), and `setoption name QSChecks value 1` tries quiet checks at the first quiescence ply. On a 1 s/position single-thread run neither changed the WAC score (279/300 with or without them), so confirm any change with engine-vs-engine games before enabling it.

### Requirements
- Go 1.20+ (tested on Go 1.27)

### Compile
```bash
go build -o devilfish ./cmd/devilfish
```

### Run Benchmark
```bash
./devilfish bench
# Or specify depth:
./devilfish bench 8
```

### Run Perft Suite
```bash
go test -v ./internal/movegen
```

### Run All Unit Tests
```bash
go test -v ./...
```

### Use with a Chess GUI
Point your favorite chess GUI (e.g. Cutechess, Arena, Banksia) to the compiled `./devilfish` executable.

---

## NNUE Training from PGN

Devilfish includes a high-performance training script [`scripts/train_from_pgn.py`](file:///Users/proc/src/devilfish/scripts/train_from_pgn.py) powered by Apple Silicon Metal GPU acceleration (via MLX) and parallel multiprocessing:

```bash
# Basic training from any PGN file (outputs to internal/nnue/default.nnue)
python3 scripts/train_from_pgn.py games.pgn

# Train with custom parameters and compressed PGN (.pgn or .pgn.gz)
python3 scripts/train_from_pgn.py master_games.pgn.gz \
  -o custom.nnue \
  --epochs 50 \
  --max-positions 250000 \
  --workers 10

# Cache dataset to .npz for fast multi-run experimentation
python3 scripts/train_from_pgn.py master_games.pgn --save-dataset data.npz
python3 scripts/train_from_pgn.py --load-dataset data.npz --epochs 60 -o fine_tuned.nnue

# Fine-tune starting from an existing network
python3 scripts/train_from_pgn.py games.pgn --init-weights internal/nnue/default.nnue
```

After training, recompile Devilfish with `go build -o devilfish ./cmd/devilfish` to embed the newly trained default weights, or load external weights dynamically using `./devilfish --eval-file custom.nnue`.

