# DB 設計 JSON の形式

[README](../README.ja.md) · [English](db-design-format.md)

完全な例は [`examples/db-design.example.json`](../examples/db-design.example.json)、
構造の正式な定義は [`schema/db-design.schema.json`](../schema/db-design.schema.json)。

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
| enum | `dbms`: `PostgreSQL`, `MySQL`, `MariaDB`, `SQLite`, `Oracle`, `SQLServer`。`onUpdate` / `onDelete`: `CASCADE`, `RESTRICT`, `SET NULL`, `SET DEFAULT`, `NO ACTION` |

`$schema` をルートに書いておくと、VS Code などのエディタで補完と警告が効く。
`jjf` はこの値を読まない。

**現在の検証は構造検証のみ**である。外部キー参照先の存在、テーブル名・カラム名の重複、
主キーやインデックスのカラムの存在といった**意味的整合性は検証しない**。

## 生成される Excel の構成

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
