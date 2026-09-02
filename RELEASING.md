# Release runbook

`speria-jp/googleworkspace` community provider の GitHub Release 作成と Terraform Registry publish の手順書。
背景と決定事項は `docs/plans/20260901-community-release.md` を参照する。

## 役割分担

- GPG 鍵の生成、Registry への鍵登録、repository secrets の登録、Registry の publish 操作は人間が行う。
- agent は keypair、passphrase、secret、Registry credential を作成、閲覧、保存しない。
- preflight の起動、CHANGELOG の PR、tag push、draft の検証、finalize、consumer 検証、homepage 更新は agent に依頼できる。

必要な権限は次の通り。

- GitHub org `speria-jp` の admin（Registry の Signing Keys で org namespace を選ぶため）
- repository `speria-jp/terraform-provider-googleworkspace` の admin（secrets 登録と tag push のため）

## 1. 初回だけ行う設定

### 1.1 鍵種別

RSA 4096 bit を使う。Terraform Registry は RSA と DSA を受け付け、GnuPG の既定である ECC は拒否する。
DSA を選ばない理由は次の通り。

- GnuPG が生成できる DSA は 3072 bit までで、RSA 4096 より強くならない。
- DSA 署名は署名ごとの乱数に安全性が依存し、乱数の再利用や偏りで秘密鍵が復元される。RSA にこの失敗モードはない。
- FIPS 186-5 は署名生成での DSA を承認から外し、OpenSSH は DSA 対応を削除した。OpenPGP の現行仕様 RFC 9580 は Section 12.5 で「an implementation MUST NOT generate DSA keys」と規定している。

### 1.2 鍵の生成

手元の GnuPG は 2.5 系。`~/.gnupg` に鍵が増えるだけで、外部には何も送られない。

passphrase の入力には GUI の `pinentry-mac` を使う。Homebrew の既定である curses 版 pinentry は端末を掴めないと passphrase 画面を出せず、`gpg: agent_genkey failed: Timeout` で鍵生成が失敗する。設定後は gpg-agent を再起動して反映する。

```
brew install pinentry-mac
echo "pinentry-program /opt/homebrew/bin/pinentry-mac" >> ~/.gnupg/gpg-agent.conf
gpgconf --kill gpg-agent
```

鍵生成は Terminal.app や iTerm2 で直接実行する。tmux や他ツールの内蔵シェル越しだと pinentry が入力を受け取れないことがある。

```
gpg --full-generate-key
```

各プロンプトへの入力は次の通り。

| プロンプト | 入力 | 補足 |
|---|---|---|
| `Please select what kind of key you want:` | `4` | `RSA (sign only)`。署名専用の最小権限の鍵にする。`1` の `RSA and RSA` でも動作する |
| `What keysize do you want? (3072)` | `4096` | |
| `Key is valid for? (0)` | `0` | 無期限。鍵が期限切れになると、Registry が配る公開鍵での検証に失敗して公開済み version の `terraform init` まで巻き込む恐れがある。期限を付けるなら更新運用を先に決める |
| `Is this correct? (y/N)` | `y` | |
| `Real name:` | `speria-jp Terraform Provider Release` | 個人名ではなく組織の release 用鍵と分かる名前。5 文字以上が必須 |
| `Email address:` | 組織で管理しているメールアドレス | 個人アドレスは避ける。公開鍵の一部として Registry に載る |
| `Comment:` | 空のまま Enter | |
| `Change (N)ame, (C)omment, (E)mail or (O)kay/(Q)uit?` | `O` | |
| pinentry-mac の passphrase ダイアログ | 十分に長い passphrase | password manager に保存する。後で secret `PASSPHRASE` に同じ値を入れる。`Save in Keychain` のチェックは外す |

生成後に fingerprint を控える。

```
gpg --list-secret-keys --keyid-format long
```

`sec   rsa4096/...` の次の行にある 40 桁の 16 進文字列が fingerprint。以降のコマンドで使う。

```
export FPR=<40 桁の fingerprint>
```

### 1.3 公開鍵を Terraform Registry に登録する

```
gpg --armor --export "$FPR" | pbcopy
```

1. https://registry.terraform.io に GitHub で sign in する。
2. 右上のユーザーメニューから User Settings を開き、Signing Keys を選ぶ。URL は https://registry.terraform.io/settings/gpg-keys 。
3. New GPG Key で Namespace に `speria-jp` を選び、クリップボードの公開鍵を貼り付けて保存する。

Namespace に `speria-jp` が出ない場合は、org admin でないか、Terraform Registry の OAuth app に org へのアクセスが許可されていない。
後者は GitHub の Settings > Applications > Authorized OAuth Apps > Terraform Registry で `speria-jp` への Organization access を Grant する。

### 1.4 repository secrets を登録する

秘密鍵はファイルに書き出さず、gh に直接渡す。export 時に passphrase を聞かれる。

```
gpg --armor --export-secret-keys "$FPR" | gh secret set GPG_PRIVATE_KEY --repo speria-jp/terraform-provider-googleworkspace
gh secret set PASSPHRASE --repo speria-jp/terraform-provider-googleworkspace
gh secret list --repo speria-jp/terraform-provider-googleworkspace
```

`PASSPHRASE` は対話プロンプトに貼り付ける。入力は表示されない。
最後の `gh secret list` で `GPG_PRIVATE_KEY` と `PASSPHRASE` の 2 行が出れば完了。

### 1.5 バックアップ

次の 3 つを組織の password manager に保存する。repository、issue、chat には貼らない。

- `gpg --armor --export-secret-keys "$FPR"` の出力
- passphrase
- 失効証明書 `~/.gnupg/openpgp-revocs.d/<FPR>.rev`

秘密鍵を失っても、新しい鍵を生成して Registry に追加登録すれば以後の release は継続できる。公開済み version は Registry が保持する旧公開鍵で検証され続ける。

## 2. version ごとの release 手順

`v0.8.0` を例にする。以降は version を読み替える。

### 2.1 事前条件

- `CHANGELOG.md` の `## 0.8.0 (Unreleased)` 節に内容が入っている。
- `main` の CI が green。
- secrets `GPG_PRIVATE_KEY` と `PASSPHRASE` が存在する。

### 2.2 署名 preflight

Release を作らずに、snapshot build と GPG 署名だけを CI で検証する。

```
gh workflow run release.yml --repo speria-jp/terraform-provider-googleworkspace --ref main
gh run list --repo speria-jp/terraform-provider-googleworkspace --workflow release.yml --limit 1
gh run watch <run-id> --repo speria-jp/terraform-provider-googleworkspace --exit-status
```

`signing-preflight` job が `Verify detached checksum signature` まで成功すれば通過。`release` job はこの起動では skip される。
失敗した場合は secrets か鍵の問題なので、直してから再実行する。preflight が通るまで tag は発行しない。

### 2.3 CHANGELOG の日付を確定する PR

`## 0.8.0 (Unreleased)` を既存の書式に合わせて `## 0.8.0 (September 2, 2026)` のように書き換え、PR で `main` に merge する。
commit message は `docs(changelog): release 0.8.0` とする。

### 2.4 tag の発行

tag は必ず merge 済みの `main` に対して git から push する。GitHub UI の Release 作成で tag を作ると、workflow の draft 作成と衝突する。

```
git switch main
git pull --ff-only origin main
git log --oneline -1
git tag -a v0.8.0 -m "v0.8.0"
git push origin v0.8.0
gh run list --repo speria-jp/terraform-provider-googleworkspace --workflow release.yml --limit 1
gh run watch <run-id> --repo speria-jp/terraform-provider-googleworkspace --exit-status
```

`git log` で CHANGELOG PR の merge commit が HEAD であることを確認してから tag を打つ。
成功すると draft の GitHub Release `v0.8.0` が作られる。

### 2.5 draft の検証

```
gh release view v0.8.0 --repo speria-jp/terraform-provider-googleworkspace
```

asset が次の構成であることを確認する。

- `terraform-provider-googleworkspace_0.8.0_<os>_<arch>.zip` が 14 個（darwin 2、linux 5、freebsd 4、windows 3）
- `terraform-provider-googleworkspace_0.8.0_manifest.json`
- `terraform-provider-googleworkspace_0.8.0_SHA256SUMS`
- `terraform-provider-googleworkspace_0.8.0_SHA256SUMS.sig`

署名と checksum を手元で検証する。

```
cd "$(mktemp -d)"
gh release download v0.8.0 --repo speria-jp/terraform-provider-googleworkspace --pattern '*SHA256SUMS*' --pattern '*darwin_arm64.zip'
gpg --verify terraform-provider-googleworkspace_0.8.0_SHA256SUMS.sig terraform-provider-googleworkspace_0.8.0_SHA256SUMS
shasum -a 256 -c --ignore-missing terraform-provider-googleworkspace_0.8.0_SHA256SUMS
unzip -l terraform-provider-googleworkspace_0.8.0_darwin_arm64.zip
```

`gpg --verify` が `Good signature` で登録した鍵の fingerprint を示し、`shasum` が `OK`、zip に `terraform-provider-googleworkspace_v0.8.0` と `LICENSE.txt` が入っていれば合格。

### 2.6 draft の finalize

ここで初めて public release になる。finalize 後の asset は置換しない。修正が必要なら新しい version を切る。
release note は CHANGELOG の該当節をそのまま使う。

```
awk '/^## 0\.8\.0/{f=1; next} /^## /{f=0} f' CHANGELOG.md > notes.md
gh release edit v0.8.0 --repo speria-jp/terraform-provider-googleworkspace --draft=false --latest --notes-file notes.md
gh release view v0.8.0 --repo speria-jp/terraform-provider-googleworkspace
```

### 2.7 Terraform Registry への初回 publish

初回だけ人間が行う。2 回目以降は Registry が作る webhook が release event を受けて自動で取り込む。

1. https://registry.terraform.io に GitHub で sign in する。
2. 右上の Publish > Provider を選ぶ。
3. organization `speria-jp`、repository `terraform-provider-googleworkspace` を選ぶ。
4. Registry が最新 release の asset と登録済み鍵を検証する。問題がなければ terms of use に同意して Publish する。

取り込みを確認する。

```
curl -s https://registry.terraform.io/v1/providers/speria-jp/googleworkspace/versions | jq '.versions[].version'
```

### 2.8 consumer 検証

空のディレクトリで provider を取得し、署名検証付きで入ることを確認する。

```
cd "$(mktemp -d)"
cat > versions.tf <<'HCL'
terraform {
  required_providers {
    googleworkspace = {
      source  = "speria-jp/googleworkspace"
      version = "~> 0.8"
    }
  }
}
HCL
terraform init
```

`Installed speria-jp/googleworkspace v0.8.0 (self-signed, key ID <登録した鍵の key ID>)` が出れば合格。

### 2.9 後処理

- repository の homepage を Registry の新ページに変える。

```
gh repo edit speria-jp/terraform-provider-googleworkspace --homepage https://registry.terraform.io/providers/speria-jp/googleworkspace/latest
```

- 初回のみ: `README.md` の「After the first Registry release」の文言を更新し、`docs/plans/20260901-community-release.md` の status を `Implemented` にする。
- `CHANGELOG.md` に次の `## 0.9.0 (Unreleased)` 節を追加する。

## 3. 失敗時の扱い

- preflight の失敗: Release も tag も作られていないので、原因を直して再実行する。
- draft 作成の失敗: finalize 前の draft と tag に限り、対象と理由を示して人間の確認を得た上で削除し、同じ version で再試行できる。

```
gh release delete v0.8.0 --repo speria-jp/terraform-provider-googleworkspace --yes
git push origin :refs/tags/v0.8.0
git tag -d v0.8.0
```

- finalize 後の不具合: release を削除、置換しない。修正を含む新しい patch version を切る。
- 鍵の漏えい: 失効証明書で鍵を失効させ、新しい鍵を生成して 1.3 と 1.4 をやり直す。
