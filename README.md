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

### 2. Modern Tapered Evaluation
- **Phase-interpolated Evaluation**: Smoothly blends Midgame (MG) and Endgame (EG) scores based on remaining non-pawn material.
- **PeSTO-style Piece-Square Tables (PSQT)** for all piece types, mathematically mirrored for White and Black.
- **Pawn Structure Analysis**:
  - Doubled pawns penalty.
  - Isolated pawns penalty.
  - Passed pawns with rank-based exponential bonus and protected passer detection.
- **Piece Positional Features**:
  - Bishop pair bonus.
  - Rooks on open and semi-open files.
  - Rooks on the 7th rank.
  - Knight and Bishop safe mobility counts (excluding squares under enemy pawn control).
- **King Safety**:
  - Midgame pawn shields.
  - Open file penalties near the king.
- **Static Exchange Evaluation (SEE)**:
  - Exact net-gain calculation via swap algorithms for move ordering and pruning.

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
  - **Reverse Futility Pruning (Static Null Move Pruning)** at shallow depths.
  - **Razoring** to drop directly into quiescence when static eval is far below alpha.
  - **Null Move Pruning (NMP)** with adaptive reduction $R = 3 + \lfloor\text{depth}/4\rfloor$ and zugzwang safeguards.
  - **Late Move Pruning (LMP)** to prune quiet moves after finding a strong move at shallow depths.
  - **Futility Pruning** at frontier nodes.
  - **Late Move Reductions (LMR)** using logarithmic reduction tables based on depth and move number.
  - **Check Extensions** to search critical checks deeper.
  - **Mate Distance Pruning** to prioritize faster mates.

### 4. Move Ordering
1. **Transposition Table Best Move** (PV move).
2. **Winning & Equal Captures / Promotions** (MVV-LVA + SEE $\ge 0$).
3. **Killer Moves** (2 killer slots per ply).
4. **Countermove Heuristic** (history of responses to opponent's previous move).
5. **History Heuristic** (gravity-damped cutoff bonus table).
6. **Losing Captures** (SEE $< 0$).
7. **Quiet Moves**.

### 5. Multithreading: Lazy Symmetric Multiprocessing (Lazy SMP)
- Multiple worker goroutines search the tree concurrently.
- Workers share a unified **Transposition Table** using lockless XOR verification:
  $$\text{Key} = \text{Hash} \oplus \text{Data}$$
  Ensuring 100% thread safety without lock contention or mutex bottlenecks.
- The master thread drives iterative deepening and reports UCI `info` and `bestmove`.
- Helper threads execute varied iterative deepening iterations, priming the shared TT with deep cutoffs and branch evaluations.

### 6. Universal Chess Interface (UCI) Compatibility
Fully compatible with modern chess GUIs (Cutechess, Arena, Banksia GUI, Fritz, LiChess bot frameworks).

Supports:
- `uci`
- `isready`
- `setoption name Hash value <MB>`
- `setoption name Threads value <N>`
- `ucinewgame`
- `position [startpos | fen <fen>] [moves <m1> ...]`
- `go [wtime <ms>] [btime <ms>] [winc <ms>] [binc <ms>] [movestogo <n>] [depth <d>] [nodes <n>] [movetime <ms>] [infinite]`
- `stop`
- `quit`
- Diagnostic commands: `d` / `display`, `eval`, `perft <depth>`, `bench [depth]`.

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
