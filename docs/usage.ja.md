# jjf の使い方

[README](https://github.com/shutx-net/jumping-json-flush/blob/main/README.ja.md) · [English](usage.md)

## validate

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

## export

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

## import

```sh
pg_dump --schema-only mydb > schema.sql
jjf import postgres schema.sql -o db-design.json
```

PostgreSQL のスキーマダンプから設計文書を組み立てる。入力は**ファイル**であり、
`jjf` がデータベースへ接続することはない。dialect は `postgres` のみ。

- 生成した文書は**書き出す前にスキーマで検証する**。`jjf validate` が拒否するような
  文書が import から出てくることはない
- `-o` を省略すると、**入力パスの拡張子を `.json` に置き換えた場所**へ出力する
  （`schema.sql` → `schema.json`）
- `-o -` で標準出力へ書き出せる。`export` と違い端末でも拒否しない。JSON は読める
  テキストだからである
- `-schema` は取り込む PostgreSQL スキーマを選ぶ（既定は `public`）。設計文書には
  スキーマ修飾の置き場所が無いため、一度に取り込めるのは 1 スキーマだけで、
  それ以外は捨てられる
- `-database` は生成する文書のデータベース名を決める。省略した場合、ダンプに
  `\connect` 行があればそこから、無ければ入力ファイル名から採る（この場合、
  ファイル名自体が識別子として妥当である必要がある）
- `-strict` はすべての警告をエラーに変える。このとき出力は書かれない
- 想定しているのは **pg_dump 13 〜 17** の出力である。ヘッダのバージョン表記を読み、
  範囲外のダンプは失敗ではなく警告にする

### ダンプについて jjf が言うこと

3 段階ある。どれになるかは SQL の珍しさではなく、設計フォーマットに書き場所が
あるかどうかで決まる。

| 段階 | 例 | 挙動 |
| --- | --- | --- |
| 黙って読み飛ばす | `SET`, `GRANT`, `CREATE VIEW`, `CREATE FUNCTION`, `OWNER TO` | 何も出さない。ダンプはこれらで埋まっており、いちいち警告すると本当に必要な警告が埋もれるため |
| 警告する | `CHECK` 制約、部分索引・式索引、`INCLUDE`、btree 以外のアクセスメソッド、`DEFERRABLE`、`INHERITS`、生成列 | 標準エラーへ行番号付きで 1 行出す。**周囲のテーブルや索引はそのまま取り込む** |
| エラーにする | 構文として壊れた SQL、書けない識別子、同名テーブルの二重定義 | 終了コード 2。何も書かれない |

```text
$ jjf import postgres schema.sql -o db-design.json
schema.sql:14: warning: constraint users_email_check on table public.users: check constraint is not imported
schema.sql:20: warning: index users_email_live_idx on table public.users: partial index predicate is not imported
schema.sql:22: warning: index users_doc_idx on table public.users: access method gin is not imported; recorded as a plain index
db-design.json: written
```

`file:line: warning:` という形は、エディタや CI のアノテータがそのまま解釈できる。
警告は標準エラー、成功メッセージは標準出力に出る。

書けない識別子は**黙って改名せずエラーにする**。`"user-profiles"` というテーブルは
`user_profiles` に化けるのではなく import を止める。改名された文書は一見正しく見えて、
存在しないデータベースを説明してしまうためである。例外は制約名で、スキーマ上
省略可能なので、書けない名前は警告とともに落とし、制約自体は名前なしで取り込む。

### logicalName と description

スキーマはすべてのテーブルとカラムに `logicalName` を要求するが、ダンプにはそれが無い。
そこで次のようにする。

- `COMMENT ON` の**1 行目**を `logicalName` にする
- **2 行目以降**を `description` にする
- コメントが**無い**テーブル・カラムには物理名をそのまま `logicalName` として置く

最後の規則は答えではなく、編集の出発点である。生成された文書は開いて本当の名前を
与えるためにある。

### 取り込まないもの

ビュー、マテリアライズドビュー、関数、トリガ、列の型として使われた enum の名前を
超える型定義、拡張、パーティション、継承、行レベルセキュリティ、権限、そして
autoIncrement の判定を超えるシーケンス。

`CHECK` 制約と排他制約、索引の述語と式、`INCLUDE` 列、演算子クラス、`DESC` / `NULLS`
の並び、`DEFERRABLE` は設計フォーマットに書き場所が無いため、警告して捨てる。
`-schema` で選ばなかったスキーマのものも捨てるが、そちらへ張られた外部キーだけは
実在する関係が失われるので報告する。

PostgreSQL の型が `type` と `length` / `precision` / `scale` にどう分解されるかは
[DB 設計 JSON フォーマット](db-design-format.ja.md#import-時の-postgresql-型の扱い)
にある。

## version

```sh
jjf version
# jjf v0.1.0
# built with go1.24.0 for linux/amd64
```

リリースバイナリはタグ名を、`go install` で入れたものは Go が記録した
モジュールバージョンを表示する。

## 終了コード

| コード | 意味 | 典型的な原因 |
| --- | --- | --- |
| 0 | 成功 | — |
| 1 | 一般エラー | 上記のいずれにも分類されない内部エラー |
| 2 | 入力不正 | 引数の誤り、ファイルが無い、JSON 構文エラー、未対応の `formatVersion`、未知の出力形式、`-o -` を端末に向けた、ダンプを解析できない、警告がある状態での `-strict` |
| 3 | スキーマ検証エラー | JSON Schema 違反 |
| 4 | 出力生成エラー | 出力先に書き込めない、ディレクトリが無い |

CI で使うときは **3 と 2 を区別できる**ことが重要である。3 は設計 JSON の中身の問題、
2 は呼び出し方・ファイルの場所・`jjf` のバージョンの問題である。

成功メッセージは標準出力、エラーと usage は標準エラーに出力される。
