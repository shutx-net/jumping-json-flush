# Jumpin' Json Flush

[README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.ja.md) · [English](index.md)

**Jumpin' Json Flush**（`jjf`）は、構造化された JSON 形式の DB 設計情報を
Single Source of Truth として管理し、人間向けの設計成果物へ変換する CLI ツール。
Excel の DB 設計書、Graphviz の ER 図、PostgreSQL の DDL スクリプトを生成する。

```sh
jjf import postgres schema.sql -o db-design.json
jjf validate db-design.json
jjf export xlsx db-design.json -o db-design.xlsx
jjf export dot db-design.json -o er.dot
jjf export ddl db-design.json -o schema.sql
```

- **[インストール](install.ja.md)** — ワンライナー、バージョン固定、配置先の指定、
  手動での検証
- **[使い方](usage.ja.md)** — 3 つのコマンド、`-o` の規則、パイプラインが読む終了コード
- **[DB 設計 JSON の形式](db-design-format.ja.md)** — 各項目・全規則・生成される
  Excel の 3 シート
- **[DB 設計ガイド](db-design-guide.ja.md)** — Agent Skill と同じ規約を人間が読む形で

プロジェクト全体の概要・CI への組み込み・Agent Skill・対象外の範囲は
[リポジトリの README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.ja.md)
にある。
