# jjf のインストール

[README](../README.ja.md) · [English](install.md)

`jjf` は静的リンクされた単一バイナリ。隣に入れるべきランタイムはなく、CGO も
glibc も要らないので、musl や alpine のイメージでもそのまま動く。

| 方法 | こういうときに |
| --- | --- |
| `install.sh` | 開発機やイメージを用意していて、curl か wget がある |
| リリースアーカイブ | CI で使う、または勝手に中身が変わらない URL が欲しい |
| `go install` | Go ツールチェインがすでにある |
| nix | nix を使っている |

## install.sh

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | sh
```

bash ではなく POSIX sh なので、`| bash` としても同じように動き、bash を持たない
alpine イメージでもそのまま実行できる。

やっていることは次の順:

1. `uname` からターゲットを判定し、公開アーカイブが無いプラットフォームは
   404 で気づくのではなく最初に拒否する
2. バージョン指定が無ければ GitHub API に最新リリースのタグを問い合わせる
3. アーカイブとリリースの `checksums.txt` を取得する
4. **sha256 を照合し、食い違えば展開する前に中止する**
5. 一時ディレクトリに展開する（このディレクトリはどう終了しても削除される）
6. インストール先に一時名でコピーしてから rename する。配置はアトミックで、
   実行中のバイナリも差し替えられる
7. `jjf version` を実行し、どこに入ったかを表示する

### オプション

| オプション | 環境変数 | 既定値 |
| --- | --- | --- |
| `--version <tag>` | `JJF_VERSION` | 最新リリース |
| `--dir <path>` | `JJF_INSTALL_DIR` | [配置先](#配置先) を参照 |
| `--help` | | |

パイプ経由で渡すときは `-s --` の後ろに置く。

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh |
  sh -s -- --version v0.1.0 --dir ~/bin
```

環境変数はシェルの手前に書くだけで、意味は同じ。こちらのほうが読みやすいことも
ある。

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh |
  JJF_VERSION=v0.1.0 JJF_INSTALL_DIR=~/bin sh
```

リリースバージョンの形をしていないタグは拒否される。ダウンロード URL に
埋め込まれる値は他に無い。

### 配置先

| 条件 | ディレクトリ |
| --- | --- |
| `--dir` か `JJF_INSTALL_DIR` を指定した | そのディレクトリ |
| root で実行、または `/usr/local/bin` が書き込み可能 | `/usr/local/bin` |
| それ以外 | `$HOME/.local/bin` |

**`sudo` は一切呼ばない。** パイプ実行の途中でパスワードを聞かれるのは不意打ちで、
端末が無い環境ではそもそも答えられない。root しか書けない場所に入れたい場合は、
スクリプト全体を root で実行する。

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | sudo sh
```

選ばれたディレクトリは最後に表示される。`PATH` に入っていない場合は、追加する
ための行も一緒に出る。

### 事前に必要なもの

- curl または wget
- tar（Windows では unzip）
- `sha256sum` / `shasum` / `openssl` のいずれか。**3 つとも無い場合はインストール
  せずに終了する**。ダウンロードしたものを検証する手段が無いため

### 対応プラットフォーム

公開ターゲットは `linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64`,
`darwin/arm64` の 5 種類。Linux / macOS / WSL はそのまま対象。

Windows では Git Bash か Cygwin 上でのみ動作し、`unzip` が必要になる。Git for
Windows には `unzip` が含まれないため、用意できない場合は後述の手順で zip を
直接取得する。

### CI で使う

パイプラインが勝手に別のものを入れないよう、バージョンを固定する。

```sh
curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh |
  sh -s -- --version v0.1.0
```

ただし固定してもスクリプト自体はデフォルトブランチから配信され、そこで変更され
うる。誰にも動かせない URL が欲しいパイプラインは、リリースアーカイブを直接
取得するほうがよい。
[`examples/ci/github-actions.yml`](../examples/ci/github-actions.yml) と
[`examples/ci/gitlab-ci.yml`](../examples/ci/gitlab-ci.yml) はそうしている。

### スクリプトをシェルに流し込むことについて

警戒するのはもっともなので、このスクリプトが何をしているかを書いておく。

- トップレベルにあるのは定数と関数定義だけで、`main "$@"` がファイルの最終行。
  通信が途中で切れても**何も実行されない**
- 展開前にリリースの `checksums.txt` と sha256 を照合し、不一致なら何もインストール
  しない
- 書き込むファイルは 1 つだけで、そのパスは表示される
- `sudo` を呼ばず、シェルの設定ファイルにも触らない
- 先に中身を読むのは簡単:
  `curl -fsSL https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/install.sh | less`

## リリースアーカイブ

各リリースは 5 つのアーカイブと、その全部を含む `checksums.txt` を
[Releases](https://github.com/shutx-net/jumping-json-flush/releases) に公開する。
アーカイブは自身と同名のディレクトリに展開され、その中にバイナリと `LICENSE`、
両言語の README が入っている。

```sh
VERSION=v0.1.0
ARCHIVE="jjf_${VERSION}_linux_amd64"
BASE="https://github.com/shutx-net/jumping-json-flush/releases/download/${VERSION}"

curl -sSfL -O "${BASE}/${ARCHIVE}.tar.gz"
curl -sSfL -O "${BASE}/checksums.txt"
# checksums.txt は 5 つ分を含むので、残り 4 つが無いのは想定どおり。
sha256sum --check --ignore-missing checksums.txt

tar xzf "${ARCHIVE}.tar.gz"
sudo install -m 0755 "${ARCHIVE}/jjf" /usr/local/bin/jjf
jjf version
```

`--ignore-missing` は GNU 拡張。busybox や macOS のように使えない環境では、
必要な 1 行だけを残す。

```sh
grep "  ${ARCHIVE}.tar.gz$" checksums.txt > archive.sha256
sha256sum -c archive.sha256      # macOS: shasum -a 256 -c archive.sha256
```

## go install

```sh
go install github.com/shutx-net/jumping-json-flush/cmd/jjf@latest
```

バージョンを固定する場合は `@v0.1.0` のようにタグを指定する。Go 1.24 以上が必要。
この方法で入れたバイナリが表示するのはリリースタグではなく、Go が記録した
モジュールバージョン。

## nix

```sh
nix profile add github:shutx-net/jumping-json-flush
```

`nix run github:shutx-net/jumping-json-flush -- validate db-design.json` なら
インストールせずに実行できる。nix でビルドしたバイナリは
`v<version>+nix.<rev>` を表示する。nix ビルドにはタグを刻むための VCS 情報が
無いため。

## アップグレード

同じコマンドをもう一度実行する。`install.sh` はすでに入っている場所のバイナリを
置き換える。`go install` と nix はそれぞれいつもどおり。

## アンインストール

バイナリを削除するだけ。設定ファイルもキャッシュも作らず、シェルの設定にも触れて
いない。

```sh
rm "$(command -v jjf)"
```
