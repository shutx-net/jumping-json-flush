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
jjf export svg  db-design.json -o er.svg
jjf export ddl  db-design.json -o schema.sql
```

対応フォーマットは 3 つある。Excel の DB 設計書 `xlsx`、`jjf` 自身が描く ER 図
`svg`、文書が名指したデータベース向けの DDL スクリプト `ddl` である。3 つは
同じ約束を共有する。

- 出力前に必ず検証する。**検証に失敗した文書からは出力ファイルが 1 バイトも作られない**
- `-o` を省略すると、**入力パスの拡張子を選んだフォーマットのものに置き換えた場所**へ出力する
  （`docs/db-design.json` → `docs/db-design.xlsx`・`docs/db-design.svg`・
  `docs/db-design.sql`）。拡張子がフォーマット名と違うのは `ddl` だけである。
  `.ddl` というファイルは存在しないし、フォーマット名を `sql` にすると、
  スキーマを一から作る 1 本のスクリプトではなく任意の SQL を書くように読めてしまう
- `-o -` で標準出力へ書き出せる。ただし**フォーマットがバイナリで標準出力が端末の場合は
  拒否する**。今のところ該当するのは `xlsx` だけである。パイプやリダイレクトなら常に通る
- 出力は一時ファイルへ書いてから rename するので、途中で失敗しても壊れたファイルが残らない
- **同じ入力からは、どのフォーマットでも常にバイト同一の出力**が得られる
- **自分自身と矛盾する文書を拒否するのは `ddl` だけである**（終了コード 2）。
  データベースが受け付けない SQL は何の役にも立たないが、少し壊れた文書でも
  Excel 設計書と ER 図は役に立つ。だから `xlsx`・`svg` は描き出し、
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

### svg

```sh
jjf export svg db-design.json -o er.svg
```

こちらは `jjf` 自身が**描く**。レンダラを入れる必要も、設定するものも無い。
ランク付けも並び順も座標も文字幅も `jjf` 自身のもので、書き出した `.svg` は
そのままブラウザで開けるし README にも表示される。

```text
db-design.json --[jjf]--> er.svg
```

- SVG はテキストなので、`xlsx` と違い**`-o -` を端末に向けても拒否しない**
- テーブル 1 つにつき箱 1 つ、列 1 つにつき行 1 つ（`PK` / `FK` のマーカー、
  物理名と論理名、型が入る）、外部キー 1 本につき関係 1 本。関係ごとの crow's foot
  記法は文書から推論する。規則は下にある
- 文書が定義していないテーブルを参照する外部キーは、破線のスタブとして描く。
  exporter は何も検査せず、何も報告しない。そうした文書は合法で、図は JSON が
  主張しているとおりのものを示す。同じ文書を警告として報告するのは `jjf validate`
  の役目である
- **背景は不透明な白の矩形**であり、透過ではない。暗い文字の図を透過にすると、
  このファイルが最も見られるであろうダークモードの README では読めなくなる。
  かといってテーマに追従させるには `prefers-color-scheme` を書いた `<style>` が
  要るが、それは GitHub の SVG sanitiser が落とすものそのものである。
  ブラウザでは正しく、置きたかった場所では間違っているファイルになってしまう
- **レイアウトに調整つまみは無い**し、これから増えることも無い。ワークブックと
  同じく、この図は `jjf` のものである。方向も間隔も曲線も選ぶフラグは無い。
  それはブックの列幅や配色にフラグが無いのと同じである。つまみを足せば上の約束も
  失われる。バイト同一になるのが「同じ入力から」ではなく
  「同じ入力と同じフラグから」に変わってしまう

#### 多重度

crow's foot 記法は**推論**である。JSON は多重度を一切述べていない。

- 子側は、外部キーの列が集合として子テーブル内で一意に制約されているとき **1** に
  なる。判定材料は主キー、ユニークキー、そして一意インデックスである。
  そうでなければ **多** になる
- 子側は常に**任意**である。主キー・ユニークキー・`NOT NULL` はいずれも、親 1 行に
  子が何行まで並べるかを縛るだけで、最低何行必要かを言わない。だから親 1 行に子が
  必ず 1 行あるとは文書のどこからも導けない
- 親側は常に **1** である。外部キーは特定の 1 行を指すためである
- 親側は、外部キーの列に 1 つでも nullable があれば**任意**になる。その場合、子の行は
  親を指さずに存在できる。すべて `NOT NULL` なら**必須**である

### ddl

```sh
jjf export ddl db-design.json -o schema.sql
psql -d mydb -f schema.sql            # PostgreSQL の文書
mysql mydb < schema.sql               # MySQL の文書
```

`jjf` が書くのは SQL の**テキスト**であり、データベースへ接続することはない。
だからここでも実行時依存が増えない。スクリプトを適用するのは読み手自身の
クライアントの仕事である。

```text
db-design.json --[jjf]--> schema.sql --[手元の psql / mysql]--> データベース
```

生成する DDL は**スキーマを一から作る**。既にスキーマを持つデータベースへ
適用することは対応していないし、これから対応することもない。既存のスキーマを
別の状態へ動かすには、いまどの状態にあるかを知る必要があり、それは
introspection であり、別の道具の仕事だからである。`.xlsx` や `.svg` と同じく
`.sql` は**ビルド成果物**であり、編集せず作り直す。設計そのものとして扱わない。
生成したファイルの冒頭 2 行がそう述べている。

#### PostgreSQL と MySQL

`database.dbms` が方言を選ぶ。必須である。このフィールドを読むのは
`jjf export ddl` だけであり、しかも厳密に読む。値が無いことは既定値ではなく
エラーであり、`jjf` が書かない system を名指すこともエラーである。

```text
jjf: ddl export needs the document to name its target; add "dbms": "PostgreSQL" or "MySQL" to "database"
jjf: ddl export supports PostgreSQL, MySQL; this document names "MariaDB"
```

どちらも終了コードは 2 で、他の何よりも先に報告する。MariaDB の文書に
PostgreSQL の規則を説教しないためである。

`jjf` が書く方言は、端から端まで確かめられる 2 つだけである。どちらにも、実在の
データベースを読み戻す importer と、その 2 つを実機サーバ上で突き合わせる CI の
往復がある。それが無い方言は golden ファイルだけを根拠に出荷することになり、
golden が証明するのは「生成器が生成したものを生成した」ことだけである。だから
`MariaDB`・`SQLite`・`Oracle`・`SQLServer` は近似せず拒否する。これらを対象と
する文書には、誰も検証していない system の SQL を渡すのではなく、`jjf` はその
DDL を生成しないと答えるのが正しい。

#### 何を書くか

どちらの方言でも共通なこと。

- **文の順序は固定である**。まずすべての `CREATE TABLE`（`PRIMARY KEY` と
  `UNIQUE` は表定義の中に書く）、次にすべての `CREATE [UNIQUE] INDEX`、
  最後にすべての外部キーを `ALTER TABLE` として書く。各段階の中は文書の順で、
  **並べ替えは一切しない**。段階を固定することで表どうしの順序依存が消えるので、
  相互参照も自己参照もトポロジカルソートを必要としない
- **外部キーを表定義の中に書くことはない**。第 2 段階が先に来る必要があるのは、
  どちらの system も `UNIQUE` 制約だけでなく素の `UNIQUE INDEX` を外部キーの
  参照先として受け付けるからである
- **`default` は `DEFAULT ` の後にそのまま写す**。このフィールドは SQL 式のテキストと
  定義されており、空の既定値や式として読めない既定値は `jjf validate` が先に拒否している
- **固定の 2 行ヘッダ**。生成日時もツールのバージョンも入力パスも書かない。
  バージョンを書くと同じ文書について jjf のビルド違いが食い違うことになり、
  差分を取られるのはこの成果物だからである。この 2 行は両方の方言で同一である
- **全か無か**。文書全体を検査してから書き出す

##### PostgreSQL

- **段階は 4 つ**。上の 3 つに続けて、すべての `COMMENT ON` を書く
- **`autoIncrement` は `GENERATED BY DEFAULT AS IDENTITY`** になる。標準 SQL であり、
  PostgreSQL 自身が `SERIAL` より推奨する形である
- **識別子は常に二重引用符で囲む**。大文字小文字が保たれ、`order` や `user` のような
  予約語も予約語一覧を持たずに通る。**型名は決して囲まない**。`"integer"` は型ではないし、
  `"ORDER_STATUS"` は PostgreSQL が実際に作る小文字の型とは別物になってしまう
- **`logicalName` と `description` は `COMMENT ON`** になる。両者は実際の改行で
  つなぐ。1 行目が論理名、残りが説明であり、これは `jjf import` がコメントを読み戻す
  規則そのものである。論理名が物理名と同じで説明も無いオブジェクトにはコメントを
  書かない。それは、ダンプにコメントが無かったオブジェクトを import が残す状態だからである
- 生成するスクリプトは `standard_conforming_strings = on`（PostgreSQL 9.1 以降の既定）を
  前提とする。文字列リテラル中のバックスラッシュはただの文字である。`SET` は書かない

##### MySQL

- **段階は 3 つ**。MySQL には `COMMENT ON` 文が存在しないので、第 4 段階を置く
  場所が無い。`logicalName` と `description` は `CREATE TABLE` の中へ畳み込み、
  列定義の末尾の `COMMENT '...'` と、閉じ括弧の後ろの `COMMENT='...'` になる。
  2 つを実際の改行でつなぐ規則は PostgreSQL と同じである
- **`autoIncrement` は `AUTO_INCREMENT`** になり、`NOT NULL` と `DEFAULT` の後ろに
  書く。これは `mysqldump` が書く順序であり、生成したスクリプトと、そのスクリプトが
  作ったデータベースのダンプとが同じように読めるようにするためである
- **識別子は常に逆引用符で囲む**。理由は同じで、中の逆引用符は二重にする。二重引用符は
  使わない。`sql_mode` に `ANSI_QUOTES` が入っていない限り MySQL は `"order"` を
  文字列リテラルとして読むからであり、それを入れるための `SET` は書かないからである
- **文字列リテラル中のバックスラッシュは二重にする**。`sql_mode` に
  `NO_BACKSLASH_ESCAPES` が入っていない限り（既定では入っていない）MySQL は
  バックスラッシュをエスケープ文字として読むので、`description` に書いた
  `C:\tmp` を二重にしないと、データベースには `C:` とタブが入ってしまう
- **`TINYINT` だけが `length` を保ち、他の整数型は落とす**。表示幅は MySQL 8.0.17 で
  非推奨になり `mysqldump` も書かなくなったので、`length: 11` の `INT` は `INT` と
  書く。`tinyint(1)` は今も書かれる。真偽値の格納形がそれだからである
- **型の属性はパラメータの後ろに置く**。`type` が `DECIMAL UNSIGNED` で
  `precision: 10`・`scale: 2` なら `DECIMAL(10,2) UNSIGNED` と書く。
  `DECIMAL UNSIGNED(10,2)` は構文エラーだからである

#### 拒否

`ddl` は自分自身と矛盾する文書を拒否し、何も書かない。

```text
db-design.json: error: primary key pk_orders on table orders: names column "id", which the table declares nullable
db-design.json: error: foreign key fk_orders_customer on table orders: references table "customers", which this document does not define
jjf: 2 problem(s) prevent PostgreSQL DDL generation
```

最後の行が名乗る方言は、文書自身が選んだものである。MySQL の文書なら
`prevent MySQL DDL generation` になる。

終了コードは 4 ではなく **2** である。悪いのは文書であり、4 は「環境が書き込みを
止めた」という意味を保たなければならない。`-strict` は無いし、これからも無い。
拒否は見逃せる警告ではない。見逃せばデータベースが受け付けない SQL ができるからである。

理由の多くは `jjf validate` が警告として報告するものと同じである。残りはここでしか
検査しない。文書についてではなくデータベース system についての言明だからであり、
しかも**2 つの方言で同じ一覧ではない**。要約の行が拒否した方言を名乗るのはそのため
である。

PostgreSQL では次の 2 群。

- **スキーマ全体でひとつの名前空間**。表名、索引名、`PRIMARY KEY` と `UNIQUE` の
  制約名は、スキーマごとにひとつの名前空間を共有するので、どれも互いに衝突できない。
  外部キーの制約名はここに入らない。表ごとの名前空間にあり、2 つの表が同じ名前の
  制約を持つことを PostgreSQL は認める
- **identity 列**。`autoIncrement` の列は `default` を併せ持てない（PostgreSQL が
  両方持つ列を拒否する）。`nullable` にもできない。PostgreSQL が黙って `NOT NULL` に
  してしまい、データベースが文書と食い違うからである

MySQL では名前空間が**ちょうど逆**になる。

- **同じ名前の表が 2 つ**。これは PostgreSQL と同じく MySQL も作れない
- **同じ名前の外部キーが 2 つ**。スキーマのどこにあってもいけない。InnoDB は外部キーの
  名前をデータベースごとの名前空間で持ち、衝突には
  `Duplicate foreign key constraint name` と答える。索引名は逆で、表の中にあるので、
  2 つの表がそれぞれ `ix_created` を持つことは MySQL では通る
- **`AUTO_INCREMENT` の 4 つの規則**。1 つの表に 1 列まで。その表の `primaryKey` か
  `uniqueKeys` のいずれかの先頭列でなければならない（`indexes` の項目では代わりに
  ならない。スクリプトは表より 1 段階後に索引を作るからである）。`default` を
  併せ持てない。`nullable` にもできない。MySQL が `NOT NULL` として格納してしまい、
  データベースが文書と食い違うからである
- **`TEXT`・`BLOB`・`JSON` の列を含むキー**。MySQL は前者 2 つには接頭辞長を、
  `JSON` には生成列を求めるが、設計フォーマットにはどちらの置き場所も無い
- **`length` の無い `VARCHAR` と `VARBINARY`**。MySQL に既定の幅は無いので、
  列ではなく構文エラーになってしまう

#### 書かないもの

設計フォーマットに書き場所が無いので、スクリプトに現れることはない。`CHECK` 制約、
`CREATE TYPE`、既定以外のスキーマ、照合順序、部分索引と式索引、索引方式、
`DEFERRABLE`、格納パラメータ、パーティション、行レベルセキュリティ。MySQL の
スクリプトも文法が違うだけで同じ考えである。`CHECK` 制約、トリガ、ビュー、
表オプション（エンジン、文字集合、照合順序、行フォーマット、パーティション）、
`ON UPDATE CURRENT_TIMESTAMP` は書かない。エンジンや照合順序を推測して書けば、
誰も決めていない設計上の判断を書くことになるからである。
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

`ENUM` と `SET` は MySQL における同じ話であり、一段だけ鋭い。括弧が持つのは数値では
なく値の一覧で、フォーマットにその置き場所は無く、しかも MySQL は素の `ENUM` を
実行時ではなく構文解析の時点で拒否する。それでも文書が書いたとおりに出力する。
*すべての* `ENUM` 列がこの状態にあり、`jjf import` が実在のデータベースから作った
文書も例外ではないので、拒否すれば `jjf` 自身が書いた文書を拒否することになるから
である。`length` の無い `VARCHAR` は**同じ話ではなく**拒否する。`mysqldump` は必ず
長さを書くので、取り込んだ文書がこの拒否に当たることはない。

`ON UPDATE NO ACTION` と `ON DELETE NO ACTION` はデータベースを一周すると消える。
`NO ACTION` はどちらの system でも既定なので `pg_dump` も `mysqldump` も省き、
`jjf import` は何も記録しない。これは想定どおりであり、不具合ではない。MySQL には
同じように振る舞う場合が 2 つある。名前を付けた `primaryKey` は名前を失って戻る。
MySQL がすべての主キーを `PRIMARY` と呼ぶからである。そして参照動作の
`SET DEFAULT` は格納もされダンプにも戻るが、実行されることはない。InnoDB は実行時に
`NO ACTION` として扱う。

#### バイト決定性

**同じ入力からは常にバイト同一の `.xlsx`・`.svg`・`.sql` が生成される。**
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

mysqldump --no-data --default-character-set=utf8mb4 mydb > schema.sql
jjf import mysql schema.sql -o db-design.json
```

スキーマダンプから設計文書を組み立てる。入力は**ファイル**であり、`jjf` が
データベースへ接続することはない。

| dialect | 入力 |
| --- | --- |
| `postgres` | `pg_dump --schema-only` |
| `mysql` | `mysqldump --no-data` |

- 生成した文書は**書き出す前にスキーマで検証する**。`jjf validate` が拒否するような
  文書が import から出てくることはない
- `-o` を省略すると、**入力パスの拡張子を `.json` に置き換えた場所**へ出力する
  （`schema.sql` → `schema.json`）
- `-o -` で標準出力へ書き出せる。`export` と違い端末でも拒否しない。JSON は読める
  テキストだからである
- `-schema` は取り込む PostgreSQL スキーマを選ぶ（既定は `public`）。設計文書には
  スキーマ修飾の置き場所が無いため、一度に取り込めるのは 1 スキーマだけで、
  それ以外は捨てられる。これは **PostgreSQL 専用**であり、`mysql` に渡すと
  **黙って無視するのではなくエラーにする**。MySQL のスキーマはデータベース
  そのものなので、選ぶべき第 2 の階層が存在しないためである
- `-database` は生成する文書のデータベース名を決める。省略した場合、`postgres`
  ではダンプの `\connect` 行から、`mysql` では `USE` 文かヘッダの banner から
  採り、どちらも無ければ入力ファイル名から採る（この場合、ファイル名自体が
  識別子として妥当である必要がある）
- `-strict` はすべての警告をエラーに変える。このとき出力は書かれない
- 想定しているのは **pg_dump 13 〜 18** と **MySQL 8.0** の出力であり、リポジトリに
  取り込んだ実ダンプで検証済みである（PostgreSQL はこの範囲の全メジャーが、MySQL は
  捕獲したすべての系列が、同一の文書にバイト単位で一致して取り込まれる）。ヘッダの
  バージョン表記を読み、範囲外のダンプは失敗ではなく警告にする。名乗る範囲は捕獲した
  ダンプが覆う範囲ちょうどであり、8.4 や 9.x を捕獲することが範囲を広げる手順である

**`mysqldump` には `--default-character-set=utf8mb4` を渡すこと。** 付けないと
クライアントが `latin1` で接続することがあり、ダンプ中の日本語の `COMMENT` が
二重にエンコードされる。そのダンプも構文解析でき、取り込めて、往復もするので、
気付く手がかりは自分の `logicalName` に現れる文字化けだけである。

### ダンプについて jjf が言うこと

3 段階ある。どれになるかは SQL の珍しさではなく、設計フォーマットに書き場所が
あるかどうかで決まる。

| 段階 | 例 | 挙動 |
| --- | --- | --- |
| 黙って読み飛ばす | `SET`, `GRANT`, `CREATE VIEW`, `CREATE FUNCTION`, `OWNER TO`、MySQL ではさらに `LOCK TABLES`, `DROP TABLE`, `DELIMITER`、トリガとルーチン、そしてすべてのテーブルオプション | 何も出さない。ダンプはこれらで埋まっており、いちいち警告すると本当に必要な警告が埋もれるため |
| 警告する | `CHECK` 制約、部分索引・式索引、`INCLUDE`、btree 以外のアクセスメソッド、`DEFERRABLE`、`INHERITS`、生成列、MySQL ではさらに `FULLTEXT`・`SPATIAL` 索引、索引の前置長、鍵の `DESC`、`ON UPDATE CURRENT_TIMESTAMP`、`ENUM`・`SET` の値リスト、パーティション、InnoDB 以外のエンジン | 標準エラーへ行番号付きで 1 行出す。**周囲のテーブルや索引はそのまま取り込む** |
| エラーにする | 構文として壊れた SQL、書けない識別子、同名テーブルの二重定義 | 終了コード 2。何も書かれない |

```text
$ jjf import postgres schema.sql -o db-design.json
schema.sql:14: warning: constraint users_email_check on table public.users: check constraint is not imported
schema.sql:20: warning: index users_email_live_idx on table public.users: partial index predicate is not imported
schema.sql:22: warning: index users_doc_idx on table public.users: access method gin is not imported; recorded as a plain index
db-design.json: written
```

```text
$ jjf import mysql schema.sql -o db-design.json
schema.sql:31: warning: index ft_users_bio on table users: full-text index is not imported
schema.sql:32: warning: constraint ck_users_email on table users: check constraint is not imported
schema.sql:29: warning: users.updated_at: ON UPDATE CURRENT_TIMESTAMP is not represented
db-design.json: written
```

警告の並びは行番号順ではなく、見つけた順である。構文解析が気付いたものが先に、
解決の段が気付いたものが後に来る。

`file:line: warning:` という形は、エディタや CI のアノテータがそのまま解釈できる。
警告は標準エラー、成功メッセージは標準出力に出る。

書けない識別子は**黙って改名せずエラーにする**。`"user-profiles"` というテーブルは
`user_profiles` に化けるのではなく import を止める。改名された文書は一見正しく見えて、
存在しないデータベースを説明してしまうためである。例外は制約名で、スキーマ上
省略可能なので、書けない名前は警告とともに落とし、制約自体は名前なしで取り込む。

### logicalName と description

スキーマはすべてのテーブルとカラムに `logicalName` を要求するが、ダンプにはそれが無い。
そこで次のようにする。

- コメントの**1 行目**を `logicalName` にする
- **2 行目以降**を `description` にする
- コメントが**無い**テーブル・カラムには物理名をそのまま `logicalName` として置く

ここでいうコメントは、PostgreSQL では `COMMENT ON` 文、MySQL では列に書かれた
`COMMENT` かテーブルの `COMMENT=` オプションである。分け方は `jjf export ddl` が
両方の dialect で組み立てるものと同一で、これが実在のデータベースを一往復しても
文書が変わらない理由である。

最後の規則は答えではなく、編集の出発点である。生成された文書は開いて本当の名前を
与えるためにある。

### 取り込まないもの

ビュー、マテリアライズドビュー、関数、トリガ、ルーチン、イベント、列の型として
使われた enum の名前を超える型定義、拡張、パーティション、継承、行レベル
セキュリティ、権限、そして autoIncrement の判定を超えるシーケンス。

`CHECK` 制約と排他制約、索引の述語と式、`INCLUDE` 列、演算子クラス、`DESC` / `NULLS`
の並び、`DEFERRABLE` は設計フォーマットに書き場所が無いため、警告して捨てる。
`-schema` で選ばなかったスキーマのものも捨てるが、そちらへ張られた外部キーだけは
実在する関係が失われるので報告する。

MySQL では次が加わる。

| 取り込まないもの | 理由 |
| --- | --- |
| テーブルオプション（エンジン、文字セット、照合順序、行フォーマット、`AUTO_INCREMENT` の現在値） | フォーマットに置き場所が無い。InnoDB 以外のエンジンだけは警告する。文書が宣言する外部キーを実際に効かせるのは InnoDB だけだからである |
| パーティション | 分割された表は文書が説明する表ではない。ただし列は真なので表は取り込み、分割だけを報告する |
| `FULLTEXT`・`SPATIAL` 索引 | どちらも文書が名指しする列に対する索引ではない。一方は `MATCH … AGAINST` に、もう一方は R-tree の問い合わせに答えるものである |
| 索引の前置長 `KEY ix (body(255))` | 索引は文書が名指しする列を覆ったままなので取り込み、狭められたことだけを報告する |
| 鍵の `DESC` | MySQL 8 には本物の降順索引があるが、フォーマットが記録するのは列だけである |
| `ON UPDATE CURRENT_TIMESTAMP` | 既定値ではなく自動更新の規則である。`default` に畳み込めば MySQL が拒否するスクリプトになる |
| `ENUM`・`SET` の値リスト | フォーマットに置き場所が無い。型名は残り、値は警告が名指しする |
| 外部キーを支えるために InnoDB が作る索引 | 外部キーと同じ名前で現れるが、jjf の文書は制約名と索引名を表ごとにひとつの名前空間で持つ。索引を作り直すのは文書の外部キーなので、失われるものは無い |
| 可為 null 列の `DEFAULT NULL` | MySQL は既定値を与えられなかった可為 null 列すべてにこれを書き、内容は `nullable` が既に言っていることと同じである。列ごとに警告すれば他が埋もれるので黙って落とす |

型が `type` と `length` / `precision` / `scale` にどう分解されるかは DB 設計 JSON
フォーマットの
[PostgreSQL](db-design-format.ja.md#import-時の-postgresql-型の扱い) と
[MySQL](db-design-format.ja.md#import-時の-mysql-型の扱い) にある。

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
