# speria-jp fork の Terraform Registry 公開

- Status: `Implemented`
- 作成日: 2026-09-01
- 関連: `speria-jp/admin` の `docs/plans/20260901-google-workspace-root.md`

## 背景

`speria-jp/admin` では Google Workspace の users / groups を Terraform 管理する方針が人間レビュー済みであり、provider source を `speria-jp/googleworkspace`、version を `~> 0.8` とすることが決定した。現在の fork は公開リポジトリだが GitHub Release と Terraform Registry の provider が存在せず、残置された release workflow は HashiCorp 社内 secret に依存するため fork では動作しない。

本リポジトリの従来方針「Terraform Registry へ publish しない」は、上記 admin plan とその実装依頼によって「community provider として publish する」へ変更されたものとして扱う。

## Goal

1. 公式の community provider 向け GoReleaser 構成で、署名済み GitHub Release を tag push から作成できるようにする。
2. 最初の fork release `v0.8.0` を Terraform Registry の `speria-jp/googleworkspace` として公開できる状態にする。
3. 公開後、consumer が `terraform init` で署名検証済み provider を取得できることを確認する。

## Non-goals

- provider schema、resource / data source、認証実装の変更。
- acceptance test、sweeper、実 Workspace に対する CRUD。
- GitHub Actions から Google Workspace へ接続する認証の追加。
- GPG private key、passphrase、Terraform Registry credential の repository 保存。
- `v0.8.0` より後の自動 version bump や changelog 生成の導入。

## Strategy

1. HashiCorp の provider scaffolding と公式 publishing guide に合わせ、repository 内で GoReleaser を直接実行する community workflow を採用する。
2. workflow 内の third-party actions は immutable commit SHA で pin し、権限は `contents: write` のみに限定する。
3. `GPG_PRIVATE_KEY` と `PASSPHRASE` は GitHub Actions secrets から読み、release asset と log に秘密値を出さない。
4. tag は PR merge 後の `main` commit に対してだけ発行し、GitHub Release は最初に draft として作る。finalize 済みの release asset は置換しない。`v0.8.0` の draft 作成に失敗した場合は、公開前の draft / tag に限り、人間の明示確認後に削除・再作成して同 version を再試行できる。finalize 後の修正は必ず新しい version とする。
5. `v0.8.0` は upstream `v0.7.0` の続番とし、fork で実施した Go / dependency / CI の更新と keyless local plan 導線を changelog に記録する。
6. Go module path は既存 import と履歴の互換性を優先して `github.com/hashicorp/terraform-provider-googleworkspace` のままとする。一方、Terraform provider address と公開リポジトリの案内はすべて `speria-jp/googleworkspace` / `speria-jp/terraform-provider-googleworkspace` へ移行する。

## Implementation

1. `AGENTS.md` の配布方針と TODO を community provider 公開方針へ更新する。
2. `.github/workflows/release.yml` を公式 scaffolding と同型の workflow へ差し替える。
   - trigger: `workflow_dispatch` の署名 preflight と `v*` tag push の draft release
   - checkout: full history
   - Go: `go.mod` の version
   - GPG import: `GPG_PRIVATE_KEY` / `PASSPHRASE`
   - preflight: `release --snapshot --clean` で同じ key import と署名処理を検証し、release は作成しない
   - tag: `release --clean` で draft release を作成する
3. `.goreleaser.yml` を version 2 形式へ更新する。
   - provider binary は `terraform-provider-googleworkspace_v{{ .Version }}`
   - archive、manifest、checksum、detached GPG signature を Terraform Registry の命名規則に合わせる
   - ldflags は `main.version` に release version を設定する
4. public identity を fork に合わせる。
   - `README.md`: archived / no-release 表示、maintainer、badge、issue / contribution / Registry link を fork に更新する
   - `examples/data-sources/googleworkspace_domain/versions.tf`: source を `speria-jp/googleworkspace` にする
   - `scripts/plan-domain.sh`: `dev_overrides` の address を `speria-jp/googleworkspace` にする
   - `main.go`: debug address を `registry.terraform.io/speria-jp/googleworkspace` にする
   - release 後: GitHub repository の homepage を新しい Registry page に更新する
5. `CHANGELOG.md` の `0.8.0 (Unreleased)` を release 内容で埋める。日付は tag 発行時に確定する。
6. code verifier と GoReleaser の構成検証を実行し、PR を merge する。
7. User-owned Actions 1〜4 の完了後、`workflow_dispatch` の署名 preflight を成功させる。
8. merge 済み `main` に `v0.8.0` tag を発行し、draft GitHub Release の artifact と署名を検証する。
9. draft を finalize した後、Terraform Registry で初回 publish する。
10. Registry ingestion 後、consumer fixture の `terraform init` で `speria-jp/googleworkspace` v0.8.0 を取得・検証する。

## User-owned Actions

1. RSA または DSA の release 用 GPG keypair を用意する（Terraform Registry は既定の ECC key を受け付けない）。
2. public key を Terraform Registry の `speria-jp` namespace に登録する。
3. private key と passphrase を repository secrets `GPG_PRIVATE_KEY` / `PASSPHRASE` に登録する。
4. Terraform Registry に GitHub で sign in し、`speria-jp` namespace で provider を publish できる権限を確認する。初回 publish 自体は GitHub Release finalize 後に行う。

agent は keypair・secret・Registry credential を作成、閲覧、保存しない。

## Question classification

### Default Decisions

`speria-jp/admin` の人間レビュー済み plan と今回の実装依頼により、次を決定済みとする。

1. fork を Terraform Registry の community provider `speria-jp/googleworkspace` として公開する。
2. 最初の version は `v0.8.0` とする。
3. release signing は GPG を使い、秘密値は GitHub Actions secrets に保存する。
4. GitHub Actions workflow の community release への差し替えを実施する。

### Human Decisions Required

なし。

### Agent-resolvable Decisions

1. 公式 workflow を転記する際の action commit SHA と GoReleaser version 2 構文の調整。
2. changelog の分類と文言。
3. `v0.8.0` の release note 生成方法。既存 changelog からの抽出が初回 tag で不安定な場合は GoReleaser の既定 changelog または固定 release note を使う。

## Stop condition

- GPG public key の Registry 登録または repository secrets の存在を確認できない状態では tag を発行しない。
- Terraform Registry の `speria-jp` namespace で publish 権限を確認できない場合は初回 publish を行わない。
- `workflow_dispatch` の署名 preflight が成功していない状態では tag を発行しない。
- release workflow が `contents: write` を超える権限、新しい long-lived token、または HashiCorp 社内 secret を要求する場合は停止する。
- release artifact の checksum / signature / manifest が公式要件を満たさない場合は Registry publish を進めない。
- `v0.8.0` の draft / tag を削除・再作成する必要が生じた場合は、対象と理由を示して実行直前に人間確認を得る。finalize 済み release は削除・置換しない。
- schema 変更、実 Workspace credential、acceptance test、sweeper が必要になった場合は停止する。
- verifier が同じ理由で 3 回失敗した場合は停止する。

## Verifier

- `go mod verify`
- `go vet ./...`
- `make build`
- `make test`
- `make lint`
- `make generate` を実行し、生成差分がないこと
- `goreleaser check`
- `goreleaser release --snapshot --clean` を実行し、binary / zip / manifest / checksum の名前と対象 platform を確認する（署名と GitHub upload は行わない）
- `workflow_dispatch` preflight が repository secrets の GPG key / passphrase を import し、`SHA256SUMS.sig` を生成できること
- workflow YAML が preflight / tag trigger、最小権限、GPG secret、pinned actions、draft release を満たすことを review する
- `rg 'hashicorp/googleworkspace|registry\.terraform\.io/providers/hashicorp/googleworkspace|github\.com/hashicorp/terraform-provider-googleworkspace' README.md main.go scripts examples` の残件が、意図的に維持する Go module import または upstream 履歴参照だけであること
- PR の CI が green であること
- release 後、GitHub Release に zip / manifest / `SHA256SUMS` / `SHA256SUMS.sig` が存在すること
- release 後、空の consumer fixture で `terraform init` が `speria-jp/googleworkspace` v0.8.0 を署名検証付きで取得できること

## 更新履歴

### 2026-09-01 初版

`speria-jp/admin` の Google Workspace Terraform root 実装に必要な provider 公開手順を、公式 publishing guide と provider scaffolding に合わせて具体化した。

### 2026-09-01 AI critical review cycle 1 反映

初回公開の循環を解消し、順序を「key / secret / 権限確認 → 署名 preflight → tag → draft release 検証 → finalize → Registry 初回 publish → consumer test」とした。公開 identity の移行対象に README、example の provider source、dev override、debug address、repository metadata を追加した。`v0.8.0` は draft / tag の公開前だけ人間確認付きで再試行可能、finalize 後は immutable とする失敗方針を明記した。

### 2026-09-01 AI critical review cycle 2: クリア

前回 3 finding の解消を確認し、「No critical issues remain.」「plan can move to `Ready for Implementation`」の判定を受けた。

### 2026-09-02 実装状況の記録

Implementation 1〜6 は PR #3（2026-09-01 merge）で完了した。残りは User-owned Actions 1〜4 と Implementation 7〜10。人間が実行するコマンドと GPG 鍵生成時の選択項目、version ごとの release 手順を `RELEASING.md` に runbook として切り出した。runbook では鍵種別を RSA 4096 とする（DSA は GnuPG で 3072 bit 上限かつ deprecated 扱いのため）。

### 2026-09-02 実装完了

署名 preflight の成功後に `v0.8.0` tag を発行し、draft の asset、署名、checksum を検証して finalize した。Terraform Registry の `speria-jp/googleworkspace` として初回 publish し、release event の webhook が作成された。空の consumer fixture で `terraform init` が v0.8.0 を署名検証付き（key ID `0EF194ED7615F8F2`）で取得することを確認し、repository の homepage を Registry の新ページへ更新した。
