# Contributing

バグ報告・機能提案・プルリクエストを歓迎します。

## 開発の準備

```sh
git clone https://github.com/ikepggthb/reversi-dataset-gen.git
cd reversi-dataset-gen
make setup-edax   # or make setup-egaroucid
make build
make test
```

## ブランチ名

GitHub Flow に近い短命ブランチ運用を前提に、ブランチ名は軽量にする。

```text
feat/<name>
fix/<name>
docs/<name>
chore/<name>
```

例:

```text
feat/window-mode
fix/edax-timeout
docs/readme-usage
chore/ci
```

ルール:

- `main` から作る
- 小文字英数字とハイフンを使う
- 1 ブランチ = 1 目的にする
- PR マージ後はブランチを削除する
- Issue 番号は必要なときだけ入れる（例: `fix/12-edax-timeout`）

## コミットメッセージ

### フォーマット（1 行目）

```text
type(scope): emoji title
```

例:

```
feat(dataset): ✨ window modeの進捗集計を追加
fix(engine): 🐛 Egaroucidのhandshake timeoutを修正
docs(docs): 📝 実行手順を更新
```

### type と emoji（対応固定）

| type | 意味 | emoji |
|------|------|-------|
| `feat` | 機能追加・変更 | ✨ |
| `fix` | バグ修正 | 🐛 |
| `refactor` | リファクタリング | ♻️ |
| `docs` | ドキュメント | 📝 |
| `test` | テスト追加・修正 | ✅ |
| `chore` | ビルド・ツール・依存更新など | 🔧 |
| `style` | 見た目・フォーマット（挙動変更なし） | 💄 |

### scope（固定）

| scope | 対象 |
|-------|------|
| `cmd` | CLI エントリポイント（`cmd/rdg/`） |
| `config` | 設定ファイル・設定読み込み（`internal/config/`） |
| `engine` | Edax / Egaroucid などのエンジン連携（`internal/engine/`） |
| `board` | 盤面・合法手・ハッシュ（`internal/board/`） |
| `dataset` | データセット生成・出力・統計（`internal/dataset/`） |
| `runui` | 進捗表示・ログ表示（`internal/runui/`） |
| `setup` | エンジン setup・Makefile 連携（`internal/setup/`） |
| `docs` | README や docs 配下 |
| `workspace` | ルート設定・全体に関わる変更 |

複数領域にまたがる場合は、主目的に最も近い scope を選ぶ。全体に関わる変更は `workspace` を使う。

### その他のルール

- `type` / `scope` は小文字固定
- `emoji` は必須（1 つだけ）
- `#Issue` 番号は 1 行目に付けない
- `title` は短く具体的に。日本語の場合は現在形・過去形どちらでも可
