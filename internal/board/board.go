// Package board implements an 8x8 Othello board.
package board

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ---------- Player ----------

// Player は手番。Black か White のいずれか。
type Player int8

const (
	Black Player = 1
	White Player = -1
)

func (p Player) Opponent() Player { return -p }
func (p Player) Cell() Cell       { return Cell(p) }

// ---------- Cell ----------

// Cell は盤面 1 マスの状態。
type Cell int8

const (
	CellEmpty Cell = 0
	CellBlack Cell = 1  // = Cell(Black)
	CellWhite Cell = -1 // = Cell(White)
)

// ---------- Status ----------

type Status int

const (
	StatusPlay     Status = iota // 合法手あり
	StatusPass                   // 自分は無し、相手はある
	StatusGameOver               // 両者無し
)

// ---------- Square ----------

// Square は盤上の 1 マス。0..63 のみ。Pass は表現できない。
type Square int8

// SquareXY は (x, y) を Square に変換する。範囲外は panic。
func SquareXY(x, y int) Square {
	if x < 0 || x >= 8 || y < 0 || y >= 8 {
		panic(fmt.Sprintf("SquareXY: (%d, %d) is out of bounds", x, y))
	}
	return Square(y*8 + x)
}

// String は "f5" 形式を返す (Stringer interface)。
func (s Square) String() string {
	return fmt.Sprintf("%c%d", 'a'+(int(s)&7), (int(s)>>3)+1)
}

// ParseSquare は "f5" 形式の文字列を Square に変換する。Pass は受け付けない。
func ParseSquare(s string) (Square, error) {
	if len(s) != 2 {
		return 0, fmt.Errorf("invalid square %q: must be 2 chars", s)
	}
	x := int(s[0] - 'a')
	y := int(s[1] - '1')
	if x < 0 || x >= 8 || y < 0 || y >= 8 {
		return 0, fmt.Errorf("invalid square %q: out of bounds", s)
	}
	return SquareXY(x, y), nil
}

// ---------- Move ----------

// Move は 1 アクション。0..63 はマスへの着手、PassMove はパス。
type Move int8

// PassMove はパスを表す Move 値。
const PassMove Move = -1

// IsPass はこの Move がパスか。
func (m Move) IsPass() bool { return m == PassMove }

// Square は通常手としての Move のマスを返す。
// Pass のときに呼ぶのはバグなので panic する。
func (m Move) Square() Square {
	if m == PassMove {
		panic("Move.Square called on PassMove")
	}
	return Square(m)
}

// AsSquare は通常手としてのマスと、通常手かどうかを返す。
func (m Move) AsSquare() (Square, bool) {
	if m == PassMove {
		return 0, false
	}
	return Square(m), true
}

// String は Move を "f5" / "pa" 形式で返す。
func (m Move) String() string {
	if m == PassMove {
		return "pa"
	}
	return Square(m).String()
}

// ParseMove は "f5" / "pa" / "ps" / "pass" を Move に変換する。
func ParseMove(s string) (Move, error) {
	switch s {
	case "pa", "ps", "pass":
		return PassMove, nil
	}
	sq, err := ParseSquare(s)
	if err != nil {
		return 0, err
	}
	return Move(sq), nil
}

// ---------- 方向 ----------

// 8 方向の Square 差分 (= 8*dy + dx)。
var directions = [8]int{+1, +9, +8, +7, -1, -9, -8, -7}

// ---------- Board ----------

// Board は盤面、手番、履歴を保持する。
type Board struct {
	cells   [64]Cell
	player  Player
	history []Square // 実際に石を置いたマス。Pass は含まない。
}

// New は初期局面を返す (黒番、中央 4 マスに初期石)。
func New() Board {
	var b Board
	b.cells[SquareXY(3, 3)] = CellWhite
	b.cells[SquareXY(4, 4)] = CellWhite
	b.cells[SquareXY(3, 4)] = CellBlack
	b.cells[SquareXY(4, 3)] = CellBlack
	b.player = Black
	return b
}

func (b *Board) Player() Player { return b.player }

// Cells returns a copy of the board cells in a1..h8 square order.
func (b *Board) Cells() [64]Cell { return b.cells }

// DiscCounts returns the number of black, white, and empty squares.
func (b *Board) DiscCounts() (black, white, empty int) {
	for _, c := range b.cells {
		switch c {
		case CellBlack:
			black++
		case CellWhite:
			white++
		default:
			empty++
		}
	}
	return black, white, empty
}

// MoveCount returns the number of non-pass moves played.
func (b *Board) MoveCount() int { return len(b.history) }

// flipsFor は手番 p が sq に打つときにひっくり返るマス一覧を返す。不正な手なら nil。
func (b *Board) flipsFor(sq Square, p Player) []Square {
	if b.cells[sq] != CellEmpty {
		return nil
	}
	pCell := p.Cell()
	oCell := p.Opponent().Cell()

	var flips []Square
	for _, offset := range directions {
		cur := int(sq)
		var path []Square
		for {
			next := cur + offset
			fd := (next & 7) - (cur & 7)
			// uint(next) < 64 で負数と 64 以上をまとめて弾く。
			// fd は king move なので |fd| <= 1。盤端を回り込むと |fd| = 7 になる。
			if uint(next) >= 64 || fd < -1 || fd > 1 {
				break
			}
			cur = next
			cell := b.cells[cur]
			if cell == oCell {
				path = append(path, Square(cur))
				continue
			}
			if cell == pCell {
				flips = append(flips, path...)
			}
			break
		}
	}
	return flips
}

// LegalSquaresFor は指定手番の合法な着手マス一覧を返す。
// 戻り値は file 優先のループ順 = 文字列順 (a1, a2, ..., h8) で返るため sort 不要。
func (b *Board) LegalSquaresFor(p Player) []Square {
	var moves []Square
	for file := 0; file < 8; file++ {
		for rank := 0; rank < 8; rank++ {
			sq := Square(rank*8 + file)
			if b.flipsFor(sq, p) != nil {
				moves = append(moves, sq)
			}
		}
	}
	return moves
}

// LegalSquares は手番側の合法な着手マス一覧を返す。
func (b *Board) LegalSquares() []Square { return b.LegalSquaresFor(b.player) }

// Status は手番側の状況と (StatusPlay のときのみ) 合法手を返す。
// 内部で LegalSquaresFor を最大 2 回しか呼ばない。
func (b *Board) Status() (Status, []Square) {
	self := b.LegalSquares()
	if len(self) > 0 {
		return StatusPlay, self
	}
	if len(b.LegalSquaresFor(b.player.Opponent())) > 0 {
		return StatusPass, nil
	}
	return StatusGameOver, nil
}

// MustPass / IsGameOver は Status の薄いラッパ。
func (b *Board) MustPass() bool {
	s, _ := b.Status()
	return s == StatusPass
}
func (b *Board) IsGameOver() bool {
	s, _ := b.Status()
	return s == StatusGameOver
}

// Apply は Move を盤面に適用する。
//
// Pass の場合は手番のみ反転 (合法でない Pass なら error)。
// 通常手の場合は石を置き、ひっくり返してから手番を反転する。
// 不正な手なら error を返し、盤面は変更しない。
func (b *Board) Apply(m Move) error {
	if m.IsPass() {
		if !b.MustPass() {
			return fmt.Errorf("pass is not legal in this position")
		}
		b.player = b.player.Opponent()
		return nil
	}
	sq := m.Square()
	flips := b.flipsFor(sq, b.player)
	if flips == nil {
		return fmt.Errorf("illegal move: %s", m)
	}
	stone := b.player.Cell()
	b.cells[sq] = stone
	for _, f := range flips {
		b.cells[f] = stone
	}
	b.history = append(b.history, sq)
	b.player = b.player.Opponent()
	return nil
}

// GameText は履歴を連結した棋譜文字列を返す。例: "f5d6c3..."
// Pass は盤面状態から決定的に復元できるため含めない。
func (b *Board) GameText() string {
	var sb strings.Builder
	sb.Grow(len(b.history) * 2)
	for _, s := range b.history {
		sb.WriteString(s.String())
	}
	return sb.String()
}

// ReplayGameText replays a pass-free game text from the initial position.
// Passes are reconstructed from local board status before each non-pass move.
func ReplayGameText(moves string) (Board, error) {
	if len(moves)%2 != 0 {
		return Board{}, fmt.Errorf("game text length must be even: %d", len(moves))
	}
	b := New()
	for i := 0; i < len(moves); i += 2 {
		for {
			status, _ := b.Status()
			if status != StatusPass {
				break
			}
			if err := b.Apply(PassMove); err != nil {
				return Board{}, fmt.Errorf("implicit pass before %q: %w", moves[i:i+2], err)
			}
		}
		m, err := ParseMove(moves[i : i+2])
		if err != nil {
			return Board{}, fmt.Errorf("parse move %d: %w", i/2, err)
		}
		if m.IsPass() {
			return Board{}, fmt.Errorf("game text must not contain pass token at move %d", i/2)
		}
		if err := b.Apply(m); err != nil {
			return Board{}, fmt.Errorf("apply move %d %s: %w", i/2, m, err)
		}
	}
	for {
		status, _ := b.Status()
		if status != StatusPass {
			break
		}
		if err := b.Apply(PassMove); err != nil {
			return Board{}, fmt.Errorf("trailing implicit pass: %w", err)
		}
	}
	return b, nil
}

// CanonicalHash returns a SHA-256 hash of the canonical side-to-move
// perspective bitboard pair among the eight square symmetries.
func (b *Board) CanonicalHash() string {
	own, opponent := b.BitboardsSideToMove()
	return CanonicalHashBits(own, opponent)
}

// EncodeSideToMove returns 64 signed bytes in a1..h8 order from the side-to-move
// perspective: own discs are +1, opponent discs are -1, empty cells are 0.
func (b *Board) EncodeSideToMove() [64]int8 {
	var out [64]int8
	for i, c := range b.cells {
		out[i] = int8(c) * int8(b.player)
	}
	return out
}

// BitboardsSideToMove returns own and opponent bitboards in side-to-move
// perspective. Bit 0 is a1, bit 63 is h8.
func (b *Board) BitboardsSideToMove() (own, opponent uint64) {
	for i, c := range b.cells {
		if c == CellEmpty {
			continue
		}
		bit := uint64(1) << uint(i)
		if c == b.player.Cell() {
			own |= bit
		} else {
			opponent |= bit
		}
	}
	return own, opponent
}

// CanonicalHashBits returns a SHA-256 hash of an own/opponent bitboard pair
// after canonicalizing the pair over the eight square symmetries.
func CanonicalHashBits(own, opponent uint64) string {
	bestOwn, bestOpponent := transformBits(own, 0), transformBits(opponent, 0)
	for t := 1; t < 8; t++ {
		candidateOwn := transformBits(own, t)
		candidateOpponent := transformBits(opponent, t)
		if compareBitPair(candidateOwn, candidateOpponent, bestOwn, bestOpponent) < 0 {
			bestOwn, bestOpponent = candidateOwn, candidateOpponent
		}
	}
	var payload [16]byte
	for i := 0; i < 8; i++ {
		payload[i] = byte(bestOwn >> uint(i*8))
		payload[i+8] = byte(bestOpponent >> uint(i*8))
	}
	sum := sha256.Sum256(payload[:])
	return hex.EncodeToString(sum[:])
}

func transformBits(in uint64, transform int) uint64 {
	var out uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			from := uint(SquareXY(x, y))
			if in&(uint64(1)<<from) == 0 {
				continue
			}
			tx, ty := transformXY(x, y, transform)
			to := uint(SquareXY(tx, ty))
			out |= uint64(1) << to
		}
	}
	return out
}

func compareBitPair(ownA, opponentA, ownB, opponentB uint64) int {
	if ownA < ownB {
		return -1
	}
	if ownA > ownB {
		return 1
	}
	if opponentA < opponentB {
		return -1
	}
	if opponentA > opponentB {
		return 1
	}
	return 0
}

func transformXY(x, y, transform int) (int, int) {
	switch transform {
	case 0:
		return x, y
	case 1:
		return 7 - y, x
	case 2:
		return 7 - x, 7 - y
	case 3:
		return y, 7 - x
	case 4:
		return 7 - x, y
	case 5:
		return x, 7 - y
	case 6:
		return y, x
	case 7:
		return 7 - y, 7 - x
	default:
		panic("invalid transform")
	}
}
