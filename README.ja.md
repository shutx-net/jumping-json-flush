# Jumpin' Json Flush

[English](README.md)

**Jumpin' Json Flush**（`jjf`）は、構造化された JSON 形式の DB 設計情報を
Single Source of Truth として管理し、人間向けの Excel DB 設計書へ変換する CLI ツール。

- **JSON が唯一の正。** 生成された `.xlsx` は派生成果物であり、権威あるデータとして扱わない
- **決定的な出力。** 同じ入力からは常にバイト同一の `.xlsx` が生成される
- **単一バイナリ。** CGO なし・実行時の外部依存なし。musl/alpine 環境でもそのまま動く
- **AI エージェント前提。** JSON Schema による構造検証と Agent Skill で、
  エージェントが設計 JSON を安全に編集できる。スキルは Claude Code プラグインとして
  配布する（`/plugin install jjf@jjf-tools`）

```sh
jjf validate db-design.json
jjf export xlsx db-design.json -o db-design.xlsx
```

## インストール

### リリースバイナリ

[Releases](https://github.com/shutx-net/jumping-json-flush/releases) から使用する OS / CPU 向けの
アーカイブを取得する。対応ターゲットは `linux/amd64`, `linux/arm64`, `windows/amd64`,
`darwin/amd64`, `darwin/arm64` の 5 種類。

```sh
VERSION=v0.1.0
curl -sSfL -o jjf.tar.gz \
  "https://github.com/shutx-net/jumping-json-flush/releases/download/${VERSION}/jjf_${VERSION}_linux_amd64.tar.gz"
tar xzf jjf.tar.gz
sudo install -m 0755 "jjf_${VERSION}_linux_amd64/jjf" /usr/local/bin/jjf
jjf version
```

各リリースには `checksums.txt`（sha256）が添付されている。

```sh
curl -sSfL -O "https://github.com/shutx-net/jumping-json-flush/releases/download/${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
```

### go install

```sh
go install github.com/shutx-net/jumping-json-flush/cmd/jjf@latest
```

バージョンを固定する場合は `@v0.1.0` のようにタグを指定する。Go 1.24 以上が必要。

## 使い方

### validate

```sh
jjf validate db-design.json
```

DB 設計 JSON を組み込みの JSON Schema（Draft 2020-12）で検証する。
違反は**全件まとめて**報告され、それぞれが JSON Pointer で位置を指す。

```text
db-design.json: does not conform to the jjf database design schema
  /database/dbms                   value must be one of 'PostgreSQL', 'MySQL', 'MariaDB', 'SQLite', 'Oracle', 'SQLServer'
  /tables/0                        missing property 'logicalName'
  /tables/0/columns/0              missing property 'nullable'
  /tables/0/columns/1/logicalName  minLength: got 0, want 1
  /tables/0/columns/1/name         '9bad' does not match pattern '^[A-Za-z_][A-Za-z0-9_]*$'
  /tables/0/columns/1/nullable     got string, want boolean
  /tables/0/indexes/0              missing property 'name'

7 error(s). See schema/db-design.schema.json.
```

検証はネットワークにアクセスしない。スキーマはバイナリに埋め込まれているため、
文書に `$schema` が書かれていても外部を取得しにいくことはない。

### export

```sh
jjf export xlsx db-design.json -o db-design.xlsx
```

- 出力前に必ず検証する。**検証に失敗した文書からは出力ファイルが 1 バイトも作られない**
- `-o` を省略すると、**入力パスの拡張子を `.xlsx` に置き換えた場所**へ出力する
  （`docs/db-design.json` → `docs/db-design.xlsx`）
- `-o -` で標準出力へ書き出せる。ただし標準出力が**端末の場合は拒否する**
  （バイナリで画面を汚さないため）。パイプやリダイレクトなら通る
- 出力は一時ファイルへ書いてから rename するので、途中で失敗しても壊れたファイルが残らない
- Phase 1 の対応フォーマットは `xlsx` のみ

```sh
# パイプへ流す
jjf export xlsx db-design.json -o - | sha256sum

# 端末へ直接出そうとすると拒否される (終了コード 2)
jjf export xlsx db-design.json -o -
# jjf: refusing to write a workbook to the terminal; redirect standard output or pass -o <file>
```

#### バイト決定性

**同じ入力からは常にバイト同一の `.xlsx` が生成される。**
生成日時を埋め込まず、ZIP のタイムスタンプを固定し、Go の map の反復順に依存しないためである。

```sh
jjf export xlsx db-design.json -o a.xlsx
jjf export xlsx db-design.json -o b.xlsx
sha256sum a.xlsx b.xlsx   # 2 つのハッシュは一致する
```

これにより CI で成果物のハッシュを比較したり、
「JSON を変えていないのに設計書が変わった」を異常として検出できる。

### version

```sh
jjf version
# jjf v0.1.0
# built with go1.24.0 for linux/amd64
```

リリースバイナリはタグ名を、`go install` で入れたものは Go が記録した
モジュールバージョンを表示する。

### 終了コード

| コード | 意味 | 典型的な原因 |
| --- | --- | --- |
| 0 | 成功 | — |
| 1 | 一般エラー | 上記のいずれにも分類されない内部エラー |
| 2 | 入力不正 | 引数の誤り、ファイルが無い、JSON 構文エラー、未対応の `formatVersion`、未知の出力形式、`-o -` を端末に向けた |
| 3 | スキーマ検証エラー | JSON Schema 違反 |
| 4 | 出力生成エラー | 出力先に書き込めない、ディレクトリが無い |

CI で使うときは **3 と 2 を区別できる**ことが重要である。3 は設計 JSON の中身の問題、
2 は呼び出し方・ファイルの場所・`jjf` のバージョンの問題である。

成功メッセージは標準出力、エラーと usage は標準エラーに出力される。

## DB 設計 JSON の形式

完全な例は [`examples/db-design.example.json`](examples/db-design.example.json)、
構造の正式な定義は [`schema/db-design.schema.json`](schema/db-design.schema.json)。

```json
{
  "$schema": "https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/schema/db-design.schema.json",
  "formatVersion": "1.0",
  "database": {
    "name": "ec_shop",
    "logicalName": "ECサイト",
    "dbms": "PostgreSQL"
  },
  "tables": [
    {
      "name": "users",
      "logicalName": "会員",
      "columns": [
        {
          "name": "id",
          "logicalName": "会員ID",
          "type": "BIGINT",
          "nullable": false,
          "autoIncrement": true
        },
        {
          "name": "email",
          "logicalName": "メールアドレス",
          "type": "VARCHAR",
          "length": 255,
          "nullable": false
        }
      ],
      "primaryKey": { "name": "pk_users", "columns": ["id"] },
      "uniqueKeys": [{ "name": "uq_users_email", "columns": ["email"] }]
    }
  ]
}
```

要点:

| 項目 | 内容 |
| --- | --- |
| エンコーディング | UTF-8（BOM 付きも受け付ける）。改行は LF 推奨 |
| ルート必須 | `formatVersion`, `database`, `tables` |
| テーブル必須 | `name`, `logicalName`, `columns` |
| カラム必須 | `name`, `logicalName`, `type`, `nullable` |
| 未知プロパティ | すべてのオブジェクトで**禁止**（`additionalProperties: false`） |
| 物理名 | `^[A-Za-z_][A-Za-z0-9_]*$`、128 文字以内。日本語は `logicalName` に書く |
| 型名 | `VARCHAR(30)` のようなパラメータ込みは不可。`type: "VARCHAR"` + `length: 30` に分ける |
| 既定値 | `default` は文字列のみ。SQL リテラルは引用符込み（`"'pending'"`）。DEFAULT 句なしはキー自体を書かない |
| enum | `dbms` は 6 値、`onUpdate` / `onDelete` は 5 値（`CASCADE`, `RESTRICT`, `SET NULL`, `SET DEFAULT`, `NO ACTION`） |

`$schema` をルートに書いておくと、VS Code などのエディタで補完と警告が効く。
`jjf` はこの値を読まない。

**現在の検証は構造検証のみ**である。外部キー参照先の存在、テーブル名・カラム名の重複、
主キーやインデックスのカラムの存在といった**意味的整合性は検証しない**。

### 生成される Excel の構成

| シート | 内容 |
| --- | --- |
| 表紙 | データベース名・論理名・DBMS・テーブル数・フォーマットバージョン・説明 |
| テーブル一覧 | 全テーブルの物理名／論理名／説明／カラム数／シート名 |
| テーブル定義 | 1 テーブル 1 シート。カラム定義と、主キー・ユニークキー・外部キー・インデックスのブロック |

表記のルール:

- `NULL` 列と `自動採番` 列は `○` = 該当、空セル = 非該当
- `長さ` 列は `length` / `precision` / `precision,scale` のいずれか。桁指定のない型では空
- シート名は 31 文字（Excel の上限）に切り詰められ、衝突時は `(2)` `(3)` … が付く。
  テーブル一覧シートには**実際に割り当てられたシート名**が出るので、切り詰めや採番が起きたか分かる
- レイアウト・配色は `jjf` が固定で持ち、JSON 側からは制御できない

## AI エージェントから使う

`skills/db-design/` に、AI エージェント向けの DB 設計ルールと操作方針を
[Agent Skills](https://code.claude.com/docs/en/skills.md) 形式で同梱している。
スキル本文は英語だが、トリガー語に日本語も含めてあるので日本語の依頼でも起動する。

Claude Code へは[プラグイン](https://code.claude.com/docs/en/plugins-reference.md)
として導入する。**利用者側に git / npm / Node は不要**で、HTTPS 経由で zip
アーカイブを取得する（`archive` ソースのため Claude Code v2.1.224 以降が必要）。

```text
/plugin marketplace add https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/.claude-plugin/marketplace.json
/plugin install jjf@jjf-tools
```

導入後は `/jjf:db-design` で呼び出せる。マーケットプレース定義とプラグイン
マニフェストは [`.claude-plugin/`](.claude-plugin/marketplace.json) にあり、
アーカイブは各リリースの `jjf-plugin-<tag>.zip` として公開される。

プラグインを使わず `.claude/skills/` へディレクトリごとコピーする方法や、
リリース手順は [`skills/README.ja.md`](skills/README.ja.md) を参照。含まれる内容:

- Excel を直接編集しない／変更は JSON に対して行う、といったハードルール
- 変更 → `jjf validate` → 成功を確認してから完了、というワークフロー
- 必須プロパティ早見表と enum の全許容値
- DBMS ごとの推奨型名
- 実際の検証エラーメッセージと直し方の対応表
- テーブル追加・カラム変更・外部キー追加などの編集レシピ
- 生成される Excel の表記ルールと、JSON から制御できない範囲

スキル本文は英語なので、**同じ内容を日本語で読み・レビューする**ための人間向け
ドキュメントを [`docs/db-design-guide.ja.md`](docs/db-design-guide.ja.md) に置いてある。
上記の内容を 1 本の通読できるガイドにまとめたものである。エージェントが読むのは
英語スキルであり、両者が食い違った場合は英語スキルが正である。

## CI に組み込む

`db-design.json` の変更を検出して検証し、失敗したら CI を落とし、
成功したら `.xlsx` を artifact として保存する構成を推奨する。
生成された `.xlsx` はリポジトリにコミットしない（派生成果物なので `.gitignore` に入れる）。

そのまま使えるワークフロー例:

- GitHub Actions: [`examples/ci/github-actions.yml`](examples/ci/github-actions.yml)
- GitLab CI: [`examples/ci/gitlab-ci.yml`](examples/ci/gitlab-ci.yml)

## 依存関係

**直接依存 1 件と、回避不可能な間接依存 1 件のみ。**

| モジュール | 種別 | 用途 |
| --- | --- | --- |
| `github.com/santhosh-tekuri/jsonschema/v6` | 直接 | JSON Schema Draft 2020-12 検証 |
| `golang.org/x/text` | 間接 | `jsonschema/v6` が `ErrorKind.LocalizedString(*message.Printer)` を公開 API に露出しているため回避できない |

最終バイナリに記録される依存も上記 2 件だけである（`go version -m jjf` で確認できる）。
Excel 出力は `archive/zip` と `encoding/xml` による自前実装であり、外部の Excel ライブラリは使わない。
実行時の外部依存は無く、`CGO_ENABLED=0` の静的バイナリとして配布される。

## 開発

Go はコンテナ内にのみ用意する。[devcontainer CLI](https://github.com/devcontainers/cli) 経由で実行する。

```sh
devcontainer up   --workspace-folder .
devcontainer exec --workspace-folder . bash -lc '<CMD>'
```

| 目的 | `<CMD>` |
| --- | --- |
| ビルド | `go build -o /tmp/jjf ./cmd/jjf` |
| 実行 | `go run ./cmd/jjf validate examples/db-design.example.json` |
| テスト | `go test ./...` |
| テスト（race） | `CGO_ENABLED=1 go test -race ./...` |
| ゴールデン再生成 | `go test ./cmd/jjf/ ./internal/schema/ ./internal/sml/ ./internal/export/xlsx/ -update` |
| カバレッジ | `go test -covermode=atomic -coverprofile=/tmp/c.out ./... && go tool cover -func=/tmp/c.out \| tail -1` |
| vet | `go vet ./...` |
| 書式チェック | `test -z "$(gofmt -l .)" \|\| gofmt -d .` |
| 書式適用 | `gofmt -w .` |
| staticcheck | `staticcheck ./...` |
| クロスビルド確認 | `for t in linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64; do CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -trimpath -ldflags "-s -w" -o /dev/null ./cmd/jjf \|\| echo "FAIL $t"; done` |

注意点:

- `gofmt -l` は未整形ファイルがあっても終了コード 0 を返す。ゲートには必ず
  `test -z "$(gofmt -l .)"` を使う
- `go test -race` は CGO を要求する。`CGO_ENABLED=0` を環境に固定してはならない
- staticcheck は CI では `go run honnef.co/go/tools/cmd/staticcheck@2026.1 ./...` で実行し、
  `go.mod` には入れない（`go get -tool` は間接依存を増やし `go` ディレクティブを引き上げる）
- `go run ./cmd/jjf ...` は `jjf` 自身の終了コードを隠す。終了コードを検証するときは
  ビルド済みバイナリを使う
- `-update` フラグを定義しているのはゴールデンを持つ 4 パッケージだけなので、
  `go test ./... -update` は残りのパッケージで `flag provided but not defined` になる。
  上の表のようにパッケージを列挙して渡す

### リポジトリ構成

```text
cmd/jjf/               CLI（サブコマンド分岐・引数解析・終了コード）
internal/exitcode/     終了コードとエラーラップ
internal/model/        DB 設計 JSON に 1 対 1 対応する Go 型とデコード
internal/schema/       埋め込みスキーマのコンパイルと検証エラーの整形
internal/sml/          汎用 SpreadsheetML / OPC ライタ（DB 設計を知らない）
internal/export/xlsx/  DB 設計書レンダラ（レイアウトの唯一の所有者）
schema/                【正】DB 設計 JSON Schema と go:embed 宣言
skills/db-design/      【正】AI エージェント向け Agent Skill
.claude-plugin/        プラグインマニフェストとマーケットプレース定義
examples/              サンプル設計 JSON と CI ワークフロー例
docs/                  人間向け日本語ドキュメント（DB 設計ガイド）
```

## バージョニング

ツールのバージョンと DB 設計フォーマットのバージョンは**独立**している。

- `jjf` 本体は Semantic Versioning に従う（`v0.1.0` のようなタグ）
- DB 設計文書は `formatVersion`（`MAJOR.MINOR`）を持つ。現在は `1.0`
- `formatVersion` は**フォーマット自体が非互換に変わったときだけ**上がる
- 対応外のメジャーバージョンを読ませると、専用のメッセージで終了コード 2 になる
  （`unsupported formatVersion "2.0"; this jjf supports 1.x - please upgrade jjf`）

## 対象外

DB への接続・既存 DB からのスキーマ取り込み、DDL 生成、ER 図 / Mermaid 出力、
Markdown 出力、意味的整合性検証、マイグレーション管理、Excel から JSON への逆変換、
Excel の直接編集、GUI、Excel テンプレートのカスタマイズは対象外である。

## ライセンス

[MIT](LICENSE)
