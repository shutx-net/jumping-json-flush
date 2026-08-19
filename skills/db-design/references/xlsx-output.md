# Reading the generated workbook

How `jjf export xlsx` lays out the workbook and how to read its cells. The
layout is fixed inside `jjf` and cannot be driven from the JSON, so a request
to change how the Excel looks cannot be satisfied by editing the document — say
so.

Back to [SKILL.md](../SKILL.md).

## Sheets

In order: **表紙** (cover) → **テーブル一覧** (table list) → one **テーブル定義**
(table definition) sheet per table, in `tables` order.

The cover is always emitted and carries **no generation timestamp**, so the same
input always produces a byte-identical `.xlsx`. That is deliberate: it makes diff
review and CI comparison possible.

| Sheet | Contents |
| --- | --- |
| 表紙 | データベース名, 論理名, DBMS, テーブル数, フォーマットバージョン, 説明 |
| テーブル一覧 | No, 物理テーブル名, 論理テーブル名, 説明, カラム数, シート名 |
| テーブル定義 | The column grid, then one block per constraint kind |

An optional value that is absent leaves an empty cell rather than dropping the
row, so every cover has the same shape.

## The column grid

Headings, left to right: `No`, `物理カラム名`, `論理カラム名`, `型`, `長さ`,
`NULL`, `既定値`, `自動採番`, `説明`.

- `NULL` and `自動採番` mark yes with **`○`** and no with an **empty cell**
- `長さ` shows `length` when present, otherwise `precision`, or `precision,scale`
  when both are set (`10,2`). It is empty when none of the three is set
- `既定値` distinguishes two states that look similar but are not: a `default` of
  the empty string produces an **empty-string cell**, while **no `default` key at
  all** produces a **blank cell**
- `説明` wraps

## Constraint blocks

Below the grid, in this order, and **a constraint the document does not define
produces no block at all**:

| Block | Columns |
| --- | --- |
| 主キー (PRIMARY KEY) | 制約名, 対象カラム |
| ユニークキー (UNIQUE) | 制約名, 対象カラム |
| 外部キー (FOREIGN KEY) | 制約名, 対象カラム, 参照先テーブル, 参照先カラム, ON UPDATE, ON DELETE |
| インデックス (INDEX) | インデックス名, 対象カラム, ユニーク |

Multi-column lists are joined with `, ` in one cell, preserving document order.
An unnamed constraint leaves the name cell blank. An omitted `onUpdate` or
`onDelete` leaves its cell blank, meaning no clause is emitted.

## Sheet names

A table's sheet is named after the table, subject to Excel's rules:

- Truncated to **31 characters**
- Collisions get a `(2)`, `(3)`, ... suffix, with the base name trimmed to make
  room. Excel compares sheet names case-insensitively, so `Users` and `users`
  collide too
- The **`シート名` column of テーブル一覧 shows the name actually allocated**, which
  is where to look to see whether truncation or numbering happened

Long table names therefore stay fully readable in `物理テーブル名` even when the
sheet tab is cut short.

## What cannot be changed

Layout, colours, fonts, column widths, row heights, sheet titles and the language
of the headings are all owned by `jjf`. There is no template, no theme option
and no property in the JSON that affects any of them.
