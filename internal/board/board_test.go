package board

import (
	"slices"
	"testing"
)

// ---------- ヘルパ ----------

// mustParseSquare はテスト用ヘルパ。t.Helper を呼ぶことで
// このヘルパ内で t.Fatal が起きてもエラー位置として
// "呼び出し元のテスト関数の行" を表示してくれる。
func mustParseSquare(t *testing.T, s string) Square {
	t.Helper()
	sq, err := ParseSquare(s)
	if err != nil {
		t.Fatalf("ParseSquare(%q): %v", s, err)
	}
	return sq
}

// ---------- New ----------

func TestNew(t *testing.T) {
	b := New()

	if b.Player() != Black {
		t.Errorf("initial player = %d, want Black", b.Player())
	}

	// 中央 4 マスの確認
	want := map[Square]Cell{
		mustParseSquare(t, "d4"): CellWhite,
		mustParseSquare(t, "e5"): CellWhite,
		mustParseSquare(t, "d5"): CellBlack,
		mustParseSquare(t, "e4"): CellBlack,
	}
	for sq, c := range want {
		if b.cells[sq] != c {
			t.Errorf("cell[%s] = %d, want %d", sq, b.cells[sq], c)
		}
	}

	// それ以外は空であること
	for sq := Square(0); sq < 64; sq++ {
		if _, ok := want[sq]; ok {
			continue
		}
		if b.cells[sq] != CellEmpty {
			t.Errorf("cell[%s] = %d, want CellEmpty", sq, b.cells[sq])
		}
	}
}

func TestInitialCounts(t *testing.T) {
	b := New()
	black, white, empty := b.DiscCounts()
	if black != 2 || white != 2 || empty != 60 {
		t.Fatalf("DiscCounts = black %d white %d empty %d, want 2/2/60", black, white, empty)
	}
	if got := b.MoveCount(); got != 0 {
		t.Fatalf("MoveCount = %d, want 0", got)
	}
}

// ---------- Square ↔ string ----------

// テーブル駆動テストの典型形。サブテストにすると失敗時に
// どのケースで落ちたか名前で分かる。
func TestSquareRoundTrip(t *testing.T) {
	cases := []struct {
		s    string
		x, y int
	}{
		{"a1", 0, 0},
		{"h1", 7, 0},
		{"a8", 0, 7},
		{"h8", 7, 7},
		{"d4", 3, 3},
		{"f5", 5, 4},
	}
	for _, c := range cases {
		t.Run(c.s, func(t *testing.T) {
			sq, err := ParseSquare(c.s)
			if err != nil {
				t.Fatalf("ParseSquare: %v", err)
			}
			if sq != SquareXY(c.x, c.y) {
				t.Errorf("ParseSquare(%q) = %d, want %d", c.s, sq, SquareXY(c.x, c.y))
			}
			if got := sq.String(); got != c.s {
				t.Errorf("Square(%d).String() = %q, want %q", sq, got, c.s)
			}
		})
	}
}

func TestParseSquareErrors(t *testing.T) {
	cases := []string{"", "a", "a9", "i1", "11", "aa", "f5x", "pa"}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseSquare(s); err == nil {
				t.Errorf("ParseSquare(%q) should fail", s)
			}
		})
	}
}

// ---------- Move ↔ string (TODO) ----------

func TestMoveRoundTrip(t *testing.T) {
	cases := []struct {
		s    string
		want Move
	}{
		{"f5", Move(mustParseSquare(t, "f5"))},
		{"a1", Move(mustParseSquare(t, "a1"))},
		{"h8", Move(mustParseSquare(t, "h8"))},
		{"pa", PassMove},
	}
	for _, c := range cases {
		t.Run(c.s, func(t *testing.T) {
			got, err := ParseMove(c.s)
			if err != nil {
				t.Fatalf("ParseMove(%q): %v", c.s, err)
			}
			if got != c.want {
				t.Errorf("ParseMove(%q) = %d, want %d", c.s, got, c.want)
			}
			if s := got.String(); s != c.s {
				t.Errorf("Move(%d).String() = %q, want %q", got, s, c.s)
			}
		})
	}
}

// "ps" / "pass" は ParseMove では受け付けるが、String では正規化された "pa" になる。
func TestParseMoveAliasesForPass(t *testing.T) {
	for _, s := range []string{"pa", "ps", "pass"} {
		t.Run(s, func(t *testing.T) {
			m, err := ParseMove(s)
			if err != nil {
				t.Fatalf("ParseMove(%q): %v", s, err)
			}
			if m != PassMove {
				t.Errorf("ParseMove(%q) = %d, want PassMove", s, m)
			}
		})
	}
}

func TestParseMoveErrors(t *testing.T) {
	for _, s := range []string{"", "a", "i1", "f5x", "PA"} {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseMove(s); err == nil {
				t.Errorf("ParseMove(%q) should fail", s)
			}
		})
	}
}

// ---------- Move.Square() の panic ----------

// recover を使った panic テストの基本形。
// defer で recover し、panic が起きなかったら t.Error を呼ぶ。
func TestMoveSquarePanicsOnPass(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	_ = PassMove.Square()  // ここで panic するはず
	t.Error("unreachable") // 到達したら panic していない
}

func TestMoveAsSquare(t *testing.T) {
	sq, err := ParseSquare("f5")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Move(sq).AsSquare()
	if !ok || got != sq {
		t.Fatalf("AsSquare = %v, %v; want %v, true", got, ok, sq)
	}
	if _, ok := PassMove.AsSquare(); ok {
		t.Fatal("PassMove.AsSquare ok = true, want false")
	}
}

// ---------- LegalSquares ----------

func TestLegalSquaresInitial(t *testing.T) {
	b := New()
	got := b.LegalSquares()

	// 初期局面の黒の合法手は c4, d3, e6, f5 の 4 つ。
	want := []Square{
		mustParseSquare(t, "c4"),
		mustParseSquare(t, "d3"),
		mustParseSquare(t, "e6"),
		mustParseSquare(t, "f5"),
	}

	if !slices.Equal(got, want) {
		t.Errorf("LegalSquares = %v, want %v", squaresToStrings(got), squaresToStrings(want))
	}
}

func squaresToStrings(sqs []Square) []string {
	out := make([]string, len(sqs))
	for i, s := range sqs {
		out[i] = s.String()
	}
	return out
}

// ---------- Apply ----------

func TestApplyValidF5(t *testing.T) {
	b := New()
	f5 := mustParseSquare(t, "f5")

	if err := b.Apply(Move(f5)); err != nil {
		t.Fatalf("Apply(f5): %v", err)
	}

	// f5 に黒石、e5 が黒に反転、手番は白
	if b.cells[f5] != CellBlack {
		t.Errorf("cell[f5] = %d, want CellBlack", b.cells[f5])
	}
	e5 := mustParseSquare(t, "e5")
	if b.cells[e5] != CellBlack {
		t.Errorf("cell[e5] = %d, want CellBlack (flipped)", b.cells[e5])
	}
	if b.Player() != White {
		t.Errorf("player after f5 = %d, want White", b.Player())
	}
	if !slices.Equal(b.history, []Square{f5}) {
		t.Errorf("history = %v, want [f5]", b.history)
	}
	if got := b.MoveCount(); got != 1 {
		t.Errorf("MoveCount = %d, want 1", got)
	}
}

// ---------- TODO(you) ----------

// TODO(you): 不正な手を打ったら error が返り、盤面が変更されないことを確認する。
//
// 観点:
//   - 例えば a1 (= sq 0) は初期盤面で illegal。
//   - Apply が error を返すこと。
//   - cells が初期状態のまま (a1 が CellEmpty)。
//   - history が空のまま。
//   - 手番が Black のまま。
func TestApplyIllegalDoesNotMutate(t *testing.T) {
	b1, b2 := New(), New()
	if b1.Apply(0) == nil {
		t.Error("Apply(illegal move) should fail")
	}
	// 状態は変わっていないこと
	if b1.Player() != b2.Player() {
		t.Errorf("player after illegal pass = %d, want Black", b1.Player())
	}
	if b1.cells != b2.cells {
		t.Errorf("cells after illegal pass = %v, want %v", b1.cells, b2.cells)
	}
	if !slices.Equal(b1.history, b2.history) {
		t.Errorf("history after illegal pass = %v, want nil", b1.history)
	}
}

// ---------- Pass ----------

func TestPassOnInitialBoardErrors(t *testing.T) {
	b := New()
	if err := b.Apply(PassMove); err == nil {
		t.Error("Apply(PassMove) on initial board should fail")
	}
	// 状態は変わっていないこと
	if b.Player() != Black {
		t.Errorf("player after illegal pass = %d, want Black", b.Player())
	}
}

// ---------- GameText (TODO) ----------

// TODO(you): GameText が history を連結した文字列を返すことを確認する。
//
// 観点:
//   - 初期盤面では "" (空) が返る。
//   - f5 を打った後は "f5" が返る。
//   - f5, d6 と続けて打った後は "f5d6" が返る (両方合法)。
//
// ヒント: d6 は f5 の後で白の合法手に含まれる。
func TestGameText(t *testing.T) {
	b := New()
	if gt := b.GameText(); gt != "" {
		t.Errorf("GameText on initial board = %s, want \"\"", gt)
	}
	f5, _ := ParseSquare("f5")
	if err := b.Apply(Move(f5)); err != nil {
		t.Errorf("Apply(f5) should not fail: %v", err)
	}
	if gt := b.GameText(); gt != "f5" {
		t.Errorf("GameText after f5 = %s, want \"f5\"", gt)
	}

	d6, _ := ParseSquare("d6")
	if err := b.Apply(Move(d6)); err != nil {
		t.Errorf("Apply(d6) should not fail: %v", err)
	}
	if gt := b.GameText(); gt != "f5d6" {
		t.Errorf("GameText after f5, d6 = %s, want \"f5d6\"", gt)
	}
}

func TestReplayGameTextRoundTripWithImplicitPasses(t *testing.T) {
	b := New()
	for {
		status, legal := b.Status()
		switch status {
		case StatusGameOver:
			replayed, err := ReplayGameText(b.GameText())
			if err != nil {
				t.Fatalf("ReplayGameText: %v", err)
			}
			if replayed.cells != b.cells || replayed.player != b.player || replayed.GameText() != b.GameText() {
				t.Fatalf("replayed board differs from original")
			}
			return
		case StatusPass:
			if err := b.Apply(PassMove); err != nil {
				t.Fatalf("Apply(pass): %v", err)
			}
		case StatusPlay:
			if err := b.Apply(Move(legal[0])); err != nil {
				t.Fatalf("Apply(%s): %v", legal[0], err)
			}
		}
	}
}

func TestCanonicalHashMatchesSymmetricPosition(t *testing.T) {
	a := New()
	if err := a.Apply(Move(mustParseSquare(t, "f5"))); err != nil {
		t.Fatal(err)
	}
	b := New()
	if err := b.Apply(Move(mustParseSquare(t, "d3"))); err != nil {
		t.Fatal(err)
	}
	if a.CanonicalHash() != b.CanonicalHash() {
		t.Fatalf("CanonicalHash mismatch for symmetric positions")
	}
}

func TestCanonicalHashMatchesSideToMoveBitboards(t *testing.T) {
	a := New()
	if err := a.Apply(Move(mustParseSquare(t, "f5"))); err != nil {
		t.Fatal(err)
	}
	own, opponent := a.BitboardsSideToMove()
	if got, want := a.CanonicalHash(), CanonicalHashBits(own, opponent); got != want {
		t.Fatalf("CanonicalHash = %s, want %s", got, want)
	}
}

func TestEncodeSideToMovePerspective(t *testing.T) {
	b := New()
	b.player = Black
	blackView := b.EncodeSideToMove()
	b.player = White
	whiteView := b.EncodeSideToMove()
	for i := range blackView {
		if blackView[i] != -whiteView[i] {
			t.Fatalf("cell %d: black view %d, white view %d", i, blackView[i], whiteView[i])
		}
	}
}
