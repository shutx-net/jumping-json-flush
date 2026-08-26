# Jumpin' Json Flush

[English](README.md)

[![CI](https://github.com/shutx-net/jumping-json-flush/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/shutx-net/jumping-json-flush/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/shutx-net/jumping-json-flush)](go.mod)

`jjf` は DB 設計を 1 つの JSON ファイルで持ち、そこから残りを生成する CLI。
Excel の DB 設計書、ER 図（`jjf` 自身が描く SVG、または Graphviz の DOT ソース）、
PostgreSQL の DDL スクリプトを出力する。
JSON が唯一の正であり、生成されるファイルはすべてビルド成果物である。編集せず
作り直す。

```sh
jjf import postgres schema.sql -o db-design.json   # pg_dump のファイルから作る
jjf validate db-design.json                        # 検証する
jjf export xlsx db-design.json -o db-design.xlsx   # Excel の DB 設計書
jjf export dot  db-design.json -o er.dot           # Graphviz の ER 図
jjf export svg  db-design.json -o er.svg           # jjf 自身が描く ER 図
jjf export ddl  db-design.json -o schema.sql       # PostgreSQL の DDL スクリプト
```

- **依存なし。** `go.mod` に `require` は 1 行もない。JSON Schema の検証器も
  Excel の書き出しも標準ライブラリの上に自前で書いてある。CGO なし・実行時依存
  なしの単一バイナリで、musl/alpine でもそのまま動く
- **決定的な出力。** 同じ入力からは常にバイト同一のファイルが出るので、CI で
  成果物のハッシュを比較する意味がある
- **AI エージェント前提。** JSON Schema による構造検証と Agent Skill で、
  エージェントが設計 JSON を安全に編集できる

## インストール

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | sh
```

OS と CPU に対応するアーカイブを取得し、リリースの `checksums.txt` と sha256 を
照合したうえで配置する。配置先は `/usr/local/bin` が書き込み可能ならそこ、
そうでなければ `$HOME/.local/bin`。`sudo` は一切呼ばない。

他に `go install github.com/shutx-net/jumping-json-flush/cmd/jjf@latest`、
`nix profile add github:shutx-net/jumping-json-flush`、
[Releases](https://github.com/shutx-net/jumping-json-flush/releases) のアーカイブ
（`linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64`, `darwin/arm64`）。

バージョン固定・Windows・CI・アンインストールは
[`docs/install.ja.md`](docs/install.ja.md)（[English](docs/install.md)）にある。

## 各コマンドの役割

- **`import`** が読むのは `pg_dump --schema-only` の**ファイル**であり、`jjf` が
  データベースへ接続することはない。組み立てた文書は書き出す前に検証する。
  `CHECK` 制約のように設計フォーマットに書き場所が無いものは行番号付きで報告する
- **`validate`** はバイナリに埋め込まれた schema に対し、違反を**全件まとめて**
  報告する。位置はそれぞれ JSON Pointer で示される。続けて文書が自分自身と
  矛盾していないか（参照先の無い外部キー、`nullable` な主キー列、SQL 式として
  読めない既定値など）を警告として報告する。`-strict` を付けると失敗になる
- **`export`** は必ず先に検証するので、検証を通らない文書からは 1 バイトも
  出力されない。`ddl` だけはさらに、自分自身と矛盾する文書を拒否する。
  データベースが受け付けない SQL は何の役にも立たないからである

終了コード 2 は入力不正、3 は JSON Schema 違反だけを意味する。各コマンドと
オプションは [`docs/usage.ja.md`](docs/usage.ja.md)（[English](docs/usage.md)）
にある。

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
すべてのオブジェクトで拒否される。

各項目と全規則は [`docs/db-design-format.ja.md`](docs/db-design-format.ja.md)
（[English](docs/db-design-format.md)）にある。完全な例は
[`examples/db-design.example.json`](examples/db-design.example.json)、構造の
正式な定義は [`schema/db-design.schema.json`](schema/db-design.schema.json)。

## AI エージェントから使う

`skills/db-design/` は DB 設計の規約を
[Agent Skill](https://code.claude.com/docs/en/skills.md) 形式で提供する。
Claude Code へはプラグインとして入る。**git も npm も Node も不要**。

```text
/plugin marketplace add https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/.claude-plugin/marketplace.json
/plugin install jjf@jjf-tools
```

呼び出しは `/jjf:db-design`。スキルは
[Agent Skills](https://agentskills.io) 仕様に従っているので、
`gh skill install shutx-net/jumping-json-flush db-design --agent codex` で
Codex や GitHub Copilot が見る場所へ同じディレクトリを配置できる。その他の
導入方法とリリース手順は [`skills/README.ja.md`](skills/README.ja.md)。

## CI に組み込む

`db-design.json` の変更を検知して検証し、`.xlsx` は成果物として保持する。
派生成果物なのでコミットはしない。すぐ使えるサンプル:
[`examples/ci/github-actions.yml`](examples/ci/github-actions.yml) と
[`examples/ci/gitlab-ci.yml`](examples/ci/gitlab-ci.yml)。

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

ツールのバージョンと DB 設計フォーマットのバージョンは**独立**している。`jjf`
本体は Semantic Versioning に従う。設計文書は `formatVersion`（`MAJOR.MINOR`、
現在は `1.0`）を持ち、これはフォーマット自体が非互換に変わったときだけ上がる。
対応外のメジャーバージョンを読ませると終了コード 2 で `jjf` の更新を促す。

## 対象外

稼働中の DB への接続、マイグレーション管理とスキーマ差分、設計の良し悪しの判断
（正規化・インデックス設計・型選択）、Mermaid と Markdown の出力、`.dot` の画像化、
Excel から JSON への逆変換、Excel の直接編集、GUI、Excel テンプレートの
カスタマイズ。

生成する DDL はスキーマを一から作るものである。既にスキーマを持つデータベースへ
設計を適用するには、そのデータベースの状態を知る必要があり、それは別のツールの
仕事である。DDL の形式を決めた判断は
[`design/ddl-export.md`](design/ddl-export.md) に記録してある。

## ライセンス

[MIT](LICENSE)
