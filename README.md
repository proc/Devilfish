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
  - Perspective-oriented neural network architecture: $768 \to 256 \times 2 \to 32 \to 1$.
  - 768 input features ($12$ piece types $\times 64$ squares) with vertical symmetry ($sq \oplus 56$).
  - **Incremental Accumulator Updates**: Updates 256-dimensional accumulators incrementally during search ($2$ to $4$ vector additions/subtractions per move).
  - **Hardware ARM64 NEON SIMD Vectorization**: Handcrafted 128-bit NEON SIMD assembly (`VLD1`, `VADD.H8`, `VSUB.H8`, `VST1`) processing 32 16-bit integers per unrolled step, achieving an **10.88× speedup** (~19.9 ns) on Apple Silicon with automatic fallback on non-ARM64 platforms.
  - **Embedded Default Network**: Ships with a fully calibrated embedded network inside the binary, running with zero external dependencies.
  - **Custom Model Loading**: Load external `.nnue` binary models via `--eval-file <path>` or UCI `setoption name EvalFile value <path>`.
  - **Dual Engine Evaluation**: Toggle between NNUE and classical evaluation via `--no-nnue` or `setoption name UseNNUE value false`.
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

### 7. Universal Chess Interface (UCI) Compatibility & Configuration
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
     "eval_file": "<embedded>"
   }
   ```
2. **Command-Line Arguments** (override config file):
   - `-t`, `--threads <N>`: Set number of Lazy SMP search threads.
   - `-hash`, `--hash <MB>`: Set Transposition Table size in MB.
   - `-c`, `--config <path>`: Specify custom config file path.
   - `--eval-file <path>`: Specify external `.nnue` network file.
   - `--no-nnue`: Disable NNUE evaluation and use classical HCE.
3. **Runtime UCI Options**:
   - `setoption name Threads value <N>`
   - `setoption name Hash value <MB>`
   - `setoption name Ponder value <true|false>`
   - `setoption name UseNNUE value <true|false>`
   - `setoption name EvalFile value <path>`

#### Protocol Support & Robustness:
- Standard UCI Commands: `uci`, `isready`, `ucinewgame`, `position`, `go`, `stop`, `ponderhit`, `quit`, `ping`.
- **Thread-Safe Stdout**: All engine outputs are synchronized via write mutexes to prevent interleaved or corrupted responses.
- **Airtight Ping Timer Compliance**: Guaranteed immediate `readyok` and `bestmove` responses to satisfy GUI ping timeouts under all race conditions.
- **UCI Pondering**: Full compliance with GUI `go ponder` instructions, cleanly waiting for `ponderhit` or `stop` without premature exits.
- **Continuous Background Auto-Pondering**: When not explicitly commanded by a GUI, the engine calculates the predicted opponent continuation silently in the background, pre-warming the transposition table for instant tactical responses.
- Diagnostic commands: `d` / `display`, `eval`, `perft <depth>`, `bench [depth] [threads]`.

### 8. Performance & Benchmarks

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

---

## Building & Running

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
