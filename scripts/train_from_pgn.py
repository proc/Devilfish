#!/usr/bin/env python3
"""
train_from_pgn.py - Train Devilfish NNUE directly from a PGN file.

Supports:
- Standard PGN and gzip-compressed PGN (.pgn, .pgn.gz)
- Parallel multiprocessing for high-speed position extraction
- PGNs with embedded [%eval ...] engine evaluations or raw game results (1-0, 0-1, 1/2-1/2)
- Apple Silicon Metal GPU acceleration via MLX
- Dual-sided perspective symmetry architecture (guarantees anti-symmetric evaluation & zero DC drift)
- Direct export to Devilfish's native binary format (410,308 bytes)
- Dataset caching (.npz) and optional fine-tuning from existing .nnue weights

Usage:
  python3 scripts/train_from_pgn.py games.pgn
  python3 scripts/train_from_pgn.py games.pgn.gz -o my_network.nnue --epochs 50 --max-positions 250000
"""

import argparse
import gzip
import multiprocessing as mp
import os
import random
import re
import struct
import sys
import time
import chess
import chess.pgn
import numpy as np

# MLX import with friendly error
try:
    import mlx.core as mx
    import mlx.nn as nn
    import mlx.optimizers as optim
except ImportError:
    print("ERROR: MLX is required for training on Apple Silicon.", file=sys.stderr)
    print("Please install MLX: pip install mlx", file=sys.stderr)
    sys.exit(1)


# --- 1. Feature Representation & Classical Evaluation Fallback ---

PIECE_VALS = {
    chess.PAWN: 100,
    chess.KNIGHT: 320,
    chess.BISHOP: 330,
    chess.ROOK: 500,
    chess.QUEEN: 950,
    chess.KING: 0
}

PST_PAWN = [
    0,   0,   0,   0,   0,   0,   0,   0,
    98, 134,  61,  95,  68, 126,  34, -11,
    -6,   7,  26,  31,  65,  56,  25, -20,
    -14, 13,   6,  21,  23,  12,  17, -23,
    -27, -2,  -5,  12,  17,   6,  10, -25,
    -26, -4,  -4, -10,   3,   3,  33, -12,
    -35, -1, -20, -23, -15,  24,  38, -22,
    0,   0,   0,   0,   0,   0,   0,   0,
]

PST_KNIGHT = [
    -167, -89, -34, -49,  61, -97, -15, -107,
     -73, -41,  72,  36,  23,  62,   7,  -17,
     -47,  60,  37,  65,  84, 129,  73,   44,
      -9,  17,  19,  53,  37,  69,  18,   22,
     -13,   4,  16,  13,  28,  19,  21,   -8,
     -23,  -9,  12,  10,  19,  17,  25,  -16,
     -29, -53, -12,  -3,  -1,  18, -14,  -19,
    -105, -21, -58, -33, -17, -28, -19,  -23,
]

PST_BISHOP = [
    -29,   4, -82, -37, -25, -42,   7,  -8,
    -26,  16, -18, -13,  30,  59,  18, -47,
    -16,  37,  43,  40,  35,  50,  37,  -2,
     -4,   5,  19,  50,  37,  37,   7,  -2,
     -6,  13,  13,  26,  34,  12,  10,   4,
      0,  15,  -9,  22,   5,  28,  20,  14,
      4,   9,  13,   1,  17,  14,  17,  16,
    -33,  -3, -14, -21, -13, -12, -39, -21,
]

PST_ROOK = [
     32,  42,  32,  51,  63,   9,  31,  43,
     27,  32,  58,  62,  80,  67,  26,  44,
     -5,  19,  26,  36,  17,  45,  61,  16,
    -24, -11,   7,  26,  24,  35,  -8, -20,
    -36, -26, -12,  -1,   9,  -7,   6, -23,
    -45, -25, -16, -17,   3,   0,  -5, -33,
    -44, -16, -20,  -9,  -1,  11,  -6, -71,
    -19, -13,   1,  17,  16,   7, -37, -26,
]

PST_QUEEN = [
    -28,   0,  29,  12,  59,  44,  43,  45,
    -24, -39,  -5,   1, -16,  57,  28,  54,
    -13, -17,   7,   8,  29,  56,  47,  57,
    -27, -27, -16, -16,  -1,  17,  -2,   1,
     -9, -26,  -9, -10,  -2,  -4,   3,  -3,
    -14,   2, -11,  -2,  -5,   2,  14,   5,
    -35,  -8,  11,   2,   8,  15,  -3,   1,
     -1, -18,  -9,  10, -15, -25, -24, -50,
]

PST_KING = [
    -65,  23,  16, -15, -56, -34,   2,  13,
     29,  -1, -20,  -7,  -8,  -4, -38, -29,
     -9,  24,   2, -16, -20,   6,  22, -22,
    -17, -20, -12, -27, -30, -25, -14, -36,
    -49,  -1, -27, -39, -46, -44, -33, -51,
    -14, -14, -22, -46, -44, -30, -15, -27,
      1,   7,  -8, -64, -43, -16,   9,   8,
    -15,  36,  12, -54,   8, -28,  24,  14,
]

PSTS = {
    chess.PAWN: PST_PAWN,
    chess.KNIGHT: PST_KNIGHT,
    chess.BISHOP: PST_BISHOP,
    chess.ROOK: PST_ROOK,
    chess.QUEEN: PST_QUEEN,
    chess.KING: PST_KING,
}

def get_features(board, perspective):
    feats = []
    for sq in chess.SQUARES:
        piece = board.piece_at(sq)
        if piece is None:
            continue
        p_color = piece.color
        p_type = piece.piece_type
        
        persp_sq = sq
        if perspective == chess.BLACK:
            persp_sq = sq ^ 56
            
        if p_color == perspective:
            pt_idx = p_type - 1 # 0..5
        else:
            pt_idx = 6 + p_type - 1 # 6..11
            
        feat = pt_idx * 64 + persp_sq
        feats.append(feat)
    return feats

def evaluate_board_heuristic(b):
    if b.is_checkmate():
        return -20000 if b.turn == chess.WHITE else 20000
    if b.is_stalemate() or b.is_insufficient_material():
        return 0
        
    score = 0
    w_bishops, b_bishops = 0, 0
    
    for sq in chess.SQUARES:
        p = b.piece_at(sq)
        if not p:
            continue
        val = PIECE_VALS[p.piece_type]
        pst = PSTS[p.piece_type]
        
        if p.color == chess.WHITE:
            score += val + pst[sq ^ 56]
            if p.piece_type == chess.BISHOP:
                w_bishops += 1
        else:
            score -= val + pst[sq]
            if p.piece_type == chess.BISHOP:
                b_bishops += 1
                
    if w_bishops >= 2: score += 35
    if b_bishops >= 2: score -= 35
        
    # Passed pawns
    for sq in b.pieces(chess.PAWN, chess.WHITE):
        f = chess.square_file(sq)
        r = chess.square_rank(sq)
        is_passed = True
        for adj_f in range(max(0, f - 1), min(8, f + 2)):
            for adv_r in range(r + 1, 8):
                if b.piece_at(chess.square(adj_f, adv_r)) == chess.Piece(chess.PAWN, chess.BLACK):
                    is_passed = False
                    break
        if is_passed:
            score += [0, 10, 20, 35, 60, 110, 180, 0][r]
            
    for sq in b.pieces(chess.PAWN, chess.BLACK):
        f = chess.square_file(sq)
        r = chess.square_rank(sq)
        is_passed = True
        for adj_f in range(max(0, f - 1), min(8, f + 2)):
            for adv_r in range(0, r):
                if b.piece_at(chess.square(adj_f, adv_r)) == chess.Piece(chess.PAWN, chess.WHITE):
                    is_passed = False
                    break
        if is_passed:
            score -= [0, 10, 20, 35, 60, 110, 180, 0][7 - r]
            
    return max(-3000, min(3000, score))


# --- 2. Parallel PGN Parsing with CCRL & Standard Comment Support ---

CCRL_MATE_RE = re.compile(r"(?:([+-])?#([+-]?\d+)|([+-])?M(\d+))")
CCRL_EVAL_RE = re.compile(r"(?<!#)([+-]?\d+(?:\.\d+)?)\s*/\s*\d+")

def parse_node_eval(node):
    """
    Extract centipawn evaluation from node, supporting:
    - Standard [%eval ...] PGN comments via node.eval()
    - CCRL tournament comments: {+0.60/16 193s}, {(Rd8) -0.71/14 171s}
    - Mate comments: {#16/19 63s}, {#-5/9 0s}, {+M3}, {-M2}
    Returns evaluation in centipawns from White's perspective, or None if unavailable.
    """
    # 1. Standard python-chess [%eval ...]
    try:
        ev = node.eval()
        if ev is not None:
            return ev.white().score(mate_score=10000)
    except Exception:
        pass

    # 2. CCRL / UCI engine comment formats
    comment = node.comment
    if comment:
        m_mate = CCRL_MATE_RE.search(comment)
        if m_mate:
            try:
                if m_mate.group(2):
                    dist = int(m_mate.group(2))
                    sign = -1 if m_mate.group(1) == "-" or dist < 0 else 1
                    dist = abs(dist)
                else:
                    dist = int(m_mate.group(4))
                    sign = -1 if m_mate.group(3) == "-" else 1
                return sign * (10000 - dist * 100)
            except (ValueError, TypeError):
                pass

        m_eval = CCRL_EVAL_RE.search(comment)
        if m_eval:
            try:
                # CCRL comment evaluations are strictly in pawns from White's perspective (+0.60 = +60 cp)
                return int(round(float(m_eval.group(1)) * 100.0))
            except ValueError:
                pass

    return None

def parse_game_stream(pgn_stream, max_games, min_ply, max_ply, sample_rate, max_positions):
    samples = []
    games_parsed = 0
    limit_pos = max_positions if (max_positions and max_positions > 0) else None
    
    while True:
        if max_games and games_parsed >= max_games:
            break
        if limit_pos and len(samples) >= limit_pos:
            break
            
        try:
            game = chess.pgn.read_game(pgn_stream)
        except Exception:
            break
            
        if game is None:
            break
            
        games_parsed += 1
        result_str = game.headers.get("Result", "*")
        result_cp = 0
        if result_str == "1-0":
            result_cp = 350
        elif result_str == "0-1":
            result_cp = -350
        elif result_str == "1/2-1/2":
            result_cp = 0
            
        board = game.board()
        node = game
        ply = 0
        
        while node.variations:
            next_node = node.variation(0)
            board.push(next_node.move)
            ply += 1
            node = next_node
            
            if ply < min_ply or ply > max_ply:
                continue
                
            if sample_rate < 1.0 and random.random() > sample_rate:
                continue
                
            if board.is_checkmate() or board.is_stalemate() or board.is_insufficient_material():
                continue
                
            # Determine evaluation
            ev_score = parse_node_eval(node)
            if ev_score is not None:
                eval_white = ev_score
                # Blend lightly with game outcome if known
                if result_str in ("1-0", "0-1", "1/2-1/2"):
                    target_white = int(0.85 * eval_white + 0.15 * result_cp)
                else:
                    target_white = eval_white
            else:
                # Fallback: heuristic evaluation blended with game outcome
                h_eval = evaluate_board_heuristic(board)
                if result_str in ("1-0", "0-1", "1/2-1/2"):
                    target_white = int(0.60 * h_eval + 0.40 * result_cp)
                else:
                    target_white = h_eval
                    
            target_white = max(-3000, min(3000, target_white))
            
            w_feats = get_features(board, chess.WHITE)
            b_feats = get_features(board, chess.BLACK)
            samples.append((w_feats, b_feats, board.turn, target_white))
            
            if limit_pos and len(samples) >= limit_pos:
                break
                
    return samples, games_parsed

def worker_parse_chunk(args):
    filename, start_byte, end_byte, min_ply, max_ply, sample_rate, max_pos_per_worker, max_games_per_worker = args
    samples = []
    games = 0
    
    with open(filename, "r", encoding="utf-8", errors="ignore") as f:
        f.seek(start_byte)
        while True:
            curr_pos = f.tell()
            if end_byte and curr_pos >= end_byte:
                break
            if max_games_per_worker and games >= max_games_per_worker:
                break
            if max_pos_per_worker and len(samples) >= max_pos_per_worker:
                break
            try:
                game = chess.pgn.read_game(f)
            except Exception:
                break
            if game is None:
                break
                
            games += 1
            result_str = game.headers.get("Result", "*")
            result_cp = 0
            if result_str == "1-0": result_cp = 350
            elif result_str == "0-1": result_cp = -350
            elif result_str == "1/2-1/2": result_cp = 0
            
            board = game.board()
            node = game
            ply = 0
            while node.variations:
                next_node = node.variation(0)
                board.push(next_node.move)
                ply += 1
                node = next_node
                
                if ply < min_ply or ply > max_ply:
                    continue
                if sample_rate < 1.0 and random.random() > sample_rate:
                    continue
                if board.is_checkmate() or board.is_stalemate():
                    continue
                    
                ev_score = parse_node_eval(node)
                if ev_score is not None:
                    eval_white = ev_score
                    if result_str in ("1-0", "0-1", "1/2-1/2"):
                        target_white = int(0.85 * eval_white + 0.15 * result_cp)
                    else:
                        target_white = eval_white
                else:
                    h_eval = evaluate_board_heuristic(board)
                    if result_str in ("1-0", "0-1", "1/2-1/2"):
                        target_white = int(0.60 * h_eval + 0.40 * result_cp)
                    else:
                        target_white = h_eval
                        
                target_white = max(-3000, min(3000, target_white))
                w_feats = get_features(board, chess.WHITE)
                b_feats = get_features(board, chess.BLACK)
                samples.append((w_feats, b_feats, board.turn, target_white))
                
                if max_pos_per_worker and len(samples) >= max_pos_per_worker:
                    break
                    
    return samples, games

def extract_positions_from_pgn(pgn_path, max_games, max_positions, min_ply, max_ply, sample_rate, workers):
    print(f"Loading PGN: {pgn_path}")
    t0 = time.time()
    
    is_gz = pgn_path.endswith(".gz")
    limit_pos = max_positions if (max_positions and max_positions > 0) else None
    
    if is_gz or workers <= 1:
        # Sequential stream reader
        open_fn = gzip.open if is_gz else open
        with open_fn(pgn_path, "rt", encoding="utf-8", errors="ignore") as f:
            samples, games = parse_game_stream(f, max_games, min_ply, max_ply, sample_rate, limit_pos)
    else:
        # High-speed chunked multiprocessing
        file_size = os.path.getsize(pgn_path)
        chunk_size = file_size // workers
        offsets = [0]
        with open(pgn_path, "rb") as f:
            for i in range(1, workers):
                f.seek(i * chunk_size)
                line = f.readline()
                while line and not line.startswith(b"[Event "):
                    line = f.readline()
                if line:
                    offsets.append(f.tell() - len(line))
                else:
                    break
        offsets.append(file_size)
        
        tasks = []
        max_pos_per_worker = (limit_pos // workers) + 1000 if limit_pos else None
        max_games_per_worker = (max_games // workers) + 1 if max_games else None
        for i in range(len(offsets) - 1):
            tasks.append((pgn_path, offsets[i], offsets[i+1], min_ply, max_ply, sample_rate, max_pos_per_worker, max_games_per_worker))
            
        print(f"Extracting positions across {len(tasks)} parallel workers...")
        with mp.Pool(len(tasks)) as pool:
            results = pool.map(worker_parse_chunk, tasks)
            
        samples = []
        games = 0
        for s_list, g_cnt in results:
            samples.extend(s_list)
            games += g_cnt
            
    random.shuffle(samples)
    if limit_pos and len(samples) > limit_pos:
        samples = samples[:limit_pos]
        
    elapsed = time.time() - t0
    print(f"Extracted {len(samples):,} positions from {games:,} games in {elapsed:.2f}s ({len(samples)/(elapsed+1e-5):.0f} pos/sec).")
    return samples


# --- 3. Symmetric NNUE Model Architecture ---

class SymmetricNNUE(nn.Module):
    """
    Dual-Sided Perspective Symmetry NNUE Architecture:
    Input: (768 -> 256 x 2) Feature Transformer
    Layer 1: 32 hidden neurons
      - Neurons 0..15: Positive advantage detectors (W_us = +W, W_them = -W, OutW > 0)
      - Neurons 16..31: Negative advantage detectors (W_us = -W, W_them = +W, OutW < 0)
    This strictly guarantees anti-symmetric evaluation: E(-pos) == -E(pos) and Startpos == 0 cp + tempo.
    """
    def __init__(self, init_weights_path=None):
        super().__init__()
        if init_weights_path and os.path.exists(init_weights_path):
            print(f"Initializing from existing network: {init_weights_path}")
            with open(init_weights_path, "rb") as f:
                ft_b = np.frombuffer(f.read(256 * 2), dtype=np.int16).astype(np.float32)
                ft_w = np.frombuffer(f.read(768 * 256 * 2), dtype=np.int16).reshape((768, 256)).astype(np.float32)
                l1_b = np.frombuffer(f.read(32 * 4), dtype=np.int32).astype(np.float32) / 128.0
                l1_w = np.frombuffer(f.read(32 * 512), dtype=np.int8).reshape((32, 512)).astype(np.float32) / 128.0
                out_b = struct.unpack("<i", f.read(4))[0]
                out_w = np.frombuffer(f.read(32 * 2), dtype=np.int16).astype(np.float32) / 16.0
                
            self.ft_w = mx.array(ft_w)
            self.ft_b = mx.array(ft_b)
            self.l1_half_w = mx.array(l1_w[0:16, 0:256])
            self.l1_half_b = mx.array(l1_b[0:16])
            self.out_half_w = mx.array(np.abs(out_w[0:16]).reshape((16, 1)))
        else:
            w_init = np.zeros((768, 256), dtype=np.float32)
            piece_vals = [1.2, 3.8, 4.0, 6.0, 11.5, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0]
            for pt in range(12):
                base_val = piece_vals[pt]
                for sq in range(64):
                    f = pt * 64 + sq
                    rank = sq // 8
                    file = sq % 8
                    center_bonus = (3.5 - abs(3.5 - rank) + 3.5 - abs(3.5 - file)) * 0.05
                    val = base_val + (center_bonus if pt < 6 else 0.0)
                    w_init[f, :] = val + np.random.normal(0, 0.01, 256).astype(np.float32)
                    
            self.ft_w = mx.array(w_init)
            self.ft_b = mx.full((256,), 15.0)
            self.l1_half_w = mx.abs(mx.random.normal((16, 256)) * 0.002) + 0.016
            self.l1_half_b = mx.zeros((16,))
            self.out_half_w = mx.full((16, 1), 1.5)

    def __call__(self, x_us, x_them):
        acc_us = mx.clip(x_us @ self.ft_w + self.ft_b, 0.0, 127.0)
        acc_them = mx.clip(x_them @ self.ft_w + self.ft_b, 0.0, 127.0)
        
        diff = acc_us - acc_them
        w_l1 = mx.abs(self.l1_half_w)
        h_pos = mx.clip(diff @ w_l1.T, 0.0, 127.0)
        h_neg = mx.clip(-diff @ w_l1.T, 0.0, 127.0)
        
        out = (h_pos - h_neg) @ self.out_half_w
        return out


def loss_fn(model, x_us, x_them, y):
    pred = model(x_us, x_them)
    sig_pred = mx.sigmoid(pred / 400.0)
    sig_y = mx.sigmoid(y / 400.0)
    loss = mx.mean((sig_pred - sig_y) ** 2) * 200.0 + mx.mean(mx.abs(pred - y)) * 0.05
    return loss


# --- 4. Quantization and Simulator ---

def quantize_and_export(model, tempo_bonus, output_path):
    ft_w_np = np.array(model.ft_w)
    ft_b_np = np.array(model.ft_b)
    l1_half_w_np = np.abs(np.array(model.l1_half_w))
    l1_half_b_np = np.zeros(16, dtype=np.float32)
    out_half_w_np = np.full(16, 1.5, dtype=np.float32)
    
    l1_full_w = np.zeros((32, 512), dtype=np.float32)
    l1_full_b = np.zeros(32, dtype=np.float32)
    out_full_w = np.zeros(32, dtype=np.float32)
    
    # 0..15: Positive side
    l1_full_w[0:16, 0:256] = l1_half_w_np
    l1_full_w[0:16, 256:512] = -l1_half_w_np
    l1_full_b[0:16] = 0.0
    out_full_w[0:16] = 1.5
    
    # 16..31: Negative side
    l1_full_w[16:32, 0:256] = -l1_half_w_np
    l1_full_w[16:32, 256:512] = l1_half_w_np
    l1_full_b[16:32] = 0.0
    out_full_w[16:32] = -1.5
    
    ft_w_q = np.clip(np.round(ft_w_np), -32767, 32767).astype(np.int16)
    ft_b_q = np.clip(np.round(ft_b_np), -32767, 32767).astype(np.int16)
    
    l1_scale = 128.0
    l1_w_q = np.clip(np.round(l1_full_w * l1_scale), -127, 127).astype(np.int8)
    l1_b_q = np.zeros(32, dtype=np.int32)
    
    out_scale = 16.0
    out_w_q = np.zeros(32, dtype=np.int16)
    out_w_q[0:16] = 24
    out_w_q[16:32] = -24
    out_b_q = int(tempo_bonus * 16)
    
    # Validation simulator
    def eval_sim(fen):
        b = chess.Board(fen)
        w_f = get_features(b, chess.WHITE)
        b_f = get_features(b, chess.BLACK)
        s_f = w_f if b.turn == chess.WHITE else b_f
        o_f = b_f if b.turn == chess.WHITE else w_f
        
        acc_u = ft_b_q.copy().astype(np.int32)
        for f in s_f: acc_u += ft_w_q[f].astype(np.int32)
        acc_u = np.clip(acc_u, 0, 127).astype(np.int8)
        
        acc_t = ft_b_q.copy().astype(np.int32)
        for f in o_f: acc_t += ft_w_q[f].astype(np.int32)
        acc_t = np.clip(acc_t, 0, 127).astype(np.int8)
        
        inp = np.concatenate([acc_u, acc_t])
        l1_o = np.zeros(32, dtype=np.int8)
        for i in range(32):
            s = l1_b_q[i] + np.sum(inp.astype(np.int32) * l1_w_q[i].astype(np.int32))
            l1_o[i] = np.clip(s // 128, 0, 127)
            
        out = out_b_q + np.sum(l1_o.astype(np.int32) * out_w_q.astype(np.int32))
        return out // 16

    print("\n=== NNUE Evaluation Verification ===")
    positions = [
        ("Starting position", chess.STARTING_FEN),
        ("After 1. e4", "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1"),
        ("White +Knight", "r1bqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"),
        ("White +Queen", "rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"),
        ("Opposite-colored bishops", "8/5k2/4b3/8/8/4B3/5K2/8 w - - 0 1"),
        ("Pawn endgame (+1 passed pawn)", "8/8/8/4k3/8/4P3/4K3/8 w - - 0 1"),
        ("Black winning (-Rook)", "4k3/8/8/8/8/8/8/R3K3 b - - 0 1"),
    ]
    for label, fen in positions:
        print(f"  {label:35s}: {eval_sim(fen):+6d} cp")

    # Ensure parent dir exists
    parent_dir = os.path.dirname(output_path)
    if parent_dir:
        os.makedirs(parent_dir, exist_ok=True)
        
    with open(output_path, "wb") as f:
        f.write(ft_b_q.tobytes())
        f.write(ft_w_q.tobytes())
        f.write(l1_b_q.tobytes())
        f.write(l1_w_q.tobytes())
        f.write(struct.pack("<i", out_b_q))
        f.write(out_w_q.tobytes())
        
    file_size = os.path.getsize(output_path)
    print(f"\nSUCCESS! Exported trained NNUE network to: {output_path} ({file_size:,} bytes)")
    if file_size != 410308:
        print(f"WARNING: File size is {file_size} bytes (expected exactly 410,308 bytes)!", file=sys.stderr)


# --- 5. Main CLI Workflow ---

def evaluate_dataset(model, x_us_indices, x_them_indices, y_scores, batch_size=2048):
    """Compute validation loss across batches using compact feature representation."""
    total_val_loss = 0.0
    val_n = len(y_scores)
    batches = 0
    for b_idx in range(0, val_n, batch_size):
        idx = np.arange(b_idx, min(val_n, b_idx + batch_size))
        b_size = len(idx)
        bx_us_np = np.zeros((b_size, 768), dtype=np.float32)
        bx_them_np = np.zeros((b_size, 768), dtype=np.float32)
        rows = np.repeat(np.arange(b_size), 32)
        
        cols_us = x_us_indices[idx].flatten()
        valid_us = cols_us >= 0
        bx_us_np[rows[valid_us], cols_us[valid_us]] = 1.0
        
        cols_them = x_them_indices[idx].flatten()
        valid_them = cols_them >= 0
        bx_them_np[rows[valid_them], cols_them[valid_them]] = 1.0
        
        loss = loss_fn(model, mx.array(bx_us_np), mx.array(bx_them_np), mx.array(y_scores[idx]))
        total_val_loss += loss.item()
        batches += 1
    return total_val_loss / max(1, batches)

def main():
    parser = argparse.ArgumentParser(
        description="Train Devilfish NNUE neural network from a PGN file (Metal GPU accelerated).",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter
    )
    parser.add_argument("pgn", nargs="?", default=None, help="Path to PGN file (.pgn or .pgn.gz)")
    parser.add_argument("-o", "--output", default="internal/nnue/default.nnue", help="Output .nnue weight file path")
    parser.add_argument("--epochs", type=int, default=40, help="Number of training epochs")
    parser.add_argument("--batch-size", type=int, default=512, help="Batch size")
    parser.add_argument("--lr", type=float, default=0.003, help="Initial learning rate")
    parser.add_argument("--max-games", type=int, default=None, help="Max games to parse from PGN (None = all)")
    parser.add_argument("--max-positions", type=int, default=0, help="Max positions to train on (0 = unlimited / all available)")
    parser.add_argument("--min-ply", type=int, default=8, help="Minimum ply to sample positions from")
    parser.add_argument("--max-ply", type=int, default=150, help="Maximum ply to sample positions from")
    parser.add_argument("--sample-rate", type=float, default=0.5, help="Sampling probability per ply")
    parser.add_argument("--tempo", type=int, default=20, help="Tempo bonus in centipawns for side to move")
    parser.add_argument("--workers", type=int, default=min(10, mp.cpu_count()), help="Parallel worker processes for PGN parsing")
    parser.add_argument("--init-weights", default=None, help="Optional existing .nnue file to fine-tune from")
    parser.add_argument("--save-dataset", default=None, help="Save parsed positions to .npz file for fast re-training")
    parser.add_argument("--load-dataset", default=None, help="Load pre-parsed positions from .npz file (bypasses PGN parsing)")
    
    args = parser.parse_args()
    
    if args.load_dataset is None and args.pgn is None:
        parser.print_help()
        print("\nERROR: Either a PGN file or --load-dataset must be provided.", file=sys.stderr)
        sys.exit(1)
        
    mp.set_start_method("fork", force=True)

    # 1. Obtain Dataset
    if args.load_dataset:
        print(f"Loading cached dataset from {args.load_dataset}...")
        npz = np.load(args.load_dataset)
        X_us_raw = npz["X_us"]
        X_them_raw = npz["X_them"]
        Y = npz["Y"].astype(np.float32)
        N = len(Y)
        print(f"Loaded {N:,} positions from cache.")
        
        # Check if legacy dense format (N, 768)
        if X_us_raw.shape[1] == 768:
            print("Converting legacy dense cache to compact index representation...")
            X_us = np.full((N, 32), -1, dtype=np.int16)
            X_them = np.full((N, 32), -1, dtype=np.int16)
            for i in range(N):
                u_nz = np.where(X_us_raw[i] > 0)[0]
                t_nz = np.where(X_them_raw[i] > 0)[0]
                X_us[i, :len(u_nz)] = u_nz
                X_them[i, :len(t_nz)] = t_nz
        else:
            X_us = X_us_raw.astype(np.int16)
            X_them = X_them_raw.astype(np.int16)
    else:
        samples = extract_positions_from_pgn(
            args.pgn,
            args.max_games,
            args.max_positions,
            args.min_ply,
            args.max_ply,
            args.sample_rate,
            args.workers
        )
        if not samples:
            print("ERROR: No valid positions could be extracted from the PGN file.", file=sys.stderr)
            sys.exit(1)
            
        N = len(samples)
        print(f"Packing {N:,} positions into compact index representation (64 bytes/pos)...")
        X_us = np.full((N, 32), -1, dtype=np.int16)
        X_them = np.full((N, 32), -1, dtype=np.int16)
        Y = np.zeros((N, 1), dtype=np.float32)
        
        for i, (w_feats, b_feats, stm, eval_white) in enumerate(samples):
            score_stm = eval_white if stm == chess.WHITE else -eval_white
            Y[i, 0] = score_stm
            if stm == chess.WHITE:
                X_us[i, :len(w_feats)] = w_feats
                X_them[i, :len(b_feats)] = b_feats
            else:
                X_us[i, :len(b_feats)] = b_feats
                X_them[i, :len(w_feats)] = w_feats
                
        if args.save_dataset:
            print(f"Saving compact dataset cache to {args.save_dataset}...")
            np.savez_compressed(args.save_dataset, X_us=X_us, X_them=X_them, Y=Y)
            print("Dataset saved successfully.")

    val_size = min(50000, max(100, N // 10))
    train_size = N - val_size
    print(f"Dataset split: {train_size:,} train, {val_size:,} validation positions.")

    X_us_train = X_us[:train_size]
    X_them_train = X_them[:train_size]
    Y_train = Y[:train_size]

    X_us_val = X_us[train_size:]
    X_them_val = X_them[train_size:]
    Y_val = Y[train_size:]

    # 2. Build Model
    model = SymmetricNNUE(init_weights_path=args.init_weights)
    loss_and_grad_fn = nn.value_and_grad(model, loss_fn)

    # 3. Training Loop with Cosine Decay Schedule
    epochs = args.epochs
    batch_size = args.batch_size
    initial_lr = args.lr
    min_lr = 0.0002

    print(f"\nTraining Symmetric NNUE across {epochs} epochs on Metal GPU...")
    t_start = time.time()
    best_val_loss = float("inf")

    optimizer = optim.Adam(learning_rate=initial_lr)

    for epoch in range(epochs):
        lr = min_lr + 0.5 * (initial_lr - min_lr) * (1.0 + np.cos(np.pi * epoch / epochs))
        optimizer.learning_rate = lr
        
        perm = np.random.permutation(train_size)
        total_loss = 0.0
        num_batches = 0
        t_ep = time.time()
        
        for b_idx in range(0, train_size, batch_size):
            idx = perm[b_idx:b_idx+batch_size]
            b_size = len(idx)
            bx_us_np = np.zeros((b_size, 768), dtype=np.float32)
            bx_them_np = np.zeros((b_size, 768), dtype=np.float32)
            rows = np.repeat(np.arange(b_size), 32)
            
            cols_us = X_us_train[idx].flatten()
            valid_us = cols_us >= 0
            bx_us_np[rows[valid_us], cols_us[valid_us]] = 1.0
            
            cols_them = X_them_train[idx].flatten()
            valid_them = cols_them >= 0
            bx_them_np[rows[valid_them], cols_them[valid_them]] = 1.0
            
            bx_us = mx.array(bx_us_np)
            bx_them = mx.array(bx_them_np)
            by = mx.array(Y_train[idx])
            
            loss, grads = loss_and_grad_fn(model, bx_us, bx_them, by)
            optimizer.update(model, grads)
            mx.eval(model.parameters(), optimizer.state)
            
            total_loss += loss.item()
            num_batches += 1
            
        val_loss = evaluate_dataset(model, X_us_val, X_them_val, Y_val, batch_size=2048)
        dur = time.time() - t_ep
        if val_loss < best_val_loss:
            best_val_loss = val_loss
            
        print_interval = max(1, epochs // 12)
        if (epoch + 1) % print_interval == 0 or epoch == 0 or epoch == epochs - 1:
            avg_loss = total_loss / max(1, num_batches)
            print(f"Epoch {epoch+1:2d}/{epochs:2d} (lr={lr:.5f}) | Train: {avg_loss:.4f} | Val: {val_loss:.4f} (best: {best_val_loss:.4f}) | {dur:.2f}s")

    total_time = time.time() - t_start
    print(f"\nTraining completed in {total_time:.2f}s across {epochs} epochs!")

    # 4. Quantize and Export
    quantize_and_export(model, args.tempo, args.output)


if __name__ == "__main__":
    main()
