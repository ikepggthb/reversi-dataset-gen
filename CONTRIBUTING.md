# Contributing

## コミットメッセージ

### フォーマット（1行目）

```text
type(scope): emoji title
```

- `type` / `scope` は小文字固定
- `emoji` は必須（1つだけ）
- `#Issue` 番号は付けない
- `title` は短く具体的に書く
- 日本語の `title` は現在形/過去形どちらでもよい

### type（固定）

- `feat`：機能追加・変更
- `fix`：バグ修正
- `refactor`：リファクタリング
- `docs`：ドキュメント
- `test`：テスト追加・修正
- `chore`：雑務（ビルド/ツール/依存更新など）
- `style`：見た目・フォーマット（挙動変更なし）

### emoji（typeごとに固定）

- `feat`：✨
- `fix`：🐛
- `refactor`：♻️
- `docs`：📝
- `test`：✅
- `chore`：🔧
- `style`：💄

### scope（固定）

- `cmd`（CLI エントリポイント）
- `config`（設定ファイル・設定読み込み）
- `engine`（Edax / Egaroucid などのエンジン連携）
- `board`（盤面・合法手・ハッシュ）
- `dataset`（dataset 生成・出力・統計）
- `runui`（進捗表示・ログ表示）
- `setup`（エンジン setup・Makefile 連携）
- `docs`（README や docs 配下）
- `workspace`（ルート設定・全体に関わる変更）

複数領域にまたがる場合は、主目的に最も近い scope を選ぶ。全体に関わる変更は `workspace` を使う。

### 例

- `feat(dataset): ✨ window modeの進捗集計を追加`
- `fix(engine): 🐛 Egaroucidのhandshake timeoutを修正`
- `docs(docs): 📝 実行手順を更新`
- `refactor(cmd): ♻️ オプション解析を整理`
- `test(board): ✅ canonical hashのケースを追加`
- `chore(workspace): 🔧 Go依存を更新`
- `style(runui): 💄 進捗表示の余白を調整`
