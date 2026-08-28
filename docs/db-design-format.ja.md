# DB 設計 JSON の形式

[README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.ja.md) · [English](db-design-format.md)

完全な例は [`examples/db-design.example.json`](https://github.com/shutx-net/jumping-json-flush/blob/main/examples/db-design.example.json)、
構造の正式な定義は [`schema/db-design.schema.json`](https://github.com/shutx-net/jumping-json-flush/blob/main/schema/db-design.schema.json)。

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
| 既定値 | `default` は DEFAULT 句にそのまま入る SQL 式のテキスト。文字列の既定値は SQL の引用符込み（`"'pending'"`）。DEFAULT 句なしはキー自体を書かない。空の `""` は警告 |
| enum | `dbms`: `PostgreSQL`, `MySQL`, `MariaDB`, `SQLite`, `Oracle`, `SQLServer`。`onUpdate` / `onDelete`: `CASCADE`, `RESTRICT`, `SET NULL`, `SET DEFAULT`, `NO ACTION` |

`dbms` は `jjf export ddl` を除くすべてのコマンドにとって説明的な値であり、
`jjf export ddl` だけがこれを必須とし、名乗られた方言で書く。書けるのは
`PostgreSQL` と `MySQL` の 2 つで、残る 4 つは終了コード 2 で拒否される。
他に分岐するものは無い。

### import 時の PostgreSQL 型の扱い

`jjf import postgres` は PostgreSQL の型を、型名と、スキーマがその隣に持つ数値属性へ
分解する。**`varchar(255)` は長さ、`timestamp(3)` は小数秒の精度**であり、これを
取り違えると別の列になる。**`TIMESTAMP` と `TIMESTAMPTZ` は決して同一視しない。**
両者はデータの意味そのものが違うためである。

| PostgreSQL | `type` | パラメータ |
| --- | --- | --- |
| `character varying`, `varchar`, `char varying`, `national character varying`, `national char varying` | `VARCHAR` | `length` |
| `character`, `char`, `bpchar`, `national character`, `national char` | `CHAR` | `length` |
| `bit` | `BIT` | `length` |
| `bit varying`, `varbit` | `BIT VARYING` | `length` |
| `numeric`, `decimal` | `NUMERIC` | `precision`, `scale` |
| `timestamp without time zone`, `timestamp` | `TIMESTAMP` | `precision` |
| `timestamp with time zone`, `timestamptz` | `TIMESTAMPTZ` | `precision` |
| `time without time zone`, `time` | `TIME` | `precision` |
| `time with time zone`, `timetz` | `TIMETZ` | `precision` |
| `interval`, `interval <fields>` | `INTERVAL` | `precision` |
| `integer`, `int`, `int4` | `INTEGER` | — |
| `bigint`, `int8` | `BIGINT` | — |
| `smallint`, `int2` | `SMALLINT` | — |
| `boolean`, `bool` | `BOOLEAN` | — |
| `double precision`, `float8`, `float` | `DOUBLE PRECISION` | — |
| `real`, `float4` | `REAL` | — |
| `serial`, `bigserial`, `smallserial` | `INTEGER`, `BIGINT`, `SMALLINT` | `autoIncrement: true`。文が言っていなくても `nullable: false` になる。PostgreSQL に nullable な identity 列は存在せず、`GENERATED ... AS IDENTITY` と書いた列も同じである。`DEFAULT nextval(...)` だけの列はこれにあたらず、nullable のままになる |
| `text`, `bytea`, `uuid`, `json`, `jsonb`, `date`, `money`, `inet` ほか | 同じ名前の大文字 | — |
| 配列（`text[]`, `character varying(30)[]`） | `TEXT ARRAY`, `VARCHAR ARRAY` | 要素のものを引き継ぐ |

ユーザー定義型や enum は、pg_dump が付ける `public.` を落とした大文字の名前になる。
`interval day to second` のフィールド修飾子や PostGIS 型の引数のように、書き場所が
無いパラメータは警告を出して捨てる。

取り込まない対象を含む import の詳細は [jjf の使い方](usage.ja.md#import) にある。

### import 時の MySQL 型の扱い

`jjf import mysql` も同じように分解し、同じ一文が同じだけの時間を節約する。
**`varchar(255)` は長さ、`datetime(3)` は小数秒の精度**である。MySQL 固有の規則が
2 つある。**`UNSIGNED` と `ZEROFILL` は `type` 名の一部として残す。** スキーマの型
パターンは空白を許し、どちらの属性も列が保持しうる値の範囲を変えるためである。
そして **`tinyint(1)` は `TINYINT` の `length: 1` のままとし、`BOOLEAN` にはしない。**
MySQL は真偽値をそう格納し、`mysqldump` もそう書き戻すので、`BOOLEAN` と名付ければ
ダンプが言っていない型を文書に書くことになり、書き出した先のデータベースはまた
`tinyint(1)` をダンプするからである。

| MySQL | `type` | パラメータ |
| --- | --- | --- |
| `varchar`, `character varying`, `char varying`, `nchar varying`, `national varchar`, `national character varying`, `national char varying` | `VARCHAR` | `length` |
| `char`, `character`, `national char`, `national character` | `CHAR` | `length` |
| `binary` | `BINARY` | `length` |
| `varbinary` | `VARBINARY` | `length` |
| `bit` | `BIT` | `length` |
| `tinyint` | `TINYINT` | `length` |
| `decimal`, `numeric`, `dec`, `fixed` | `DECIMAL` | `precision`, `scale` |
| `datetime` | `DATETIME` | `precision` |
| `timestamp` | `TIMESTAMP` | `precision` |
| `time` | `TIME` | `precision` |
| `int`, `integer` | `INTEGER` | — |
| `smallint`, `mediumint`, `bigint` | `SMALLINT`, `MEDIUMINT`, `BIGINT` | — |
| `float` | `FLOAT` | — |
| `double`, `double precision`, `real` | `DOUBLE` | — |
| `bool`, `boolean` | `BOOLEAN` | — |
| `serial`、および属性として書く `SERIAL DEFAULT VALUE` | 型として書けば `BIGINT UNSIGNED`、属性として書けばその列自身の型 | `nullable: false`、`autoIncrement: true`、および MySQL が作るユニークキー。サーバが展開するので取り込み側も展開する ── `SHOW CREATE TABLE` は 4 つとも報告し、`mysqldump` はそれを書き戻す |
| `date`, `year`, `json` | 同じ名前の大文字 | — |
| `tinytext`, `text`, `mediumtext`, `longtext` | 同じ名前の大文字 | — |
| `tinyblob`, `blob`, `mediumblob`, `longblob` | 同じ名前の大文字 | — |
| `long varchar`, `long varbinary` | `MEDIUMTEXT`, `MEDIUMBLOB` | — |
| `enum('a','b')`, `set('a','b')` | `ENUM`, `SET` | 値リストは捨てる |
| `bigint unsigned`, `decimal(10,2) unsigned`, `int unsigned zerofill` | `BIGINT UNSIGNED`, `DECIMAL UNSIGNED`, `INTEGER UNSIGNED ZEROFILL` | 基底の型のものを引き継ぐ |
| 表に無い型（geometry 系、将来の MySQL が足すもの） | 同じ名前の大文字 | — |

`ENUM` と `SET` の値リストは**それを名指しする警告とともに捨てる**。フォーマットに
置き場所が無いためである。残る `type` は、PostgreSQL の enum とまったく同じ意味で
不完全であることに正直である。`jjf export ddl` は型名をそのまま書き戻し、MySQL は
値リストの無い `ENUM` をスクリプトの構文解析の時点で拒否する。

整数の表示幅は**警告とともに捨てる**。MySQL 8.0.17 以降は非推奨で `mysqldump` も
書かなくなったため、`INT(11)` と書き戻せばサーバが取り除こうとしている構文を出す
ことになる。`TINYINT` だけが例外で、理由は上の `tinyint(1)` である。

`$schema` をルートに書いておくと、VS Code などのエディタで補完と警告が効く。
`jjf` はこの値を読まない。

**`jjf validate` は構造検証に加えて、文書が自分自身と矛盾していないかを検査する。**
キーとインデックスが指すカラムの存在、外部キーの参照先テーブルが定義されていること、
列数が一致すること、参照先の列が一意（主キー・ユニークキー・ユニークインデックスの
いずれか）であること、主キーのカラムが `nullable: true` でないこと、同一テーブル内で
カラム名・制約名が重複しないこと、既定値が空でなく SQL 式として読めることの 8 点である。
検出結果は警告として標準エラーに出力し、
終了コードは 0 のままである。`-strict` を付けたときだけ終了コード 2 で失敗する。
詳細は [参照整合性の検査](usage.ja.md#参照整合性の検査) にある。

**設計の良し悪しは判断しない。** 正規化・インデックス設計・型選択・命名規約、および
外部キー両端の型互換性は書く側の責任である。テーブル名の文書内での重複（同一テーブル内の
カラム名の重複は検査する）、インデックス名のスキーマ全体での一意性も検査しない。

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
