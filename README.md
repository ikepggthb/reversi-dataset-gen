# reversi-dataset-gen

Reversi (Othello) の AI 学習用 value dataset を生成する Go 製 CLI ツールです。

[Edax](https://github.com/abulmo/edax-reversi) や [Egaroucid](https://github.com/Nyanyan/Egaroucid) などのオープンソース Othello AI エンジンを使って自己対戦を行い、盤面の評価値 (value) を記録したバイナリファイルを出力します。

---

## このツールが必要な背景

### Reversi AI に「評価関数」が必要な理由

Reversi の強い AI を作るには、「この盤面はどちらが有利か」を数値で返す**評価関数**が必要です。探索アルゴリズム（minimax・alpha-beta など）は、評価関数の値をもとに最善手を選びます。

現代の AI では評価関数をニューラルネットワークで表現するアプローチが主流です。ニューラルネットワークを学習させるには大量の「**盤面とその評価値のペア**」が必要で、これが **value dataset** です。このツールはその dataset を生成します。

### なぜ AI エンジンを使うのか

評価値を人間が手作業で付けることはできません。Reversi の局面数は膨大で、正確な評価には深い先読みが必要だからです。

そこで **Edax** や **Egaroucid** のような既存の強い Othello AI エンジンを「先生」として使います。これらはアルファベータ探索などを用いて盤面を高精度に評価できます。このツールはエンジンに対して「この盤面は何点？」と問い合わせ、その答えを dataset に記録します。

### なぜ自己対戦（self-play）で盤面を集めるのか

評価値を付けたい盤面をどうやって集めるかという問題があります。

- **人間の棋譜**：数が少なく偏りがある
- **ランダム生成**：実戦に現れない不自然な配置が多い

そこでエンジンに自己対戦させ、実戦的な局面を大量に生成します。自己対戦で得られたゲーム軌跡から盤面を抽出し、その盤面をエンジンが評価する、というパイプラインです。

### なぜ phase（フェーズ）ごとに分けるのか

Reversi は序盤・中盤・終盤で性質が大きく異なります。石数が少ない序盤は戦略的な選択、終盤は読みきりに近い計算です。フェーズが違えば評価の性質も違うため、dataset を phase ごとに分割することで**フェーズ特化型の評価関数**を学習できます。

**phase の定義**: 盤面の総石数 − 4（初期配置の 4 石を除いた着手数）

| 盤面の総石数 | phase | 局面の特徴 |
|:-----------:|:-----:|:---------|
| 4（初期）   | 0     | ゲーム開始直後 |
| 14          | 10    | 序盤 |
| 34          | 30    | 中盤 |
| 64（終局）  | 60    | ゲーム終了 |

### なぜ phase 到達前の着手はランダムなのか（playout）

目標の phase に到達するまでの着手を **ランダム**にする理由は、**局面の多様性を確保するため**です。

たとえば phase 30 のデータを集めたいとします。phase 30 に到達するには 30 手打つ必要があります。この 30 手を毎回強い AI に打たせると、AI は似たような最善手を選ぶため、**毎回ほぼ同じ序盤展開になり、phase 30 での局面が偏ります**。同じような局面ばかりで学習しても汎化性能が上がりません。

ランダム着手にすると毎回異なる展開になり、phase 30 での局面が多様になります。`ai_probability` を 0 より大きくすると、一定の割合で AI の手も混ぜられます。強い局面も含めたい場合に使いますが、多様性とのトレードオフになります。

```
playout（phase 到達まで）     self_play.ai（評価）
──────────────────────────  →  ──────────────────────────
ランダム or AI で 30 手          Edax lv20 がこの盤面を評価
（多様な局面を生成）             → value を記録
```

### Window モードとは何か

**Window モードなし**だと、各 phase のデータを集めるたびにゲームを最初からやり直します。

```
phase 10 のデータが欲しい → ゲームを開始 → 10 手打つ → 評価 → ゲーム終了
phase 11 のデータが欲しい → ゲームを開始 → 11 手打つ → 評価 → ゲーム終了
phase 12 のデータが欲しい → ゲームを開始 → 12 手打つ → 評価 → ゲーム終了
...
```

phase が多いほどゲーム数が膨大になり非効率です。

**Window モード（`window.enabled = true`）** では、1 回のゲームを最後まで打ちながら、途中の複数の phase で評価を行います。

```
1 回のゲームを開始
  → phase 10 に到達 → 評価して記録
  → phase 11 に到達 → 評価して記録
  → phase 12 に到達 → 評価して記録
  ...（size=10 なら 10 phase ぶん）
  → ゲーム終了
```

`window.size = 10` なら 1 ゲームで 10 phase ぶんのデータを取れるため、**生成速度が大幅に向上**します。ただし同一ゲームから得たデータは局面間に相関があるため、学習データとしての独立性はやや下がります。

### なぜビットボード形式でデータを保存するのか

盤面を 8×8 = 64 マスとみなし、手番側の石を u64 の各ビットで表現するのが**ビットボード**です。

- **コンパクト**：1 盤面 = 16 バイト（手番側 8B + 相手側 8B）
- **高速**：ビット演算で合法手計算・反転処理を高速に行える
- **標準的**：多くの Reversi / Chess エンジン・学習ライブラリで採用されている形式

このツールが出力する `.rd` ファイルは「ビットボード 2 枚 + 評価値 1 個」を 1 レコード（18 バイト）として連続して並べたバイナリファイルです。

---

## 目次

1. [必要なもの](#必要なもの)
2. [セットアップ手順](#セットアップ手順)
3. [ビルド](#ビルド)
4. [実行](#実行)
5. [設定ファイルの説明](#設定ファイルの説明)
6. [出力ファイルの説明](#出力ファイルの説明)
7. [生成したデータを学習に使う](#生成したデータを学習に使う)
8. [進捗表示とログ](#進捗表示とログ)
9. [中断・再開・やり直し](#中断再開やり直し)
10. [開発者向け](#開発者向け)

---

## 必要なもの

| ツール | 用途 | インストール |
|--------|------|------------|
| **Go 1.21 以上** | ビルド | [go.dev/dl](https://go.dev/dl/) |
| **Git** | リポジトリ取得・エンジン clone | OS のパッケージマネージャ |
| **clang / clang++** | Edax・Egaroucid のビルド | `apt install clang` など |
| **curl** | Edax の評価データファイル取得 | 通常はプリインストール済み |

エンジンは **Edax** か **Egaroucid** のどちらか一方があれば動きます（両方でも可）。

---

## セットアップ手順

### 1. リポジトリを clone する

```sh
git clone https://github.com/ikepggthb/reversi-dataset-gen.git
cd reversi-dataset-gen
```

### 2. AI エンジンをセットアップする

エンジンのソースを `third_party/` 以下に clone してビルドします。

```sh
# Edax だけ使う場合
make setup-edax

# Egaroucid だけ使う場合
make setup-egaroucid

# 両方セットアップする場合
make setup
```

> **注意（Egaroucid）**: Egaroucid のビルドには数分かかることがあります。

セットアップが成功すると、以下のパスにバイナリが配置されます。

```text
third_party/
  edax-reversi/
    bin/
      lEdax-x86-64-v3       ← Edax バイナリ
      data/eval.dat          ← Edax 評価データ
  Egaroucid/
    bin/
      Egaroucid_for_Console.out  ← Egaroucid バイナリ
      resources/eval.egev2       ← Egaroucid 評価データ
```

`configs/app.toml` のデフォルトパスはこの配置に合わせてあるため、**通常は app.toml を編集する必要はありません**。

### 3. （任意）app.toml を確認する

エンジンのバイナリを別の場所に置いた場合や、デフォルトとは異なる構成にした場合は `configs/app.toml` を編集します。

```toml
[engines.edax]
kind = "edax"
binary = "../third_party/edax-reversi/bin/lEdax-x86-64-v3"
eval_file = "../third_party/edax-reversi/bin/data/eval.dat"
```

パスは `configs/app.toml` からの相対パスです。

---

## ビルド

```sh
make build
```

または:

```sh
go build -o rdg ./cmd/rdg
```

`rdg` バイナリが生成されます。

---

## 実行

### はじめて実行する場合（推奨）

```sh
./rdg --yes
```

`--yes` を付けると、エンジン起動時の確認プロンプトを自動でスキップします。設定ファイルはデフォルトの `configs/config.toml` と `configs/app.toml` が使われます。

### 設定ファイルを明示的に指定する場合

```sh
./rdg --config configs/config.toml --app-config configs/app.toml --yes
```

### 実行前に確認だけ行う（データは生成しない）

```sh
./rdg --dry-run
```

生成されるデータ量の見積もりが表示されます。本番実行前に確認するのに便利です。

---

## 設定ファイルの説明

### configs/config.toml — データセット生成の設定

```toml
mode = "normal"
output_dir = "../datasets/test2_lv3-12"
phases = "10..60"
samples_per_phase = 1000
```

| キー | 説明 |
|------|------|
| `mode` | `"normal"`（推奨）または `"all_positions"` |
| `output_dir` | 出力先ディレクトリ（`configs/` からの相対パス） |
| `phases` | 生成する phase の範囲。`"10..60"` で phase 10〜60、`"10,20,30"` で個別指定も可 |
| `samples_per_phase` | phase ごとのサンプル数 |

```toml
[playout]
ai_probability = 0
```

| キー | 説明 |
|------|------|
| `ai_probability` | phase 到達前の手を AI で指す確率（0〜1）。`0` だとすべてランダム着手。値を上げると学習済みっぽい局面が増えるが生成が遅くなる |

```toml
[self_play]
workers = 19
```

| キー | 説明 |
|------|------|
| `workers` | 並列生成ワーカー数。CPU コア数に合わせて調整する |

```toml
[self_play.ai]
name = "edax"
level = 20
threads = 1
timeout = "300s"
```

| キー | 説明 |
|------|------|
| `name` | 使うエンジン名。`app.toml` の `[engines.<name>]` と対応 |
| `level` | 探索深さ。Edax の場合 1〜60 程度。高いほど精度が上がるが遅い |
| `threads` | エンジン 1 プロセスあたりのスレッド数 |
| `timeout` | 1 手あたりの制限時間。`"30s"`, `"5m"` などの形式 |

```toml
[window]
enabled = true
size = 10
```

| キー | 説明 |
|------|------|
| `enabled` | window モードの有効化（`true` 推奨）。1 回の自己対戦ゲームから複数 phase のサンプルを同時取得し、生成を高速化する |
| `size` | 1 回の自己対戦ゲームで取得する phase 数の幅 |

```toml
[split]
train = 0.98
valid = 0.01
test  = 0.01
```

各 phase のサンプルを train / valid / test ファイルに分割する比率です。合計が 1 になる必要があります。

---

### configs/app.toml — エンジン登録の設定

エンジンの場所と基本パラメータを登録します。通常は `make setup` 後に変更不要です。

```toml
setup = "missing"   # "missing"（未ビルド時のみ setup）, "always", "off"

[log]
file = "../logs/rdg.log"
max_size_mb = 100
max_files = 5

[engines.edax]
kind = "edax"
binary = "../third_party/edax-reversi/bin/lEdax-x86-64-v3"
eval_file = "../third_party/edax-reversi/bin/data/eval.dat"

[engines.egaroucid]
kind = "egaroucid"
binary = "../third_party/Egaroucid/bin/Egaroucid_for_Console.out"
eval_file = "../third_party/Egaroucid/bin/resources/eval.egev2"
```

---

### all_positions モード

`mode = "all_positions"` は指定した phase の **全盤面**（重複なし）を列挙して評価します。phase が深くなると盤面数が爆発的に増えるため、浅い phase（1〜9 程度）にのみ使用してください。設定例は `configs/config.all_positions.toml` を参照してください。

---

## 出力ファイルの説明

```text
datasets/
  phase_10/
    train.rd        ← 学習データ（バイナリ）
    valid.rd        ← 検証データ（バイナリ）
    test.rd         ← テストデータ（バイナリ）
    hashes.jsonl    ← 重複排除用 canonical hash の一覧
    metadata.json   ← 生成条件・エンジン情報などのメタデータ
    stats.json      ← 評価値の分布統計
  phase_11/
  ...
  summary.json      ← 全 phase の概要
```

### .rd ファイルのバイナリ形式（rdgbitboard_v1）

`.rd` ファイルは独自のバイナリ形式です。

```
header  : "RDGBBVAL1\n"  (10 バイト、固定)
--- 以下を 1 レコード = 18 バイト で繰り返す ---
own_bits      : u64 リトルエンディアン  (手番側の石のビットボード)
opponent_bits : u64 リトルエンディアン  (相手側の石のビットボード)
value         : i16 リトルエンディアン  (評価値。手番側から見た値)
```

- **ビットインデックス**: `a1 = 0`（左上）〜 `h8 = 63`（右下）
- **value**: 手番側 (side-to-move) から見た評価値。Edax の場合はディスク差（−64〜+64 程度）

Python での読み取り例：

```python
import struct

HEADER = b"RDGBBVAL1\n"
RECORD_SIZE = 18

with open("train.rd", "rb") as f:
    header = f.read(len(HEADER))
    assert header == HEADER, "invalid header"
    while chunk := f.read(RECORD_SIZE):
        own, opp, value = struct.unpack_from("<QQh", chunk)
        print(f"own={own:#018x} opp={opp:#018x} value={value}")
```

---

## 進捗表示とログ

実行中はデフォルトで TUI（ターミナル UI）が stderr に表示されます。

```sh
./rdg --no-progress      # TUI なし、1 行テキストログを出力
./rdg --json-progress    # JSONL 形式で progress を出力（パイプ処理向け）
./rdg --log-file logs/rdg.log  # ログをファイルにも書き出す
```

ログローテーションは `configs/app.toml` の `[log]` セクションで設定します（デフォルト: 100 MB × 5 世代）。

---

## 中断・再開・やり直し

```sh
# 途中で Ctrl+C した場合、互換性のある途中出力から続ける
./rdg --resume

# metadata.json が壊れている phase だけ作り直してから続ける
./rdg --resume --repair

# 全 phase を最初からやり直す（既存出力を削除）
./rdg --force
```

---

## 生成したデータを学習に使う

生成した `.rd` ファイルは「盤面 → 評価値」を予測するニューラルネットワークの**教師あり学習**に使えます。

### 学習の全体像

```
.rd ファイル
 (own_bits, opp_bits, value) のペアが大量に入っている
        ↓
  ビットボードを特徴量に変換
  （各マスの石の有無を特徴ベクトルや 2D マップとして表現）
        ↓
  ニューラルネットワークに入力し、value を予測
        ↓
  予測値と正解 value の誤差（MSE など）を最小化するよう学習
        ↓
  学習済み評価関数
  → Reversi AI の探索（minimax/alpha-beta）に組み込む
```

### Python で .rd を読み込んで学習する例

```python
import struct
import numpy as np
import torch
import torch.nn as nn

HEADER = b"RDGBBVAL1\n"

def load_rd(path):
    """(own, opp, value) のタプルリストを返す"""
    records = []
    with open(path, "rb") as f:
        assert f.read(len(HEADER)) == HEADER
        while chunk := f.read(18):
            own, opp, value = struct.unpack_from("<QQh", chunk)
            records.append((own, opp, value))
    return records

def bitboard_to_features(own: int, opp: int) -> np.ndarray:
    """ビットボード 2 枚を 128 次元の特徴ベクトルに変換する"""
    own_bits = np.array([(own >> i) & 1 for i in range(64)], dtype=np.float32)
    opp_bits = np.array([(opp >> i) & 1 for i in range(64)], dtype=np.float32)
    return np.concatenate([own_bits, opp_bits])  # shape: (128,)

# データ読み込み
records = load_rd("datasets/phase_30/train.rd")
X = np.stack([bitboard_to_features(own, opp) for own, opp, _ in records])
y = np.array([value for _, _, value in records], dtype=np.float32)

# PyTorch Dataset
dataset = torch.utils.data.TensorDataset(
    torch.from_numpy(X),
    torch.from_numpy(y).unsqueeze(1),
)
loader = torch.utils.data.DataLoader(dataset, batch_size=256, shuffle=True)

# シンプルな MLP 評価関数
model = nn.Sequential(
    nn.Linear(128, 256), nn.ReLU(),
    nn.Linear(256, 256), nn.ReLU(),
    nn.Linear(256, 1),
)
optimizer = torch.optim.Adam(model.parameters(), lr=1e-3)

# 学習ループ
for epoch in range(10):
    for X_batch, y_batch in loader:
        pred = model(X_batch)
        loss = nn.functional.mse_loss(pred, y_batch)
        optimizer.zero_grad()
        loss.backward()
        optimizer.step()
```

> 上記は動作確認用の最小例です。実用的な評価関数には CNN や残差ネットワーク、対称性を利用したデータ拡張（回転・反転）などを組み合わせることが多いです。

### value の意味と正規化

Edax / Egaroucid の `value` はおおむね**ディスク差**（手番側の石数 − 相手の石数）で、範囲は −64〜+64 程度です。学習時は正規化（÷64 など）すると収束が安定します。

phase ごとに独立した評価関数を学習する場合は、`datasets/phase_XX/train.rd` を phase ごとに別々のモデルで学習します。

---

## 開発者向け

```sh
make test          # 全テストを実行
make vet           # 静的解析
make fmt           # コード整形
```

直接 go コマンドを使う場合：

```sh
go test ./...
go test -race ./...
go vet ./...
```

コントリビューションのルールは [CONTRIBUTING.md](CONTRIBUTING.md) を参照してください。

---

## License

MIT License. See [LICENSE](LICENSE).
