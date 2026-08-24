# Jumpin' Json Flush

[English](README.md)

[![CI](https://github.com/shutx-net/jumping-json-flush/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/shutx-net/jumping-json-flush/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/shutx-net/jumping-json-flush)](go.mod)

**Jumpin' Json Flush**（`jjf`）は、構造化された JSON 形式の DB 設計情報を
Single Source of Truth として管理し、人間向けの設計成果物へ変換する CLI ツール。
Excel の DB 設計書、Graphviz の ER 図、PostgreSQL の DDL スクリプトを生成する。

- **JSON が唯一の正。** 生成された `.xlsx`・`.dot`・`.sql` は派生成果物であり、
  権威あるデータとして扱わない
- **決定的な出力。** 同じ入力からは常にバイト同一の `.xlsx`・`.dot`・`.sql` が生成される
- **単一バイナリ。** CGO なし・実行時の外部依存なし。musl/alpine 環境でもそのまま動く
- **AI エージェント前提。** JSON Schema による構造検証と Agent Skill で、
  エージェントが設計 JSON を安全に編集できる。スキルは Claude Code プラグインとして
  配布し（`/plugin install jjf@jjf-tools`）、Agent Skills 仕様に従っているので
  Codex や GitHub Copilot にもそのまま入る

```sh
jjf import postgres schema.sql -o db-design.json
jjf validate db-design.json
jjf export xlsx db-design.json -o db-design.xlsx
jjf export dot db-design.json -o er.dot
jjf export ddl db-design.json -o schema.sql
```

## インストール

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | sh
```

OS と CPU に対応するアーカイブを取得し、**リリースの `checksums.txt` と sha256 を
照合**したうえで配置する。配置先は `/usr/local/bin` が書き込み可能ならそこ、
そうでなければ `$HOME/.local/bin`。`sudo` は一切呼ばず、どこに入ったかを表示する。

他に 3 通り:

- `go install github.com/shutx-net/jumping-json-flush/cmd/jjf@latest`
- `nix profile add github:shutx-net/jumping-json-flush`
- [Releases](https://github.com/shutx-net/jumping-json-flush/releases) のアーカイブ。
  `linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64`, `darwin/arm64`
  の 5 種類で、それぞれ `checksums.txt` 付き

バージョン固定・配置先の指定・Windows・CI・手動での検証・アンインストールは
[`docs/install.ja.md`](docs/install.ja.md)（[English](docs/install.md)）に
まとめてある。

## 使い方

```sh
# PostgreSQL のスキーマダンプから設計 JSON を組み立てる
pg_dump --schema-only mydb > schema.sql
jjf import postgres schema.sql -o db-design.json

# 組み込みの JSON Schema で設計 JSON を検証する
jjf validate db-design.json

# Excel の DB 設計書に変換する
jjf export xlsx db-design.json -o db-design.xlsx

# Graphviz の ER 図に変換する
jjf export dot db-design.json -o er.dot

# PostgreSQL の DDL スクリプトに変換する
jjf export ddl db-design.json -o schema.sql
```

import が読むのは `pg_dump --schema-only` の**ファイル**であり、`jjf` が
データベースへ接続することはない。生成した文書は書き出す前に検証するので、
`jjf validate` が拒否する文書が import から残ることはない。`CHECK` 制約や
部分索引のように設計フォーマットに書き場所が無いものは、行番号付きで標準エラーに
報告し、周囲のテーブルはそのまま取り込む。

検証結果は**全件まとめて**報告され、それぞれが JSON Pointer で位置を指す。
スキーマはバイナリに埋め込まれているのでネットワークには一切アクセスしない。
export は必ず先に検証するため、検証に失敗した文書は**出力ファイルを 1 バイトも
生成しない**。**同じ入力からは常にバイト同一の `.xlsx`** が得られるので、CI で
成果物のハッシュを比較する意味がある。

`ddl` だけはさらに踏み込み、`jjf validate` が警告にとどめる矛盾と、validate が
意図的に見ない PostgreSQL 固有の誤りとを、どちらも拒否する。矛盾した文書でも
Excel 設計書と ER 図は役に立つので `xlsx` と `dot` は描き出すが、データベースが
受け付けない SQL は何の役にも立たないので、`ddl` は何も書かずに 2 で終わる。

`jjf validate` は構造検証に加えて、文書が**自分自身と矛盾していないか**を
検査する。キーとインデックスが指すカラムの存在、外部キーの参照先テーブルが
定義されていること、列数が一致すること、参照先の列が一意（主キー・ユニークキー・
ユニークインデックスのいずれか）であること、主キーのカラムが `nullable: true`
でないこと、同一テーブル内でカラム名・制約名が重複しないこと、既定値が空でなく
SQL 式として読めることの 8 点である。
検出結果は警告として標準エラーに出力し、終了コードは 0 のままなので、いま通る
文書はこれからも通る。`-strict` を付けたときだけ失敗する。

3 つのコマンドとオプション、`-o` の規則、パイプラインが読む終了コード
（不正な入力は 2、スキーマ違反は 3）は
[`docs/usage.ja.md`](docs/usage.ja.md)（[English](docs/usage.md)）にある。
終了コード 3 は JSON Schema への適合だけを意味する。参照整合性の指摘は
スキーマ違反ではないので、`validate -strict` はそれを 3 ではなく 2 で報告する。

## DB 設計 JSON

```json
{
  "formatVersion": "1.0",
  "database": { "name": "ec_shop", "logicalName": "ECサイト", "dbms": "PostgreSQL" },
  "tables": [
    {
      "name": "users",
      "logicalName": "会員",
      "columns": [
        { "name": "id", "logicalName": "会員ID", "type": "BIGINT", "nullable": false },
        { "name": "email", "logicalName": "メールアドレス", "type": "VARCHAR",
          "length": 255, "nullable": false }
      ],
      "primaryKey": { "name": "pk_users", "columns": ["id"] }
    }
  ]
}
```

物理名は ASCII のみで、日本語は `logicalName` に書く。未知のプロパティは
すべてのオブジェクトで拒否される。完全な例は
[`examples/db-design.example.json`](examples/db-design.example.json)、構造の
正式な定義は [`schema/db-design.schema.json`](schema/db-design.schema.json)。

各項目・全規則・生成される Excel の 3 シートの内容は
[`docs/db-design-format.ja.md`](docs/db-design-format.ja.md)
（[English](docs/db-design-format.md)）にまとめてある。

## AI エージェントから使う

`skills/db-design/` は、DB 設計の規約と作業ルールを
[Agent Skills](https://code.claude.com/docs/en/skills.md) 形式で提供する。
守るべきルール、変更したら検証するワークフロー、許可される enum 値、実際の
エラーメッセージと対処の対応表、編集レシピが含まれる。Claude Code へは
プラグインとして入れられる。**git も npm も Node も不要**。

```text
/plugin marketplace add https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/.claude-plugin/marketplace.json
/plugin install jjf@jjf-tools
```

呼び出しは `/jjf:db-design`。Claude Code 専用ではない。スキルは
[Agent Skills](https://agentskills.io) 仕様に従い、仕様外のものを使っていないので、
`gh skill install shutx-net/jumping-json-flush db-design --agent codex` で
Codex や GitHub Copilot ほか仕様を実装したホストが見る場所へ、同じディレクトリを
配置できる。その手順と、プラグインを使わない導入方法、リリース手順は
[`skills/README.ja.md`](skills/README.ja.md)。スキル本体は英語だが、同じ内容を
人間が読むための日本語版が
[`docs/db-design-guide.ja.md`](docs/db-design-guide.ja.md) にある。

## CI に組み込む

`db-design.json` の変更を検知して検証し、`.xlsx` は成果物として保持する。
派生成果物なのでコミットはしない。すぐ使えるサンプル:
[`examples/ci/github-actions.yml`](examples/ci/github-actions.yml) と
[`examples/ci/gitlab-ci.yml`](examples/ci/gitlab-ci.yml)。

## 依存関係

**なし。** `go.mod` に `require` は 1 行もない。`jjf` がすることはすべて Go の
標準ライブラリの上で完結している。

JSON Schema の検証も例外ではない。`internal/schema` が持っているのは仕様の
汎用実装ではなく、この schema 専用の検証器である。
`schema/db-design.schema.json` が実際に使っているキーワードだけを実装し、
それ以外は実装しない。実装していないキーワードを schema に書き足しても黙って
無視されることはない。schema は未知のキーを受け付けない Go の型へ読み込んで
おり、その時点で `jjf` は起動に失敗する。schema 自体が JSON Schema
Draft 2020-12 として妥当かどうかは CI が検証しており、そのツールもモジュール
パスから直接実行するので `go.mod` には入らない。

Excel 出力は `archive/zip` と `encoding/xml` の上に自前で書いており、
サードパーティの Excel ライブラリは使っていない。`go version -m jjf` で、
バイナリが依存を 1 つも記録していないことを確認できる。

## ドキュメント

サイトとしても公開している: **<https://shutx-net.github.io/jumping-json-flush/>**

| | |
| --- | --- |
| [`docs/install.ja.md`](docs/install.ja.md) | インストール、バージョン固定、ダウンロードの検証 |
| [`docs/usage.ja.md`](docs/usage.ja.md) | コマンドとオプション、終了コード |
| [`docs/db-design-format.ja.md`](docs/db-design-format.ja.md) | 設計 JSON の全項目と、生成される Excel |
| [`docs/db-design-guide.ja.md`](docs/db-design-guide.ja.md) | スキルと同じ規約を人間が読むための日本語ガイド |
| [`skills/README.ja.md`](skills/README.ja.md) | Agent Skill とその配布方法 |
| [DEVELOPERS.md](https://github.com/shutx-net/jumping-json-flush/blob/main/DEVELOPERS.md) | 開発環境の用意とコマンド表（英語のみ） |

DEVELOPERS.md 以外はすべて英語版が隣にある。DEVELOPERS.md をパスではなく URL で
リンクしているのは、リリースアーカイブが同梱するのは README だけのため。

## バージョニング

ツールのバージョンと DB 設計フォーマットのバージョンは**独立**している。

- `jjf` 本体は Semantic Versioning に従う（`v0.1.0` のようなタグ）
- DB 設計文書は `formatVersion`（`MAJOR.MINOR`）を持つ。現在は `1.0`
- `formatVersion` は**フォーマット自体が非互換に変わったときだけ**上がる
- 対応外のメジャーバージョンを読ませると、専用のメッセージで終了コード 2 になる
  （`unsupported formatVersion "2.0"; this jjf supports 1.x - please upgrade jjf`）

## 対象外

稼働中の DB への接続、Mermaid 出力、Markdown 出力、
設計の良し悪しの判断（正規化・インデックス設計・型選択）、
マイグレーション管理、Excel から JSON への逆変換、
Excel の直接編集、GUI、Excel テンプレートのカスタマイズは対象外である。

PostgreSQL の DDL スクリプトは `jjf export ddl` で生成する
（[`docs/usage.ja.md`](docs/usage.ja.md#export)）。生成する DDL はスキーマを
一から作るものであり、その形式を決めた判断は
[`design/ddl-export.md`](design/ddl-export.md) に記録してある。既にスキーマを
持つデータベースへ適用することは対象外のままである。その状態を知る必要があるためである。

ER 図は Graphviz DOT のソースとして生成する（`jjf export dot`、
[`docs/usage.ja.md`](docs/usage.ja.md#export)）。画像への変換は対象外であり、
読み手自身の `dot` で行う。それが `jjf` を実行時依存のない単一バイナリに
保っている理由である。

**`pg_dump --schema-only` のファイル**からのスキーマ取り込みは PostgreSQL に限り
対応している（[`docs/usage.ja.md`](docs/usage.ja.md#import)）。稼働中のサーバから
直接スキーマを読むことは対象外である。

## ライセンス

[MIT](LICENSE)
