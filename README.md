# reversi-dataset-gen

Reversi (Othello) の AI 学習用 value dataset を生成する Go 製 CLI ツールです。

[Edax](https://github.com/abulmo/edax-reversi) や [Egaroucid](https://github.com/Nyanyan/Egaroucid) などのオープンソース Othello AI エンジンを使って自己対戦を行い、盤面の評価値 (value) を記録したバイナリファイルを出力します。出力データは Reversi AI の強化学習・教師あり学習に利用できます。

---

## 目次

1. [前提知識：phase とは](#前提知識phase-とは)
2. [必要なもの](#必要なもの)
3. [セットアップ手順](#セットアップ手順)
4. [ビルド](#ビルド)
5. [実行](#実行)
6. [設定ファイルの説明](#設定ファイルの説明)
7. [出力ファイルの説明](#出力ファイルの説明)
8. [進捗表示とログ](#進捗表示とログ)
9. [中断・再開・やり直し](#中断再開やり直し)
10. [開発者向け](#開発者向け)

---

## 前提知識：phase とは

Othello は空盤から始め、石を置くたびに盤面の石数が 1 増えます。初期配置で白黒各 2 個 = 計 4 個あるため、**phase = 盤面の総石数 − 4** と定義します。

| 盤面の総石数 | phase |
|:-----------:|:-----:|
| 4（初期）   | 0     |
| 14          | 10    |
| 34          | 30    |
| 64（終局）  | 60    |

`phases = "10..60"` と指定すると phase 10〜60 の各盤面データを生成します。学習用途によって必要な phase 範囲は異なります。

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
