# reversi-dataset-gen

Reversi / Othello の value dataset を生成する Go 製 CLI です。

## 必要なもの

- Go
- Edax または Egaroucid

エンジンのパスは [configs/app.toml](configs/app.toml) に書きます。探索条件や dataset の条件は [configs/config.toml](configs/config.toml) に書きます。

## ビルド

```sh
make build
```

または:

```sh
go build -o rdg ./cmd/rdg
```

## 実行

デフォルト設定で実行:

```sh
./rdg --yes
```

設定ファイルを指定:

```sh
./rdg --config configs/config.toml --app-config configs/app.toml --yes
```

事前確認だけ行う:

```sh
./rdg --dry-run
```

既存出力がある場合:

```sh
./rdg --resume          # 互換性のある途中出力から続行
./rdg --resume --repair # metadata.json が無い壊れた phase だけ作り直して続行
./rdg --force           # 全 phase を作り直し
```

## 設定

主に触るのは [configs/config.toml](configs/config.toml) です。

```toml
mode = "normal"
output_dir = "../datasets/test2_lv3-12"
phases = "10..60"
samples_per_phase = 1000

[playout]
ai_probability = 0

[self_play]
workers = 19

[window]
enabled = true
size = 10
```

- `phases`: 作る phase。phase は `盤面の石数 - 4`
- `samples_per_phase`: phase ごとのサンプル数
- `playout.ai_probability`: phase 到達前に AI を使う確率。`0` ならランダムのみ
- `self_play.workers`: 並列 worker 数
- `window.enabled`: 複数 phase を同じ self-play trajectory から切り出す高速化

`mode = "all_positions"` を使う場合は [configs/config.all_positions.toml](configs/config.all_positions.toml) を参照してください。

## 出力

```text
datasets/
  phase_10/
    train.rd
    valid.rd
    test.rd
    hashes.jsonl
    metadata.json
    stats.json
  phase_11/
  ...
  summary.json
```

`.rd` は `rdgbitboard_v1` 形式です。

- header: `RDGBBVAL1\n`
- record size: 18 bytes
- `u64 own_bits` little-endian
- `u64 opponent_bits` little-endian
- `i16 value` little-endian

盤面と value は side-to-move 視点です。bit index は `a1 = 0`, `h8 = 63` です。

## 進捗とログ

通常は TUI が stderr に出ます。

```sh
./rdg --no-progress      # 1行ログ
./rdg --json-progress    # JSONL progress
./rdg --log-file logs/rdg.log
```

`--log-file` はログ出力先だけを上書きします。ローテーション設定は [configs/app.toml](configs/app.toml) の `[log]` を使います。

## 開発

```sh
make test
make vet
gofmt -w ./cmd ./internal
```

直接実行する場合:

```sh
go test ./...
go test -race ./...
go vet ./...
```

## License

MIT License. See [LICENSE](LICENSE).
