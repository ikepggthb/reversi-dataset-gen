# reversi-dataset-gen

Reversi (Othello) の AI 学習用 value dataset を生成する Go 製 CLI ツールです。

[Edax](https://github.com/abulmo/edax-reversi) や [Egaroucid](https://github.com/Nyanyan/Egaroucid) などのオープンソース Othello AI エンジンを使って自己対戦を行い、盤面の評価値 (value) を記録したバイナリファイルを出力します。

---

## 前提知識：Othello AI の仕組み

このセクションは「ゲーム AI を作ったことがない」「Othello AI がどう動くか知らない」という方向けです。すでに知っている方は読み飛ばしてください。

### Othello AI はどうやって手を選ぶか

Othello AI の基本構造は **「探索 + 評価関数」** の組み合わせです。

```
現在の盤面
    ↓
「この手を打ったらどうなるか」を数手先まで読む（ゲーム木探索）
    ↓
読み終えた末端の盤面を「評価関数」で数値化する
    ↓
最も評価値が高くなる手を選ぶ
```

**ゲーム木探索**（minimax・alpha-beta 枝刈り）は、あらゆる手の組み合わせを木構造で展開して最善手を探します。しかし終局まで完全に読み切るのは計算量が膨大なため、現実的には数手〜十数手先で打ち切ります。

打ち切った時点の盤面が「有利か不利か」を判定するのが**評価関数**です。

### 評価関数とは

評価関数は「盤面を入力として受け取り、手番側にとっての有利さを表すスコアを返す関数」です。

```
評価関数: 盤面 → スコア（正 = 有利、負 = 不利）
```

スコアが高いほど手番側に有利な局面です。AI はこのスコアを手がかりに最善手を探します。つまり **評価関数の精度が AI の強さを直接決めます**。

機械学習で評価関数を作る場合、「盤面とその評価値のペア」を大量に用意して、盤面を入力したときに正しい評価値が出力されるようにモデルを学習させます。**このツールはその学習データを生成します。**

---

## このツールが必要な背景

### 1 件のデータがどうやって作られるか

まず、1 件の学習データ（盤面と評価値のペア）がどのように作られるかを説明します。

**例: phase 30 の盤面データを 1 件作る場合**

```
[ステップ 1] ゲーム開始
             初期盤面から、ランダムに 30 手打って phase 30 の盤面を作る
             ↓
[ステップ 2] 自己対戦
             その盤面から、AI エンジン（Edax など）が双方の手を打って終局まで進める
             ↓
[ステップ 3] 記録
             終局時のディスク差（手番側 − 相手側）を value として記録する
             → (phase 30 の盤面, value) を 1 件保存
```

**value（評価値）の意味**: 「この盤面から AI が最後まで対局したとき、何枚差で勝ったか（負けたか）」を、手番側から見た数値です。正なら手番側有利、負なら不利です。

AI エンジンには **Edax** や **Egaroucid** を使います。これらは強い Othello AI であり、信頼性の高い対局を大量に自動実行できます。

この作業を大量に繰り返すことで、(盤面, 評価値) のペアが集まります。

### なぜ phase（フェーズ）ごとに分けるのか

Reversi は序盤・中盤・終盤で局面の性質が大きく異なります。phase ごとにデータセットを分けることで、フェーズに特化した評価関数を学習できます。

**phase の定義**: 盤面の総石数 − 4（初期配置の 4 石を除いた手数）

| 盤面の総石数 | phase | 局面の特徴 |
|:-----------:|:-----:|:---------|
| 4（初期）   | 0     | ゲーム開始直後 |
| 14          | 10    | 序盤 |
| 34          | 30    | 中盤 |
| 64（終局）  | 60    | ゲーム終了 |

### なぜステップ 1 はランダム着手なのか（playout）

目標の phase に到達するまでランダムに打つ理由は、**集まる盤面の多様性を確保するため**です。

仮に毎回「強い AI に 30 手打たせてから」データを取ったとすると、AI は毎回似たような最善手を選ぶため、ほぼ同じ序盤展開になります。その結果、phase 30 で集まる盤面がどれも似通ってしまい、偏ったデータになります。

ランダム着手にすると毎回まったく違う展開になるため、多様な phase 30 の盤面を収集できます。

ランダムと AI を混ぜる割合は設定ファイルで調整できます（[`playout.ai_probability`](#configsconfigtoml--データセット生成の設定) を参照）。

### Window モードとは何か

Window モードは、1 回の自己対戦ゲームから複数の phase のデータをまとめて取る仕組みです。

**Window モードなしの場合**

phase 30 のデータを 1 件取るには、phase 30 まで到達して自己対戦を終局まで進めるという 1 ゲームが必要です。phase 31 のデータを 1 件取るには、また別の 1 ゲームが必要です。

```
phase 30 のデータ 1 件: ランダム 30 手 → 終局まで自己対戦 → 記録
phase 31 のデータ 1 件: ランダム 31 手 → 終局まで自己対戦 → 記録
phase 32 のデータ 1 件: ランダム 32 手 → 終局まで自己対戦 → 記録
...（phase ごとに別々のゲームが必要）
```

phase が多いほど必要なゲーム数が増え、生成時間も伸びます。

**Window モードありの場合（`window.size = 10`）**

```
ランダム 30 手 → phase 30 の盤面を保存
              → 自己対戦で 1 手進める → phase 31 の盤面を保存
              → さらに 1 手進める    → phase 32 の盤面を保存
              → ...（10 phase ぶん保存）
              → 終局まで進める
              → 終局のディスク差を、保存した全盤面の value として使う
```

1 回の自己対戦ゲームで phase 30〜39 の 10 件分のデータが得られます。`size = 10` なら必要なゲーム数が約 10 分の 1 になります。

ただし同じゲームから得た複数の盤面はすべて同じ終局を共有しているため、それぞれの盤面が完全に独立したデータではありません。ゲームの展開が偏ると、その影響が複数 phase にまたがって現れることがあります。

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
11. [Acknowledgments](#acknowledgments)

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

生成した `.rd` ファイルは「盤面 → 評価値」を予測する**教師あり学習**に使えます。線形モデル・決定木・ニューラルネットなど、回帰を扱えるあらゆる手法に対応できます。

### データの読み込みと使い方

`.rd` ファイルには `(盤面, value)` のペアが大量に格納されています。盤面を特徴量に変換し、value を予測するよう学習させると、評価関数が得られます。できあがった評価関数は Reversi AI の探索（minimax/alpha-beta など）に組み込んで使います。

### Python で .rd を読み込む例

```python
import struct
import numpy as np

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

records = load_rd("datasets/phase_30/train.rd")
X = np.stack([bitboard_to_features(own, opp) for own, opp, _ in records])
y = np.array([value for _, _, value in records], dtype=np.float32)

# X, y を任意の学習ライブラリに渡す
# 例: sklearn.linear_model.Ridge(alpha=1.0).fit(X, y)
#     sklearn.ensemble.GradientBoostingRegressor().fit(X, y)
#     または PyTorch / Keras でモデルを組む
```

### value の意味

Edax / Egaroucid の `value` はおおむね**ディスク差**（手番側の石数 − 相手の石数）で、範囲は −64〜+64 程度です。

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

## Acknowledgments

このツールは以下のオープンソースプロジェクトを利用しています。

### Edax
- **作者**: Richard Delorme ([@abulmo](https://github.com/abulmo))
- **リポジトリ**: https://github.com/abulmo/edax-reversi
- **ライセンス**: [GPL v3](https://github.com/abulmo/edax-reversi/blob/master/LICENSE)

### Egaroucid
- **作者**: Takuto Yamana ([@Nyanyan](https://github.com/Nyanyan))
- **リポジトリ**: https://github.com/Nyanyan/Egaroucid
- **ライセンス**: [GPL v3](https://github.com/Nyanyan/Egaroucid/blob/master/LICENSE)

---

なお、本ツール（MIT ライセンス）はこれらのエンジンを**外部プロセスとして起動する**だけであり、ソースコードのリンクや組み込みは行っていません。GPL の伝染は発生しませんが、エンジン本体を利用・再配布する場合はそれぞれの GPL v3 ライセンスに従ってください。

---

## License

MIT License. See [LICENSE](LICENSE).
