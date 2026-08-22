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

### 参照整合性の検査

スキーマに適合した文書は、続けて**自分自身と矛盾していないか**を検査する。
見るのは次の 8 点である。

- 主キー・ユニークキー・外部キー・インデックスが指すカラムが、宣言している
  テーブルに定義されていること
- 外部キーの参照先テーブルが、この文書で定義されていること
- 外部キーの列数と参照先の列数が一致すること
- 参照先の列が集合として一意であること（参照先テーブルの主キー・ユニークキー・
  ユニークインデックスのいずれかによる）
- 主キーのカラムが `nullable: true` でないこと
- 同一テーブル内でカラム名、および制約名・インデックス名が重複しないこと
- `default` が空でないこと。DEFAULT 句がないカラムには `default` キー自体を
  書かない
- `default` が SQL 式として読めること。文字列の既定値は SQL の引用符を含める
  ので、文字列の `now` は `"'now'"` と書く。引用符のない `"now"` は SQL では
  カラム参照であり、DEFAULT 句には書けない

検出結果は 1 件 1 行で標準エラーに出力する。位置ではなく対象の名前で示す。

```text
db-design.json: warning: foreign key fk_orders_customer on table orders: references table "customers", which this document does not define
db-design.json: warning: primary key pk_orders on table orders: names column "id", which the table declares nullable
db-design.json: warning: index ix_orders_placed_at on table orders: names column "placed_at", which the table does not define
```

これらは**警告**である。終了コードは 0 のままで、標準出力の成功行に件数が添えられる。
いま通る文書はこれからも通る。

```text
db-design.json: OK, 3 warning(s)
```

- `-strict` はすべての警告をエラーに変える。警告はどちらの場合も出力される。
  `-strict` が変えるのは実行の成否だけであり、このとき標準出力には何も書かれない

```sh
jjf validate -strict db-design.json   # 検出があれば終了コード 2
```

`-strict` での失敗は **3 ではなく 2** である。3 は JSON Schema に適合しないことだけを
意味する。参照整合性の指摘はスキーマ違反ではなく、`-strict` を付けたのは呼び出し方の
問題だからである。

設計の良し悪し（正規化・インデックス設計・型選択・命名規約）は判断しない。
テーブル名の文書内での重複、インデックス名のスキーマ全体での一意性も検査しない。
`default` は式として読むだけで、評価はしない。実行もしなければ、カラムの `type`
との整合も見ないし、知らない関数名を咎めることもない。

## export

```sh
jjf export xlsx db-design.json -o db-design.xlsx
jjf export dot  db-design.json -o er.dot
jjf export ddl  db-design.json -o schema.sql
```

対応フォーマットは 3 つある。Excel の DB 設計書 `xlsx`、Graphviz の ER 図 `dot`、
PostgreSQL の DDL スクリプト `ddl` である。3 つは同じ約束を共有する。

- 出力前に必ず検証する。**検証に失敗した文書からは出力ファイルが 1 バイトも作られない**
- `-o` を省略すると、**入力パスの拡張子を選んだフォーマットのものに置き換えた場所**へ出力する
  （`docs/db-design.json` → `docs/db-design.xlsx`・`docs/db-design.dot`・
  `docs/db-design.sql`）。拡張子がフォーマット名と違うのは `ddl` だけである。
  `.ddl` というファイルは存在しないし、フォーマット名を `sql` にすると、
  スキーマを一から作る 1 本のスクリプトではなく任意の SQL を書くように読めてしまう
- `-o -` で標準出力へ書き出せる。ただし**フォーマットがバイナリで標準出力が端末の場合は
  拒否する**。今のところ該当するのは `xlsx` だけである。パイプやリダイレクトなら常に通る
- 出力は一時ファイルへ書いてから rename するので、途中で失敗しても壊れたファイルが残らない
- **同じ入力からは、どのフォーマットでも常にバイト同一の出力**が得られる
- **自分自身と矛盾する文書を拒否するのは `ddl` だけである**（終了コード 2）。
  データベースが受け付けない SQL は何の役にも立たないが、少し壊れた文書でも
  Excel 設計書と ER 図は役に立つ。だから `xlsx` と `dot` は描き出し、
  その矛盾を報告するのは `jjf validate` の役目である

### xlsx

```sh
# パイプへ流す
jjf export xlsx db-design.json -o - | sha256sum

# 端末へ直接出そうとすると拒否される (終了コード 2)
jjf export xlsx db-design.json -o -
# jjf: refusing to write a workbook to the terminal; redirect standard output or pass -o <file>
```

ワークブックは表紙、テーブル一覧、そしてテーブルごとのシートからなる。

### dot

```sh
jjf export dot db-design.json -o er.dot
```

`jjf` が書くのは DOT の**ソース**であり、graphviz を実行することはない。
だから実行時依存が増えない。`.dot` を画像にするのは読み手自身の `dot` の仕事である。

```text
db-design.json --[jjf]--> er.dot --[手元の dot]--> SVG/PNG
```

```sh
dot -Tsvg er.dot -o er.svg
```

- DOT はテキストなので、`xlsx` と違い**`-o -` を端末に向けても拒否しない**
- 図はテーブル 1 つにつき節 1 つ（列を並べた HTML 風の表で、各行に `PK` / `FK` の
  マーカー、物理名と論理名、型が入る）と、外部キー 1 本につき辺 1 本からなる
- 文書が定義していないテーブルを参照する外部キーは、失敗させずに破線のスタブ節として
  描く。exporter は何も検査せず、何も報告しない。そうした文書は合法で、図は JSON が
  主張しているとおりのものを示す。同じ文書を警告として報告するのは `jjf validate`
  の役目である

#### 多重度

crow's foot 記法は**推論**である。JSON は多重度を一切述べていない。

- 子側は、外部キーの列が集合として子テーブル内で一意に制約されているとき **1** に
  なる。判定材料は主キー、ユニークキー、そして一意インデックスである。
  そうでなければ **多** になる
- 子側は、外部キーの列に 1 つでも nullable があれば**任意**、すべて `NOT NULL` なら
  **必須**になる
- 親側は常に **1** かつ**必須**である。外部キーは特定の 1 行を指すためである

### ddl

```sh
jjf export ddl db-design.json -o schema.sql
psql -d mydb -f schema.sql
```

`jjf` が書くのは SQL の**テキスト**であり、データベースへ接続することはない。
だからここでも実行時依存が増えない。スクリプトを適用するのは読み手自身の
`psql` の仕事である。

```text
db-design.json --[jjf]--> schema.sql --[手元の psql]--> データベース
```

生成する DDL は**スキーマを一から作る**。既にスキーマを持つデータベースへ
適用することは対応していないし、これから対応することもない。既存のスキーマを
別の状態へ動かすには、いまどの状態にあるかを知る必要があり、それは
introspection であり、別の道具の仕事だからである。`.xlsx` や `.dot` と同じく
`.sql` は**ビルド成果物**であり、編集せず作り直す。設計そのものとして扱わない。
生成したファイルの冒頭 2 行がそう述べている。

#### PostgreSQL 専用

`database.dbms` は必須で、値は `PostgreSQL` でなければならない。このフィールドを
読むのは `jjf export ddl` だけであり、しかも厳密に読む。値が無いことは既定値では
なくエラーである。

```text
jjf: ddl export needs the document to name its target; add "dbms": "PostgreSQL" to "database"
jjf: ddl export supports PostgreSQL only; this document names "MySQL"
```

どちらも終了コードは 2 で、他の何よりも先に報告する。MySQL の文書に PostgreSQL
の規則を説教しないためである。

#### 何を書くか

- **4 段階に固定した文の順序**。すべての `CREATE TABLE`（`PRIMARY KEY` と
  `UNIQUE` は表定義の中に書く）、すべての `CREATE [UNIQUE] INDEX`、すべての
  外部キーを `ALTER TABLE ... ADD CONSTRAINT` として、最後にすべての
  `COMMENT ON`。各段階の中は文書の順で、**並べ替えは一切しない**。
  段階を固定することで表どうしの順序依存が消えるので、相互参照も自己参照も
  トポロジカルソートを必要としない
- **外部キーを表定義の中に書くことはない**。第 2 段階が先に来る必要があるのは、
  PostgreSQL が `UNIQUE` 制約だけでなく素の `UNIQUE INDEX` も外部キーの参照先として
  受け付けるからである
- **`autoIncrement` は `GENERATED BY DEFAULT AS IDENTITY`** になる。標準 SQL であり、
  PostgreSQL 自身が `SERIAL` より推奨する形である
- **識別子は常に二重引用符で囲む**。大文字小文字が保たれ、`order` や `user` のような
  予約語も予約語一覧を持たずに通る。**型名は決して囲まない**。`"integer"` は型ではないし、
  `"ORDER_STATUS"` は PostgreSQL が実際に作る小文字の型とは別物になってしまう
- **`default` は `DEFAULT ` の後にそのまま写す**。このフィールドは SQL 式のテキストと
  定義されており、空の既定値や式として読めない既定値は `jjf validate` が先に拒否している
- **`logicalName` と `description` は `COMMENT ON`** になる。両者は実際の改行で
  つなぐ。1 行目が論理名、残りが説明であり、これは `jjf import` がコメントを読み戻す
  規則そのものである。論理名が物理名と同じで説明も無いオブジェクトにはコメントを
  書かない。それは、ダンプにコメントが無かったオブジェクトを import が残す状態だからである
- **固定の 2 行ヘッダ**。生成日時もツールのバージョンも入力パスも書かない。
  バージョンを書くと同じ文書について jjf のビルド違いが食い違うことになり、
  差分を取られるのはこの成果物だからである
- **全か無か**。文書全体を検査してから書き出す

生成するスクリプトは `standard_conforming_strings = on`（PostgreSQL 9.1 以降の既定）を
前提とする。文字列リテラル中のバックスラッシュはただの文字である。`SET` は書かない。

#### 拒否

`ddl` は自分自身と矛盾する文書を拒否し、何も書かない。

```text
db-design.json: error: primary key pk_orders on table orders: names column "id", which the table declares nullable
db-design.json: error: foreign key fk_orders_customer on table orders: references table "customers", which this document does not define
jjf: 2 problem(s) prevent PostgreSQL DDL generation
```

終了コードは 4 ではなく **2** である。悪いのは文書であり、4 は「環境が書き込みを
止めた」という意味を保たなければならない。`-strict` は無いし、これからも無い。
拒否は見逃せる警告ではない。見逃せばデータベースが受け付けない SQL ができるからである。

理由の多くは `jjf validate` が警告として報告するものと同じである。次の 2 群だけは
ここでしか検査しない。文書についてではなく PostgreSQL についての言明だからである。

- **スキーマ全体でひとつの名前空間**。表名、索引名、`PRIMARY KEY` と `UNIQUE` の
  制約名は、スキーマごとにひとつの名前空間を共有するので、どれも互いに衝突できない。
  外部キーの制約名はここに入らない。表ごとの名前空間にあり、2 つの表が同じ名前の
  制約を持つことを PostgreSQL は認める
- **identity 列**。`autoIncrement` の列は `default` を併せ持てない（PostgreSQL が
  両方持つ列を拒否する）。`nullable` にもできない。PostgreSQL が黙って `NOT NULL` に
  してしまい、データベースが文書と食い違うからである

#### 書かないもの

設計フォーマットに書き場所が無いので、スクリプトに現れることはない。`CHECK` 制約、
`CREATE TYPE`、既定以外のスキーマ、照合順序、部分索引と式索引、索引方式、
`DEFERRABLE`、格納パラメータ、パーティション、行レベルセキュリティ。
`database.logicalName` と `database.description` も書かない。`CREATE SCHEMA` も
`CREATE DATABASE` も出さない以上、コメントを付ける相手が無く、`jjf import` も
この 2 つを埋めないので、失われるものは無い。

はっきり述べておくべき帰結が 2 つある。

- **ユーザ定義型は名前が出るだけで作られない。** 列の型は不透明な文字列なので、
  PostgreSQL から取り込んだ列挙型やドメインを名前で持つ文書からは、そのファイル内の
  どの文も作らない型を参照するスクリプトができる。構文としては通り、実行時に失敗する。
  これは設計フォーマットの限界であって不具合ではなく、塞ぐには schema に型定義を
  教える必要がある
- **既知の型が取れないパラメータは黙って落とす。** `length: 11` の `INTEGER` は
  `INTEGER` と書く。`integer(11)` は PostgreSQL が拒否する DDL だからである。
  `jjf` が知らない型については、名前もパラメータも文書が述べたとおりに再現する

`ON UPDATE NO ACTION` と `ON DELETE NO ACTION` はデータベースを一周すると消える。
`NO ACTION` は PostgreSQL 自身の既定なので `pg_dump` が省き、`jjf import` は何も
記録しない。これは想定どおりであり、不具合ではない。

#### バイト決定性

**同じ入力からは常にバイト同一の `.xlsx`・`.dot`・`.sql` が生成される。**
生成日時もツールのバージョンも埋め込まず、ZIP のタイムスタンプを固定し、
Go の map の反復順に依存しないためである。

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
- 想定しているのは **pg_dump 13 〜 18** の出力であり、この範囲の全メジャーの実ダンプで
  検証済みである（6 つとも同一の文書にバイト単位で一致して取り込まれる）。
  ヘッダのバージョン表記を読み、範囲外のダンプは失敗ではなく警告にする

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
| 2 | 入力不正 | 引数の誤り、ファイルが無い、JSON 構文エラー、未対応の `formatVersion`、未知の出力形式、バイナリ形式（`xlsx`）で `-o -` を端末に向けた、ダンプを解析できない、警告がある状態での `-strict` |
| 3 | スキーマ検証エラー | JSON Schema 違反 |
| 4 | 出力生成エラー | 出力先に書き込めない、ディレクトリが無い |

CI で使うときは **3 と 2 を区別できる**ことが重要である。3 は設計 JSON の中身の問題、
2 は呼び出し方・ファイルの場所・`jjf` のバージョンの問題である。3 は JSON Schema への
適合だけを意味する。参照整合性の指摘はスキーマ違反ではないので、`validate -strict` は
`import -strict` と同じく 2 で報告する。

成功メッセージは標準出力、エラーと usage は標準エラーに出力される。
