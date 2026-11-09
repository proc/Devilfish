# Devilfish ♟️🦈

**Devilfish** is a modern, high-performance, UCI-compliant chess engine written in Golang. It features 64-bit magic bitboard move generation, tapered midgame/endgame evaluation, an advanced search algorithm (PVS with aspiration windows and aggressive pruning/reductions), and multithreading powered by **Lazy Symmetric Multiprocessing (Lazy SMP)** over a lockless shared Transposition Table.

---

## Key Features

### 1. Board Representation & Move Generation
- **64-bit Bitboards (`uint64`)** with hardware-accelerated popcount and trailing zeros (`math/bits`).
- **Magic Bitboards** for sliding pieces (Bishops, Rooks, Queens) computing attacks with verified magic multipliers.
- **Pseudo-legal move generation with fast check detection**, achieving **> 43,000,000 NPS** on single-core perft.
- 100% verified correctness on standard perft suites (Startpos, Kiwipete, Position 3, 4, 5).
- **Zobrist Hashing** for positions, castling rights, and en passant files with full 3-fold repetition detection.
- Fast move make/unmake with delta undo stack.

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
- **Pruning & Reductions**:
  - **The `improving` Flag Heuristic**: Tracks whether the static evaluation has improved compared to two plies ago (same player's previous turn), dynamically tuning pruning aggressiveness and LMR depth reductions.
  - **Reverse Futility Pruning (Static Null Move Pruning)** at shallow depths, tightened when `!improving`.
  - **Razoring** to drop directly into quiescence when static eval is far below alpha.
  - **Null Move Pruning (NMP)** with adaptive reduction $R = 3 + \lfloor\text{depth}/4\rfloor$ (+1 if `!improving`) and zugzwang safeguards.
  - **ProbCut (Probability Cut)** at depth $\ge 5$, quickly pruning CUT nodes when a shallow capture search beats $\beta + 200$ at reduced depth ($d - 4$).
  - **Multi-Cut Pruning (MPC)** in expected CUT nodes (depth $\ge 6$), pruning the sub-tree when $M \ge 3$ candidate moves out of $C = 8$ cause a beta cutoff at reduced depth ($d - 1 - 3$).
  - **Singular Extensions (SE)** to verify whether the Transposition Table move is uniquely superior to all other alternatives; if so, extends search depth by +1 (or +2) ply to explore critical forced lines deeper.
  - **Late Move Pruning (LMP)** to prune quiet moves after finding a strong move at shallow depths (scaled by `improving`).
  - **History Pruning**: Pruning unpromising quiet moves whose history heuristic scores are significantly negative at shallow depths ($d \le 4$), bypassing moves that have consistently failed to produce cutoffs.
  - **Futility Pruning** at frontier nodes.
  - **Late Move Reductions (LMR)** using logarithmic reduction tables dynamically scaled by the `improving` flag, quiet history, and continuation history.
  - **Check Extensions** to search critical checks deeper.
  - **Mate Distance Pruning** to prioritize faster mates.

### 4. Move Ordering
1. **Transposition Table Best Move** (PV move).
2. **Winning & Equal Captures / Promotions** (MVV-LVA + Capture History + SEE $\ge 0$).
3. **Killer Moves** (2 killer slots per ply).
4. **Countermove Heuristic** (history of responses to opponent's previous move).
5. **History & Continuation History** (gravity-damped cutoff bonus table + 1-ply `conthist`).
6. **Losing Captures** (SEE $< 0$).
7. **Quiet Moves**.

### 5. Dynamic Time Management
- **Best Move Stability**: Dynamically tracks whether the root move remains stable across multiple iterative deepening iterations ($\ge 4$), scaling back time consumption to save clock for complex positions.
- **Fail-Low Recovery**: Automatically grants search time extensions when the root evaluation drops unexpectedly ($\Delta > 40$ cp), enabling recovery against hidden tactical refutations.

### 6. Multithreading: Lazy Symmetric Multiprocessing (Lazy SMP)
- Multiple worker goroutines search the tree concurrently.
- Workers share a unified **Transposition Table** using lockless XOR checksum verification:
  $$\text{Key} = \text{Hash} \oplus \text{Data}$$
  Ensuring 100% thread safety without lock contention or mutex bottlenecks.
- The master thread drives iterative deepening and reports UCI `info` and `bestmove`.
- Helper threads execute varied iterative deepening iterations, priming the shared TT with deep cutoffs and branch evaluations.

### 7. Universal Chess Interface (UCI) Compatibility & Configuration
Fully compatible with modern chess GUIs (Cutechess, Arena, Banksia GUI, Fritz, LiChess bot frameworks).

#### Configuration Methods:
1. **JSON Configuration File**:
   Devilfish automatically creates and loads settings from `~/.devilfish/config.json` on startup if it does not exist (defaulting to 10 threads):
   ```json
   {
     "threads": 10,
     "hash": 64
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
   - `setoption name UseNNUE value <true|false>`
   - `setoption name EvalFile value <path>`

Supports:
- `uci`
- `isready`
- `setoption name Hash value <MB>`
- `setoption name Threads value <N>`
- `setoption name UseNNUE value <true|false>`
- `setoption name EvalFile value <path>`
- `ucinewgame`
- `position [startpos | fen <fen>] [moves <m1> ...]`
- `go [wtime <ms>] [btime <ms>] [winc <ms>] [binc <ms>] [movestogo <n>] [depth <d>] [nodes <n>] [movetime <ms>] [infinite]`
- `stop`
- `quit`
- Diagnostic commands: `d` / `display`, `eval`, `perft <depth>`, `bench [depth] [threads]`.

### 8. Performance & Benchmarks

Devilfish includes a built-in tactical benchmark suite covering opening, middlegame, endgames, tactical sacrifices, and promotion tactics:

```bash
./devilfish bench 9
```

| Suite Position | FEN Category | Target Best Move | Nodes Searched | Single-Thread Time |
| :--- | :--- | :--- | :--- | :--- |
| **Position 1** | Standard Opening | `e2e4` | 34,816 | 18.0 ms |
| **Position 2** | Tactical Knight Sac | `e2a6` | 256,000 | 119.9 ms |
| **Position 3** | Greek Gift Sac | `b4f4` | 67,584 | 15.6 ms |
| **Position 4** | Queen Trapping | `c4c5` | 184,320 | 79.9 ms |
| **Position 5** | Promotion Tactic | `d7c8q` | 174,080 | 79.3 ms |
| **Position 6** | Central Breakthrough | `c3d5` | 114,688 | 46.9 ms |
| **Total** | **Benchmark Depth 9** | **6/6 Solved** | **831,488 nodes** | **359.9 ms (2.31M NPS)** |

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
