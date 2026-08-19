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
| 2 | 入力不正 | 引数の誤り、ファイルが無い、JSON 構文エラー、未対応の `formatVersion`、未知の出力形式、`-o -` を端末に向けた |
| 3 | スキーマ検証エラー | JSON Schema 違反 |
| 4 | 出力生成エラー | 出力先に書き込めない、ディレクトリが無い |

CI で使うときは **3 と 2 を区別できる**ことが重要である。3 は設計 JSON の中身の問題、
2 は呼び出し方・ファイルの場所・`jjf` のバージョンの問題である。

成功メッセージは標準出力、エラーと usage は標準エラーに出力される。
