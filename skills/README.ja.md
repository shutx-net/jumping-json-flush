# jjf Agent Skill

[English](README.md)

jjf の DB 設計 JSON を編集・検証し、そこから Excel 設計書を再生成するための
Agent Skill。

| スキル | ディレクトリ | 呼び出し名 |
| --- | --- | --- |
| `db-design` | [`db-design/`](db-design/SKILL.md) | `/jjf:db-design` |

スキル本文は英語である。トリガー語には日本語も含めてあるので、日本語で依頼しても
起動する。応答は依頼した言語に従う。

同じ内容（記法の作法、型の語彙、検証エラーの対応表、編集レシピ）を日本語で
読み・レビューするための人間向けドキュメントが
[`docs/db-design-guide.ja.md`](../docs/db-design-guide.ja.md) にある。
エージェント向けではなく人間向けの文書である。エージェントが読むのは英語スキルであり、
両者が食い違った場合は英語スキルが正である。

## プラグインとして導入する

推奨する導入方法。スキルは Claude Code プラグイン `jjf` として梱包され、
マーケットプレース `jjf-tools` から配布される。

```text
/plugin marketplace add https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/.claude-plugin/marketplace.json
/plugin install jjf@jjf-tools
```

導入後は `/jjf:db-design` で明示的に呼び出せる。やりたいことを普通に書けば
Claude が自分で読み込む。

**利用者側に git / npm / Node は不要。** プラグインは HTTPS 経由で取得する zip
アーカイブとして配布されるため、クローンもパッケージレジストリからのインストールも
発生しない。アーカイブは SHA-256 でピン留めされ、ダウンロードごとに Claude Code が
検証する。

**Claude Code v2.1.224 以降が必要。** `archive` 形式のプラグインソースが入った
バージョンである。v2.1.120〜v2.1.223 では
`This plugin uses a source type your Claude Code version does not support` で
インストールが拒否され、それより古いバージョンではマーケットプレース自体が
読み込まれない。

マーケットプレースの URL は[デフォルトブランチ](../.claude-plugin/marketplace.json)
を指しており、リリースアセットではない。これは意図的である。タグを含む URL を
カタログとして登録させると、その利用者は永久にそのリリースへ固定され、
`/plugin marketplace update` でも移動できなくなる。どのリリースを配るかは
カタログの中身が持つ。最初のタグ付きリリース以降にインストール可能になる。

新しいリリースへ移るには:

```text
/plugin marketplace update jjf-tools
/plugin update jjf@jjf-tools
```

## ディレクトリをコピーして導入する

マーケットプレースに依存せずスキルをリポジトリにコミットしたい場合の代替手段。
`SKILL.md` と `references/` をまとめて、ディレクトリごとコピーする。

プロジェクトスキルとして入れると、そのリポジトリで作業する全員が使えるようになり、
リポジトリと一緒にコミットされる。

```sh
mkdir -p /path/to/your-repo/.claude/skills
cp -r skills/db-design /path/to/your-repo/.claude/skills/
```

パーソナルスキルとして入れると、そのマシンの全プロジェクトで使えるようになり、
どこにもコミットされない。

```sh
mkdir -p ~/.claude/skills
cp -r skills/db-design ~/.claude/skills/
```

この方法で入れた場合、呼び出しは `jjf:` の付かない `/db-design` になり、
更新は届かない。

## 導入を確認する

`/plugin` で導入済みプラグインの一覧と有効・無効の切り替えができる。`/skills` は
導入方法に関係なく Claude Code が認識済みのスキルを一覧する。`claude doctor` で
各スキルの frontmatter が検証される。

## 前提条件

スキルは `jjf` CLI を呼び出すため、`PATH` に入っている必要がある。プラグインは
バイナリを同梱しない（スキルであってインストーラではない）。導入方法は
リポジトリの [README](../README.ja.md) を参照。

frontmatter は Agent Skills のどのホストでも解釈できるフィールド
（`name` / `description` / `license` / `compatibility` / `allowed-tools` /
`metadata`）だけを使っているため、同じディレクトリを claude.ai のスキル
アップロードや Anthropic Agent SDK でもそのまま利用できる。

`allowed-tools` で事前承認しているのは `Read` と `jjf` の 2 サブコマンドだけ
である。読み取りを行う `validate` と、利用者が指定したパスへブックを書き出す
`export` の 2 つである。JSON の編集は通常どおり権限確認を経る。`Write` / `Edit` の
事前承認はスキルを入れる利用者にとって権限昇格になるため、意図的に含めていない。

## プラグインのリリース手順

メンテナ向け。アーカイブは `v*` タグで
[`.github/workflows/release.yml`](../.github/workflows/release.yml) が
`jjf-plugin-<tag>.zip` として CLI バイナリと一緒に公開する。
ダイジェストは `checksums.txt` にも入る。

1. 1 つのコミットで、[`.claude-plugin/plugin.json`](../.claude-plugin/plugin.json)
   と [`.claude-plugin/marketplace.json`](../.claude-plugin/marketplace.json) の
   プラグインエントリの `version` を先頭の `v` を除いた新バージョンにし、
   `source.url` を新タグのアセットに向け、**`source.sha256` を削除する**。
   前リリースのダイジェストが残っている状態は、無い状態より悪い。全インストールが
   整合性検証で失敗するのに対し、無い場合は単にピン留めなしでインストールされる
   だけである。タグと矛盾していればリリースジョブが公開を拒否する。
2. タグを打って push する。ジョブがアーカイブを作り、ダイジェストを計算し、
   リリースを公開し、`marketplace.json` の次の内容をジョブサマリに全文出力する。
   同じ内容は `marketplace-json` アーティファクトとしても添付される。
3. その内容をデフォルトブランチにコミットする。コミットするまでインストールは
   ピン留めなしになる。ダイジェストは再現可能なので、同じタグでジョブを再実行しても
   同じ値になる。

ジョブがデフォルトブランチへ push することはなく、`marketplace.json` を
リリースアセットとして公開することもない。

## 参考

- Agent Skills のドキュメント: <https://code.claude.com/docs/en/skills.md>
- プラグインのリファレンス: <https://code.claude.com/docs/en/plugins-reference.md>
- プラグインマーケットプレース: <https://code.claude.com/docs/en/plugin-marketplaces.md>
