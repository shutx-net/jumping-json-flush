# jjf DB 設計ガイド（日本語）

`jjf` の DB 設計 JSON を書く・直す・検証する・Excel 設計書に変換するための作法をまとめた
日本語ドキュメント。

## このドキュメントの位置づけ

**これは人間が読むための資料である。** 日本語話者の開発者が DB 設計の作法・型語彙・
エラー対処・編集レシピをレビューし、必要なときに参照するために置いてある。

| 対象 | ファイル | 言語 | 読み手 |
| --- | --- | --- | --- |
| 構造の正式定義 | [`schema/db-design.schema.json`](https://github.com/shutx-net/jumping-json-flush/blob/main/schema/db-design.schema.json) | — | ツールとエディタ |
| Agent Skill | [`skills/db-design/SKILL.md`](https://github.com/shutx-net/jumping-json-flush/blob/main/skills/db-design/SKILL.md) と `references/` | 英語 | AI エージェント |
| 設計ガイド（この文書） | `docs/db-design-guide.ja.md` | 日本語 | 人間 |

押さえておくべき点が 3 つある。

- **AI エージェントが実際に読むのは英語版のスキル**である。Claude などのエージェントに対しては
  英語で指示するほうが安定するため、スキル本文は英語 1 本に統一してある。
  トリガー語に日本語も含めてあるので、依頼を日本語で書いてもスキルは起動する。
- **構造の正は JSON Schema** である。必須プロパティ・型・enum・pattern を最終的に決めるのは
  `schema/db-design.schema.json`（JSON Schema Draft 2020-12）だけであり、この文書ではない。
- **この日本語ガイドと英語スキルが食い違った場合は、英語スキルが正である。**
  内容は英語スキルと同じものを日本語で書いたものだが、更新が前後することはありうる。
  食い違いを見つけたら、この文書側の誤りとして直す。

## 目次

1. [概要](#概要)
2. [このガイドが対象とする作業](#このガイドが対象とする作業)
3. [権威ある入力](#権威ある入力)
4. [ハードルール](#ハードルール)
5. [ワークフロー](#ワークフロー)
6. [文書構造の全体像](#文書構造の全体像)
7. [フィールドの作法と許容値](#フィールドの作法と許容値)
8. [DBMS ごとの推奨型名](#dbms-ごとの推奨型名)
9. [編集レシピ](#編集レシピ)
10. [コマンドと終了コード](#コマンドと終了コード)
11. [検証エラーと直し方](#検証エラーと直し方)
12. [生成される Excel の読み方](#生成される-excel-の読み方)
13. [formatVersion の扱い](#formatversion-の扱い)
14. [適用範囲外](#適用範囲外)

## 概要

`jjf` は DB 設計を JSON 文書（以下 `db-design.json`）として保持し、Excel ブックとして
出力する。**JSON が単一の正**である。`.xlsx` は派生成果物であり、エクスポートごとにゼロから
作り直され、同じ入力からは常にバイト同一のファイルが出る。

したがって設計変更はすべて JSON への変更である。「DB 設計書（Excel）を更新して」という依頼は、
**Excel を触るのではなく JSON を直し、`jjf export xlsx` で作り直す**ことを意味する。
生成されたブックは入力ではない。読み取って設計を復元してはならず、ブックと JSON が
食い違っている場合、正しいのは常に JSON である。

## このガイドが対象とする作業

- リポジトリに `db-design.json`（`*.jjf.json` / `docs/db-design.json` 等の同形式ファイル）がある
- テーブルを追加する。カラムを追加・変更・削除する。型を変える。インデックスやキーを張る
- DB 設計書、DB スキーマ、Excel 設計書の更新を依頼された
- `jjf validate` が失敗し、文書を直す必要がある
- jjf 形式の DB 設計文書を新規に作る（ゼロから、または `pg_dump` の出力から）

## 権威ある入力

| 入力 | 位置づけ |
| --- | --- |
| `schema/db-design.schema.json` | **構造の正。** 必須プロパティ・型・enum・pattern はすべてここだけが決める |
| 英語スキルとその `references/` | **手順の正。** 作法・編集の順序・失敗時の直し方 |
| 既存の `db-design.json` | **現状の正。** 既存の命名・粒度・並び順に合わせる |
| ユーザーの要求 | **意図の正。** 曖昧なら推測で埋めず質問する |

生成された `.xlsx` は入力**ではない**。入力として読まない。

## ハードルール

1. `.xlsx` を直接編集しない。JSON を直してエクスポートし直す。
2. スキーマにないプロパティを追加しない。すべてのオブジェクトが `additionalProperties: false`
   なので、`comment` や `engine` のような独自プロパティは検証エラーになる。書きたい情報は
   `description` に入れる。
3. 同一オブジェクトに同名プロパティを二重に書かない。JSON の重複キーは `jjf` も
   JSON Schema も検出せず、**後に書いたほうが黙って勝つ。** 編集したオブジェクトは
   目で読み直すこと。
4. `type` にパラメータを含めない。`"VARCHAR(30)"` ではなく `"type": "VARCHAR"` と
   `"length": 30` に分ける。pattern が括弧を許可していない。
5. `default` の SQL 文字列リテラルは引用符込みで書く（`"default": "'pending'"`）。
   DEFAULT 句がないなら `default` キー自体を書かない。「既定値なし」を `"default": ""` で
   表さない。空の既定値も引用符のない語も `jjf validate` が警告する。
6. エラーを回避するために `formatVersion` を書き換えない。
7. 編集したら毎回 `jjf validate` を実行し、通ってから完了とする。
8. 依頼されたことだけ変更する。ついでの整形や並べ替えをしない。

## ワークフロー

1. 要求を確定させる。対象のテーブル・カラム・制約を決める。曖昧なら質問する。
2. 既存の `db-design.json` を読み、その作法に合わせる。
3. これから触るフィールドの許容値を
   [フィールドの作法と許容値](#フィールドの作法と許容値)で確認する。
4. JSON を変更する。編集の種類ごとの実例は[編集レシピ](#編集レシピ)にある。
5. `jjf validate <input.json>` を実行する。
6. **検証が失敗したら 4 に戻る。** 1 回の実行で違反が全件出るので、すべて直してから再実行する。
   失敗したままの文書で完了としてはならない。
7. ブックを作り直すなら `jjf export xlsx <input.json> -o <output.xlsx>` を実行する。
   ER 図が欲しいなら `jjf export dot <input.json> -o <output.dot>` を実行する。
   エクスポートは先に検証するので、検証を通らない文書からは 1 バイトも出力されない。
8. JSON の変更点を報告する。ブックを作り直していないなら、`.xlsx` の再生成が必要である
   ことも伝える。

## 文書構造の全体像

```text
db-design.json
├─ $schema         任意  エディタ補完用の参照。jjf は値を無視する
├─ formatVersion   必須  "1.0"（MAJOR.MINOR 形式の文字列）
├─ database        必須
│   ├─ name        必須  物理データベース名
│   ├─ logicalName 任意  論理名（任意の言語）
│   ├─ description 任意
│   └─ dbms        任意  enum、6 値
└─ tables          必須  1 件以上
    └─ [n]
        ├─ name        必須  物理テーブル名
        ├─ logicalName 必須  論理テーブル名
        ├─ description 任意
        ├─ columns     必須  1 件以上
        │   └─ [n]
        │       ├─ name          必須  物理カラム名
        │       ├─ logicalName   必須  論理カラム名
        │       ├─ type          必須  パラメータなしの型名
        │       ├─ nullable      必須  boolean、省略不可
        │       ├─ description   任意
        │       ├─ length        任意  integer >= 1
        │       ├─ precision     任意  integer >= 1
        │       ├─ scale         任意  integer >= 0、precision と併記必須
        │       ├─ default       任意  string、255 文字以内
        │       └─ autoIncrement 任意  boolean、既定 false
        ├─ primaryKey  任意  { name?, columns[] }
        ├─ uniqueKeys  任意  [ { name?, columns[] } ]
        ├─ foreignKeys 任意  [ { name?, columns[], references{ table, columns[] }, onUpdate?, onDelete? } ]
        └─ indexes     任意  [ { name, columns[], unique? } ]
```

必須プロパティ早見表:

| オブジェクト | 必須プロパティ |
| --- | --- |
| ルート | `formatVersion`, `database`, `tables` |
| `database` | `name` |
| `tables[]` | `name`, `logicalName`, `columns` |
| `columns[]` | `name`, `logicalName`, `type`, `nullable` |
| `primaryKey` | `columns` |
| `uniqueKeys[]` | `columns` |
| `foreignKeys[]` | `columns`, `references` |
| `references` | `table`, `columns` |
| `indexes[]` | `name`, `columns` |

`tables` と `columns` は 1 件以上必要である。カラム名を並べるリスト
（`primaryKey.columns`、`indexes[].columns` など）も同じく 1 件以上必要で、重複を許さない。

動く完全な文書の例は
[`examples/db-design.example.json`](https://github.com/shutx-net/jumping-json-flush/blob/main/examples/db-design.example.json) にある。
新規作成時はこれを雛形にする。

## フィールドの作法と許容値

全フィールドについて、何を受け付け、何を拒否し、どう書くのが作法か。
enum の許容値は、試行錯誤せず一発で正しく書けるように全値を列挙している。

### 識別子

`database.name` / `tables[].name` / `columns[].name` / `references.table`、および
すべての制約名・インデックス名に適用される。

- pattern `^[A-Za-z_][A-Za-z0-9_]*$`、1〜128 文字
- 半角英数とアンダースコアのみ。先頭に数字は置けない
- 拒否される: ハイフン (`order-lines`)、スキーマ修飾名 (`public.users`)、引用符付き識別子、
  非 ASCII の物理名。日本語などの非 ASCII 名は `logicalName` に書く
- 慣例: テーブルは複数形スネークケース (`order_items`)、カラムは単数形スネークケース
  (`unit_price`)、`pk_<table>`、`uq_<table>_<列>`、`fk_<table>_<参照先>`、`ix_<table>_<列>`。
  **既存文書に別の慣例があるならそちらに従う。**

### logicalName

- 自由文字列・任意の言語、1〜255 文字
- テーブルとカラムでは**必須**。`database` では任意
- 日本語などの非 ASCII の論理名を入れる場所

### description

- 自由記述、2000 文字まで。空文字列も許される
- 改行を含めてよい — Excel のセルは折り返し表示になる
- スキーマで表現できない情報はここに書く。業務ルール、単位、取り得る値の一覧、運用上の注意

### type

- pattern `^[A-Za-z][A-Za-z0-9_ ]*$`、1〜64 文字
- 半角英数・アンダースコア・半角スペースのみ。大文字で書く慣例
- 通る: `VARCHAR`, `BIGINT`, `NUMERIC`, `TIMESTAMP WITH TIME ZONE`, `DOUBLE PRECISION`,
  `VARCHAR2`, `DATETIME2`, `NVARCHAR`
- 通らない: `VARCHAR(30)` / `TIMESTAMP(3)`（括弧）、`INT[]`（角括弧）、
  `4BYTE_INT`（先頭が数字）、`CHARACTER-VARYING`（ハイフン）
- PostgreSQL の配列型のようにこの記法で表せない型は、`type` を要素型にして残りを
  `description` に明記する
- DBMS ごとの推奨表記は [DBMS ごとの推奨型名](#dbms-ごとの推奨型名) を参照

### length と precision と scale

- `length`: 文字列型・バイナリ型の宣言長。integer、1 以上
- `precision`: 数値型の全体桁数。integer、1 以上
- `scale`: 小数部の桁数。integer、0 以上。**`scale` を書くなら `precision` も必須** —
  `scale` 単独は構造エラーになる
- 使い分け: `VARCHAR` / `CHAR` / `VARBINARY` は `length`、`NUMERIC` / `DECIMAL` は
  `precision` と `scale`
- `INTEGER` / `BIGINT` / `TEXT` / `DATE` / `BOOLEAN` のように桁指定のない型は 3 つとも
  書かない。3 つとも無いのは正常な状態であり、埋めるべき欠落ではない

### nullable

- **boolean、省略不可。** `true` は NULL 可、`false` は NOT NULL
- 文字列 `"true"` は型エラーになる。クォートしない
- 主キーを構成するカラムは `false` にする。主キーのカラムが `nullable: true` だと
  `jjf validate` が警告する

### default

- **string のみ**、255 文字以内。数値も真偽値も文字列として書く
  （`"default": "0"`、`"default": "true"`）
- 値は **DEFAULT 句にそのまま入る SQL 式のテキスト**である。`jjf` は中身を評価しない。
  式として読めるかどうかだけを見る
- 文字列リテラルは引用符込み: `"default": "'pending'"`、空文字列なら `"default": "''"`
- 関数はそのまま: `"default": "CURRENT_TIMESTAMP"`、`"default": "now()"`、
  `"default": "gen_random_uuid()"`
- **DEFAULT 句がないなら `default` キー自体を書かない。** 完全に省略する
- `DEFAULT NULL` を明示したいときだけ `"default": "NULL"` と書く
- `"default": ""` は「空の SQL 式を既定値にする」という意味不明な指定になる。空文字列を
  既定値にしたいなら `"''"` と書く。`jjf validate` はこれを警告する
- 引用符のない語も警告する。`"default": "now"` は SQL では文字列 `now` ではなく
  **カラム参照**であり、PostgreSQL は "cannot use column reference in DEFAULT
  expression" で撥ねる。文字列なら `"'now'"`、関数なら `"now()"` と書く。
  `CURRENT_TIMESTAMP` のような keyword 定数はそのままでよい

### autoIncrement

- boolean、既定 `false`。`IDENTITY` / `AUTO_INCREMENT` / `SERIAL` およびシーケンス採番を表す
- `false` なら書かない
- 採番方法（`SERIAL` か `GENERATED BY DEFAULT AS IDENTITY` か等）を残したいなら
  `description` に書く

### dbms

許容値はこの 6 つだけである。

```text
"PostgreSQL"  "MySQL"  "MariaDB"  "SQLite"  "Oracle"  "SQLServer"
```

大文字小文字を含めて完全一致。`"postgres"` / `"MSSQL"` / `"SQL Server"` はすべて検証エラーになる。

### onUpdate と onDelete

許容値はこの 5 つだけである。

```text
"CASCADE"  "RESTRICT"  "SET NULL"  "SET DEFAULT"  "NO ACTION"
```

すべて大文字。`SET NULL` と `SET DEFAULT` は**半角スペース 1 つ**で、アンダースコアではない。
省略した場合は句を出力しないという意味になり、DBMS の既定動作に任せる。

### indexes の unique

- boolean、既定 `false`。ユニークインデックスなら `true`
- **ユニーク制約**は `indexes` に `unique: true` と書くのではなく `uniqueKeys` に書く。
  両方に同じ内容を書かない

### キーとインデックスの必須プロパティ

| オブジェクト | 必須 | 任意 |
| --- | --- | --- |
| `primaryKey` | `columns` | `name` |
| `uniqueKeys[]` | `columns` | `name` |
| `foreignKeys[]` | `columns`, `references` | `name`, `onUpdate`, `onDelete` |
| `references` | `table`, `columns` | — |
| `indexes[]` | `name`, `columns` | `unique` |

名前が**必須**なのは `indexes[].name` だけである。他は DBMS の自動命名に任せてもよいが、
設計書として読ませるなら書いたほうがよい。

`columns` のリストはいずれも 1 件以上必要で、同じ名前を 2 回並べられない。外部キーでは
`columns` と `references.columns` の**件数と順序を対応させる** — 複合キーは並び順が意味を持つ。

## DBMS ごとの推奨型名

`type` はスキーマ上は自由文字列だが、**表記の揺れは設計書の品質を直接下げる**。
`database.dbms` に対応する列からそのまま選び、既存文書が別の表記をしているならそれに合わせる。

| 用途 | PostgreSQL | MySQL / MariaDB | SQLite | Oracle | SQLServer |
| --- | --- | --- | --- | --- | --- |
| 可変長文字列 | `VARCHAR` | `VARCHAR` | `TEXT` | `VARCHAR2` | `NVARCHAR` |
| 固定長文字列 | `CHAR` | `CHAR` | `TEXT` | `CHAR` | `NCHAR` |
| 長文 | `TEXT` | `TEXT` | `TEXT` | `CLOB` | `NVARCHAR MAX` |
| 真偽値 | `BOOLEAN` | `TINYINT` | `INTEGER` | `NUMBER` | `BIT` |
| 小整数 | `SMALLINT` | `SMALLINT` | `INTEGER` | `NUMBER` | `SMALLINT` |
| 整数 | `INTEGER` | `INT` | `INTEGER` | `NUMBER` | `INT` |
| 大整数 | `BIGINT` | `BIGINT` | `INTEGER` | `NUMBER` | `BIGINT` |
| 固定小数 | `NUMERIC` | `DECIMAL` | `NUMERIC` | `NUMBER` | `DECIMAL` |
| 浮動小数 | `DOUBLE PRECISION` | `DOUBLE` | `REAL` | `BINARY_DOUBLE` | `FLOAT` |
| 日付 | `DATE` | `DATE` | `TEXT` | `DATE` | `DATE` |
| 日時 | `TIMESTAMP` | `DATETIME` | `TEXT` | `TIMESTAMP` | `DATETIME2` |
| 日時（TZ 付き） | `TIMESTAMP WITH TIME ZONE` | `TIMESTAMP` | `TEXT` | `TIMESTAMP WITH TIME ZONE` | `DATETIMEOFFSET` |
| バイナリ | `BYTEA` | `VARBINARY` | `BLOB` | `BLOB` | `VARBINARY` |
| UUID | `UUID` | `CHAR` | `TEXT` | `RAW` | `UNIQUEIDENTIFIER` |
| JSON | `JSONB` | `JSON` | `TEXT` | `CLOB` | `NVARCHAR MAX` |

補足:

- `NVARCHAR MAX` は、pattern が禁じている括弧を外して `NVARCHAR(MAX)` を表した綴りである。
  DDL 化する際に括弧を戻せるよう、`description` に「MAX 長」と書いておく。
- SQLite は型そのものではなく型親和性しか持たないため、`TEXT` / `INTEGER` / `REAL` /
  `BLOB` / `NUMERIC` に寄せる。
- MySQL の `TINYINT` を真偽値に使う場合は `"length": 1` を添える慣例がある。
- Oracle の `NUMBER` は `precision` と `scale` で用途を表す — 整数なら `"scale": 0`。

## 編集レシピ

編集の種類ごとの実例 JSON と、文書を変更するのではなく作り始める唯一のレシピ。
いずれのレシピも終わり方は同じである —
`jjf validate <input.json>` を実行し、通ってから手を止める。

### PostgreSQL のダンプから初期化する

データベースが既にあるなら手で書き写さない。スキーマをダンプして取り込む。

```sh
pg_dump --schema-only mydb > schema.sql
jjf import postgres schema.sql -o db-design.json
```

入力は**ファイル**であって接続ではない。`jjf` がサーバと通信することはない。
`pg_dump --schema-only` の出力であれば、メジャー 13 から 18 まで扱える。

結果について注意すべき点が 2 つある。

**論理名は仮置きである。** `logicalName` は全テーブル・全カラムで必須だが、ダンプに
そんなものは無い。`jjf` は `COMMENT ON` があればそこから埋め、無ければ物理名をそのまま
複製する。つまり `"logicalName": "created_at"` は「論理名が created_at である」ではなく
「まだ誰も書いていない」を意味する。取り込み後に最初にやる編集は、これを実際の名前に
置き換えることである。

**警告はノイズではない。** 設計形式が保持できないもの — CHECK 制約、部分インデックスの
述語、`INCLUDE` カラム、排他制約、生成カラム — は標準エラーに報告された上で落とされる。
つまり文書はデータベース本体より狭い記述になる。JSON を完全なものとして扱う前にすべての
警告を読み、何が失われたかを伝えること。

使えるフラグ:

| フラグ | 効果 |
| --- | --- |
| `-schema <name>` | `public` 以外のスキーマを取り込む。1 文書 1 スキーマ。識別子に `schema.` の接頭辞は持てない |
| `-database <name>` | `database.name` を明示する。既定は入力ファイル名から導出される |
| `-strict` | すべての警告をエラーにする。黙って何かが落ちることがなくなる |

取り込みに失敗した場合、ファイルは一切書かれない — 組み立てた文書を検証してから
ディスクに触るため、書きかけの文書が残ることはない。

### テーブルを追加する

`tables` の末尾に追記する。既存要素を並べ替えない — 配列順がそのままブックのシート順になる。

```json
{
  "name": "shipments",
  "logicalName": "出荷",
  "description": "受注に対する出荷実績。1 受注につき複数回の出荷を許す。",
  "columns": [
    {
      "name": "id",
      "logicalName": "出荷ID",
      "type": "BIGINT",
      "nullable": false,
      "autoIncrement": true
    },
    {
      "name": "order_id",
      "logicalName": "受注ID",
      "type": "BIGINT",
      "nullable": false
    },
    {
      "name": "shipped_at",
      "logicalName": "出荷日時",
      "type": "TIMESTAMP WITH TIME ZONE",
      "nullable": false,
      "default": "CURRENT_TIMESTAMP"
    }
  ],
  "primaryKey": {
    "name": "pk_shipments",
    "columns": ["id"]
  },
  "foreignKeys": [
    {
      "name": "fk_shipments_order",
      "columns": ["order_id"],
      "references": {
        "table": "orders",
        "columns": ["id"]
      },
      "onUpdate": "CASCADE",
      "onDelete": "CASCADE"
    }
  ],
  "indexes": [
    {
      "name": "ix_shipments_order_id",
      "columns": ["order_id"]
    }
  ]
}
```

チェック: `name` / `logicalName` と 1 件以上のカラムが揃っているか。
各カラムに `nullable` があるか。

### カラムを追加する

対象テーブルの `columns` の**意図した位置**に挿入する — 配列順がそのまま行順になる。
末尾追加が既定。監査系カラム（`created_at` / `updated_at`）の前に業務カラムを入れるのが慣例。

```json
{
  "name": "cancelled_at",
  "logicalName": "キャンセル日時",
  "description": "キャンセルされていない受注は NULL。",
  "type": "TIMESTAMP WITH TIME ZONE",
  "nullable": true
}
```

既存行が入っているテーブルに NOT NULL カラムを足すなら、既定値も同時に決める。

```json
{
  "name": "channel",
  "logicalName": "受注チャネル",
  "description": "web / phone / store のいずれか。",
  "type": "VARCHAR",
  "length": 20,
  "nullable": false,
  "default": "'web'"
}
```

### カラムを変更する

**変更していいのは依頼で指定されたプロパティだけ。** ついでの整形や並べ替えをしない。

- 桁を広げる: `length` の数値だけを変える
- 数値型に変える: `type` を変え、`length` を消して `precision` と `scale` を入れる

```json
{
  "name": "unit_price",
  "logicalName": "単価",
  "type": "NUMERIC",
  "precision": 12,
  "scale": 2,
  "nullable": false,
  "default": "0"
}
```

- NOT NULL にする: `nullable` を `false` にする。そのカラムが `SET NULL` 外部キーの
  NULL 側になっていないかを先に確認し、既存の NULL 行をどう扱うかを `description` に書く
- 既定値を外す: **`default` キーを削除する。** `""` にしない。`""` は `jjf validate` が
  警告する

### カラムを削除する

`columns` から要素を消したら、**そのカラム名を参照している箇所をすべて消す。**
消し漏れは `jjf validate` が警告するが、警告は終了コードを 0 のままにするので、
`-strict` を付けない限り検証自体は通る。

確認する場所:

- `primaryKey.columns`
- `uniqueKeys[].columns`
- `foreignKeys[].columns`
- `references.table` が自テーブルである他テーブルの `foreignKeys[].references.columns`
- `indexes[].columns`

`columns` が空になる場合は、そのテーブルごと削除する — カラムは 1 件が下限である。
`primaryKey.columns` が空になる場合は `primaryKey` オブジェクトごと削除する。

### 外部キーを張る

参照元テーブルの `foreignKeys` に追記する。参照先の列は原則そのテーブルの主キーか
ユニークキーである。

```json
{
  "name": "fk_order_items_product",
  "columns": ["product_id"],
  "references": {
    "table": "products",
    "columns": ["id"]
  },
  "onUpdate": "CASCADE",
  "onDelete": "RESTRICT"
}
```

- `columns` と `references.columns` は**件数と順序を対応させる** — 複合キーは並び順が
  意味を持つ
- 参照元カラムの `type` は参照先と一致させる
- 参照元カラムが未定義なら、先にそのカラムを追加する
- 外部キー列には索引も張るのが定石である。`indexes` に `ix_<table>_<列>` を足す
- `onDelete` の選び方: 親を消したら子も消す＝`CASCADE`、子がある限り親を消させない＝
  `RESTRICT`、子の参照を NULL に落とす＝`SET NULL`（当該カラムが `"nullable": true`
  でなければ成立しない）

### インデックスを追加する

```json
{
  "name": "ix_orders_status_ordered_at",
  "columns": ["status", "ordered_at"],
  "unique": false
}
```

- `name` は**必須**
- 複合インデックスは**絞り込みの効く列を先に**並べる
- ユニーク制約として表現したいなら、`unique: true` のインデックスではなく `uniqueKeys` を使う

### ユニークキーを追加する

```json
{
  "name": "uq_users_email",
  "columns": ["email"]
}
```

`name` は任意 — DBMS が自動命名する — が、読ませるための設計書なら付ける。

### 主キーを設定する

```json
{
  "name": "pk_order_items",
  "columns": ["order_id", "line_no"]
}
```

`columns` の順序がそのままキー順になる。構成カラムはすべて `"nullable": false` にする。

## コマンドと終了コード

| コマンド | 動作 |
| --- | --- |
| `jjf import postgres schema.sql -o db-design.json` | `pg_dump --schema-only` の出力から文書を組み立てる。書き出す前に検証する |
| `jjf validate db-design.json` | 検証し、`db-design.json: OK` を出力する |
| `jjf export xlsx db-design.json -o db-design.xlsx` | 検証してからブックを書き、`db-design.xlsx: written` を出力する |
| `jjf export xlsx db-design.json` | 同じ。入力の隣に、拡張子を置き換えた名前で出力する |
| `jjf export xlsx db-design.json -o -` | 標準出力へ書く。`xlsx` はバイナリなので端末に直接出そうとした場合は拒否される |
| `jjf export dot db-design.json -o er.dot` | 検証してから Graphviz DOT のソースを書く。画像化は各自の `dot` で行う |
| `jjf version` | ツールのバージョンを出力する |

成功メッセージは標準出力、エラーと usage は標準エラーに出る。

| コード | 意味 | 対処 |
| --- | --- | --- |
| 0 | 成功 | — |
| 1 | 一般エラー | 内部エラーとして報告する |
| 2 | 入力不正 | コマンドライン、パス、JSON 構文、`jjf` のバージョンを見直す |
| 3 | **スキーマ検証エラー** | JSON の中身を直す |
| 4 | 出力生成エラー | 出力先ディレクトリを作る、または権限を直す |

**判断の分かれ目は 3 と 2 である。** 3 なら文書の中身が誤っており、2 なら呼び出し方か
環境が誤っている。

## 検証エラーと直し方

`jjf` が実際に出力するメッセージと、その原因・直し方の対応。以下のメッセージはすべて
ツールの出力からそのまま引用している。

### 出力の読み方

スキーマ違反の報告は `<入力パス>: does not conform ...` で始まり、違反を 1 行ずつ
JSON Pointer とメッセージで並べ、件数で終わる。**Pointer が直す場所を正確に指している。**
`/tables/0/columns/1/name` は「1 番目のテーブルの 2 番目のカラムの `name`」 —
**添字は 0 始まり**である。

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

違反は 1 回の実行で全件報告される。すべて直してから再実行する。
文書のルート自体は `(document root)` と表示される。

### スキーマ違反（終了コード 3）

| メッセージ | 原因 | 直し方 |
| --- | --- | --- |
| `missing property 'nullable'` | カラムに `nullable` がない | `"nullable": true` または `false` を追加する。省略は許されない |
| `missing property 'logicalName'` | テーブルまたはカラムに論理名がない | 任意の言語で追加する |
| `missing property 'name'` | インデックスに名前がない | `indexes[]` は `name` 必須。`ix_<table>_<列>` を付ける |
| `got string, want boolean` | `"nullable": "true"` とクォートした | クォートを外して `true` / `false` にする |
| `got string, want integer` | `"length": "30"` とクォートした | クォートを外して `30` にする |
| `value must be one of 'PostgreSQL', 'MySQL', 'MariaDB', 'SQLite', 'Oracle', 'SQLServer'` | `dbms` の綴り違い | 6 値から完全一致で選ぶ |
| `value must be one of 'CASCADE', 'RESTRICT', 'SET NULL', 'SET DEFAULT', 'NO ACTION'` | `onUpdate` / `onDelete` の綴り違い | 5 値から選ぶ。`SET NULL` は半角スペース区切り |
| `'order-lines' does not match pattern '^[A-Za-z_][A-Za-z0-9_]*$'` | 識別子にハイフン・ドット・非 ASCII 文字・先頭数字がある | 半角英数とアンダースコアに直し、読ませたい名前は `logicalName` へ移す |
| `'VARCHAR(30)' does not match pattern '^[A-Za-z][A-Za-z0-9_ ]*$'` | `type` にパラメータを埋めた | `"type": "VARCHAR", "length": 30` に分ける |
| `'1' does not match pattern '^[0-9]+\.[0-9]+$'` | `formatVersion` が `MAJOR.MINOR` でない | `"1.0"` と書く |
| `additional properties 'engine' not allowed` | スキーマにないプロパティをテーブルに足した | 削除し、内容を `description` に移す |
| `additional properties 'comment' not allowed` | 同じことをカラムでやった | `description` を使う |
| `minLength: got 0, want 1` | 識別子または `logicalName` が空文字列 | 値を入れる。空文字列が許されるのは `description` だけ |
| `minItems: got 0, want 1` | `"columns": []` のような空配列 | 1 件以上入れる。空になるなら親ごと削除する |
| `items at 0 and 1 are equal` | 1 つのキーに同じカラム名が 2 回並んでいる | 重複を削る。リストは要素の一意性を要求する |
| `properties 'precision' required, if 'scale' exists` | `scale` だけ書いた | `precision` も書く、または `scale` を消す |
| `got array, want object` | 文書のルートが配列になっている | ルートは `{ }` オブジェクトである |

### 検証以前に落ちるもの（終了コード 2）

| 出力 | 原因 | 直し方 |
| --- | --- | --- |
| `jjf: db-design.json: line 5, column 4: invalid character '}' looking for beginning of object key string` | JSON 構文エラー（末尾カンマなど） | 指摘された行・桁を直す |
| `jjf: open db-design.json: no such file or directory` | パスの誤り | パスを確認する |
| `jjf: unsupported formatVersion "2.0"; this jjf supports 1.x - please upgrade jjf` | この `jjf` より新しいフォーマットの文書 | `jjf` を更新する。**JSON を書き換えて回避しない** |
| `jjf: unsupported format "csv"; supported formats: xlsx, dot` | 存在しない出力形式 | 形式は `xlsx` と `dot` |
| `jjf: validate takes exactly one input file, got 0` | 入力パスを渡していない | パスを渡す |
| `jjf: refusing to write a workbook to the terminal; redirect standard output or pass -o <file>` | 標準出力が端末の状態で `-o -` を使った | リダイレクトする、またはファイルパスを渡す |

### 出力生成の失敗（終了コード 4）

| 出力 | 原因 | 直し方 |
| --- | --- | --- |
| `jjf: cannot create output file: /nonexistent-dir/x.xlsx: no such file or directory` | 出力先ディレクトリが存在しない | ディレクトリを作る、または別のパスを選ぶ |

### 文字エンコーディング

`jjf` は UTF-8 BOM 付きでも受け付けるが、他のツールが落ちる。
文書は **BOM なし UTF-8・LF 改行**で保存すること。

## 生成される Excel の読み方

`jjf export xlsx` が組むブックの体裁と、セルの読み方。体裁は `jjf` が固定で持っており
JSON 側から制御できない。「Excel の見た目を変えたい」という要求は文書の編集では実現できないので、
そう伝えること。

### シート

順序は **表紙 → テーブル一覧 → テーブル定義**（1 テーブル 1 シート、`tables` の順）。

表紙は常に出力され、**生成日時を含まない**（CLI は生成日時をレンダラに渡さない）。
そのため同じ入力からは常にバイト同一の `.xlsx` が出る。これは意図的で、差分レビューと
CI での比較を可能にするためである。

| シート | 内容 |
| --- | --- |
| 表紙 | データベース名, 論理名, DBMS, テーブル数, フォーマットバージョン, 説明 |
| テーブル一覧 | No, 物理テーブル名, 論理テーブル名, 説明, カラム数, シート名 |
| テーブル定義 | カラム表、その下に制約種別ごとのブロック |

任意の値が無い場合は行を落とすのではなく空セルになるので、表紙はどの文書でも同じ形になる。

### カラム表

見出しは左から `No`, `物理カラム名`, `論理カラム名`, `型`, `長さ`, `NULL`, `既定値`,
`自動採番`, `説明`。

- `NULL` 列と `自動採番` 列は、該当を **`○`**、非該当を**空セル**で表す
- `長さ` 列は `length` があればその値、なければ `precision`、両方あれば `precision,scale`
  （例 `10,2`）。3 つとも無ければ空
- `既定値` 列は、似て見えるが違う 2 つの状態を書き分ける。`default` に空文字列を書いた場合は
  **空文字列のセル**、**`default` キー自体が無い**場合は**空白セル**になる。前者は書いて
  よい指定ではなく `jjf validate` が警告する間違いで、両方を空白セルにすると著者が読む
  成果物からその間違いが消えてしまうため、workbook は書かれたとおりに書き分ける。SQL の
  空文字列を既定値にしたいなら `"''"` と書く
- `説明` 列は折り返し表示になる

### 制約ブロック

カラム表の下に、この順で出力される。**文書が定義していない制約はブロック自体が出力されない。**

| ブロック | 列 |
| --- | --- |
| 主キー (PRIMARY KEY) | 制約名, 対象カラム |
| ユニークキー (UNIQUE) | 制約名, 対象カラム |
| 外部キー (FOREIGN KEY) | 制約名, 対象カラム, 参照先テーブル, 参照先カラム, ON UPDATE, ON DELETE |
| インデックス (INDEX) | インデックス名, 対象カラム, ユニーク |

複数カラムのリストは文書の順序を保ったまま `, ` で連結され、1 セルに収まる。
名前のない制約は名前セルが空白になる。`onUpdate` / `onDelete` を省略した場合もセルは空白で、
句を出力しないという意味になる。

### シート名

テーブルのシートはテーブル名で命名されるが、Excel の規則に従う。

- **31 文字**に切り詰められる
- 衝突したら `(2)` `(3)` … が付き、その分だけ基底名が削られる。Excel はシート名の
  大文字小文字を区別しないので、`Users` と `users` も衝突する
- **テーブル一覧の `シート名` 列には実際に割り当てられた名前が表示される。** 切り詰めや
  採番が起きたかどうかはそこで分かる

長いテーブル名でも `物理テーブル名` 列には全体が残るので、シートタブが切れていても読める。

### 変更できないもの

レイアウト、配色、フォント、列幅、行高、シートの見出し、見出し文言の言語はすべて `jjf` が
持っている。テンプレートもテーマ指定も無く、これらに影響する JSON のプロパティも存在しない。

## formatVersion の扱い

`formatVersion` は DB 設計 JSON フォーマットのバージョンであり、`jjf` 本体のバージョンとは
独立している。現在の値は `"1.0"`、形式は `MAJOR.MINOR` である。

新規文書には `"1.0"` と書き、既存文書を編集するときは既存の値をそのまま残す。
**自分で書き換えてはならない。** 上げるのはメンテナだけであり、フォーマット自体が非互換に
変わったときだけである。`unsupported formatVersion "2.0"` と言われたら、文書を書き換えて
回避せず `jjf` を更新する。

## 適用範囲外

`jjf` は次のことをしない。要求されたら「できない」と伝え、それらしい JSON を捏造しない。

- **設計の良し悪しの判断。** 正規化・インデックス設計・型選択・命名規約はいずれも
  書く側の責任である。外部キー両端の型互換性、テーブル名の文書内での重複も未検査である
  （文書が自分自身と矛盾していないかは `jjf validate` が検査し、警告として報告する。
  [jjf の使い方](usage.ja.md#参照整合性の検査) を見よ）
- DDL / SQL の生成
- 実データベースへの接続。スキーマの取り込みは `pg_dump` の**ファイル**からのみで、稼働中のサーバには接続しない
- マイグレーション管理、スキーマ差分、破壊的変更の検出
- Mermaid / Markdown の出力。ER 図は Graphviz DOT のソースを出力するだけで、画像への変換は行わない
- Excel から JSON への逆変換、Excel の直接編集
- ブックのレイアウト・配色・テンプレートのカスタマイズ
- スキーマにないプロパティによる文書の拡張

## 関連ドキュメント

- [`README.ja.md`](https://github.com/shutx-net/jumping-json-flush/blob/main/README.ja.md) — インストール、CLI の使い方、CI への組み込み
- [`schema/db-design.schema.json`](https://github.com/shutx-net/jumping-json-flush/blob/main/schema/db-design.schema.json) — 構造の正式定義
- [`examples/db-design.example.json`](https://github.com/shutx-net/jumping-json-flush/blob/main/examples/db-design.example.json) — 完全な設計 JSON の例
- [`skills/db-design/SKILL.md`](https://github.com/shutx-net/jumping-json-flush/blob/main/skills/db-design/SKILL.md) — AI エージェントが読む英語スキル（内容の正）
- [`skills/README.ja.md`](https://github.com/shutx-net/jumping-json-flush/blob/main/skills/README.ja.md) — スキルの導入方法
