# Google Workspace domain data source のローカル plan 導線

- Status: `Implemented`
- 作成日: 2026-08-31
- 対象: 既存の `examples/data-sources/googleworkspace_domain/`

## 背景

この provider を利用する既存リポジトリと既存 state はまだない。実 Workspace に対する read-only の動作確認を行うには、provider 開発リポジトリ内の既存 data source 例をそのまま Terraform の root module として使えばよく、新しいリポジトリ、独自 module、一時作業ディレクトリは不要である。

利用者が provider のビルド先や `dev_overrides` を手作業で管理しなくて済むように、リポジトリ側で一連のコマンドを自動化する。

## Goal

1. 既存の `googleworkspace_domain` data source 例を、実際のドメイン名を引数として実行できる root module にする。
2. リポジトリルートの 1 コマンドで、ローカル provider のビルド、開発用 CLI 設定、Terraform の検証・plan を実行する。
3. 利用者の手作業を Google Workspace / Google Cloud 側の認証準備、環境変数設定、コマンド実行だけに限定する。
4. 実 Workspace に対して行う API 操作を domain data source の read のみに限定し、`terraform apply` を導線に含めない。

## Non-goals

- 新しいリポジトリまたは新しい example ディレクトリの作成。
- 独自 Terraform module または `module` block の作成。
- 既存 state、import、state migration の考慮。
- resource の CRUD、`terraform apply`、acceptance test、sweeper の実行。
- provider schema、resource / data source 実装、認証実装の変更。
- `~/.terraformrc` など利用者のグローバル設定の変更。
- provider の配布方法やリリースフローの決定。

## User-owned Actions

利用者が行う作業は次の範囲だけとする。

1. Admin SDK API を利用できる Google Cloud service account credential を用意する。
2. Google Workspace のドメイン全体の委任で service account の OAuth client に `https://www.googleapis.com/auth/admin.directory.domain.readonly` を許可する。
3. なりすまし先に、ドメインを参照できる管理者権限（該当する Domains 権限または Super Admin）を持つユーザーを選ぶ。
4. `GOOGLEWORKSPACE_CREDENTIALS`、`GOOGLEWORKSPACE_CUSTOMER_ID`、`GOOGLEWORKSPACE_IMPERSONATED_USER_EMAIL` を環境変数へ設定する。
5. リポジトリルートで `make plan-domain DOMAIN=<実ドメイン>` を実行し、意図しない変更提案またはエラーがないことを確認する。

credential の内容はリポジトリへ保存せず、agent にも渡さない。

## 実装方針

1. `examples/data-sources/googleworkspace_domain/data-source.tf` の固定値 `example.com` を required variable `domain_name` に置き換える。
2. 同じ既存ディレクトリに次を追加する。
   - `versions.tf`: Terraform `>= 0.14.0` と provider source `hashicorp/googleworkspace` を宣言する。
   - `provider.tf`: OAuth scope を domain read-only の 1 件に限定する。credential、customer ID、impersonated user は既存 provider の環境変数解決を使い、HCL に値を書かない。
   - `README.md`: User-owned Actions だけを、実行順に記載する。
3. `scripts/plan-domain.sh` を追加する。
   - 実行場所に依存せずリポジトリルートと対象 example を解決する。
   - `DOMAIN` と必須環境変数の未設定を API 実行前に検出する。credential の値は出力しない。
   - ambient な `GOOGLE_OAUTH_ACCESS_TOKEN` は無効化し、指定された service account credential と read-only scope を必ず使用する。
   - ambient な `TF_CLI_ARGS`、`TF_CLI_ARGS_version`、`TF_CLI_ARGS_plan`、`TF_CLI_ARGS_validate` は最初の Terraform 呼び出し前に無効化し、version 判定の変更や plan file の保存など意図しない Terraform オプションの追加を防ぐ。
   - `bin/terraform-provider-googleworkspace` へ現在の checkout をビルドする。
   - `bin/googleworkspace-dev.tfrc` を生成し、`hashicorp/googleworkspace` の `dev_overrides` を上記 `bin/` に限定する。利用者のグローバル Terraform CLI 設定は変更しない。
   - CLI 設定は `dev_overrides` だけを記載する。Terraform 公式の開発用挙動に合わせて `terraform init` は実行せず、Registry 接続、公開版 provider の取得、lock file と `.terraform/` の生成を避ける。
   - Terraform `>= 0.14.0` を preflight で確認する。
   - `TF_CLI_CONFIG_FILE` を当該ファイルに限定し、対象 example で `terraform validate`、`terraform plan -input=false -var=domain_name=...` を順に実行する。
   - plan file は保存せず、`apply` は実行しない。
4. `GNUmakefile` に `plan-domain` target を追加し、`DOMAIN` を script に渡す。
5. example の変更後に `make generate` を実行し、生成 docs の差分を含める。

## 主な touched files

- `docs/plans/20260831-plan-domain-data-source.md`
- `GNUmakefile`
- `scripts/plan-domain.sh`
- `examples/data-sources/googleworkspace_domain/data-source.tf`
- `examples/data-sources/googleworkspace_domain/provider.tf`
- `examples/data-sources/googleworkspace_domain/versions.tf`
- `examples/data-sources/googleworkspace_domain/README.md`
- `docs/data-sources/domain.md`（`make generate` による生成物）

## Question classification

### Default Decisions

これまでの人間レビューで次を決定済みとする。

1. 新設リポジトリ・一時ディレクトリ・独自 module は作らず、既存の `examples/data-sources/googleworkspace_domain/` を root module として再利用する。
2. 既存 state はなく、考慮しない。
3. 実 Workspace 操作は read-only domain data source の plan のみとし、apply / CRUD は行わない。
4. OAuth scope は `https://www.googleapis.com/auth/admin.directory.domain.readonly` だけに限定する。
5. 利用者の実行コマンドは、リポジトリルートの `make plan-domain DOMAIN=<実ドメイン>` とする。
6. credential は環境変数で受け取り、リポジトリには保存しない。
7. なりすまし先には、ドメインを参照できる管理者権限を持つユーザーを使う。
8. `terraform init` は実行しない。`validate` / `plan` は `dev_overrides` のローカル provider を直接使用し、Registry への接続と公開版 provider の取得を行わない。

### Human Decisions Required

なし。

### Agent-resolvable Decisions

1. shell script のエラーメッセージと preflight の細部。
2. Make target と script 間の引数受け渡し方法。
3. Terraform / Go CLI の存在確認方法。
4. README の表現と公式ドキュメントへのリンク。

## Stop condition

`.agents/docs/planning-review.md` の標準 stop condition に委譲する。特に次の場合は停止する。

- agent が実 Workspace credential を使う必要が生じた場合。
- `make testacc`、`make sweep`、`TF_ACC=1` が必要になった場合。
- provider schema または認証実装の変更が必要になった場合。
- `terraform plan` 以外の実 Workspace 操作が必要になった場合。
- verifier が同一原因で 3 回失敗した場合。

## Verifier

- `bash -n scripts/plan-domain.sh`
- 必須値なしで `make plan-domain` を実行し、build や API access より前に安全に失敗すること。
- Terraform `0.14.0` 未満を拒否する version preflight を script のテスト可能な関数または境界値比較で確認すること。
- `terraform fmt -check -recursive examples/data-sources/googleworkspace_domain`
- 初期化済みファイルがない状態で、開発用 CLI 設定による `terraform validate` が成功すること。
- 初期化済みファイルがない状態で、無効なダミー credential を使った `terraform plan` がローカル provider の credential 読み込みまで到達し、Registry / provider installation エラーにならないこと。実 Workspace を読む plan は credential を持つ利用者が実行する。
- `make generate` を実行し、`docs/data-sources/domain.md` が example の変更だけを反映すること。
- `go mod verify`
- `go vet ./...`
- `make build`
- `make test`
- `make lint`
- fresh context code review で P0 / P1 finding がないこと。

## AI critical review 履歴

### 2026-08-31 初回（codex exec / gpt-5.6-sol）

| # | Finding | 対応 |
|---|---|---|
| 1 | [P1] `dev_overrides` と `terraform init` の組み合わせで、公開版 provider の取得方法・ネットワーク依存・Terraform 最低 version が未定義 | 実機検証で init 不要を確認し、最終方針では init 自体を省略。Terraform `>= 0.14.0` の宣言・preflight と、初期化なしの validate / plan verifier を追加 |
| 2 | [P1] OAuth scope だけでは不十分で、なりすまし先ユーザーの管理者権限が手順から欠落 | User-owned Actions と Default Decisions に、Domains 権限または Super Admin を持つユーザーが必要と追記 |

### 2026-08-31 再レビュー（fresh codex exec / gpt-5.6-sol）

前回 2 件の解消を確認。critical issue / implementation blocker は残っておらず、`Ready for Implementation` の判定を得た。その後の実機検証で Terraform が `dev_overrides` 利用時の init スキップを明示的に推奨したため、Registry 依存を受け入れる案から init を省略する案へ更新し、再レビュー対象とした。

### 2026-08-31 no-init 方針の最終再レビュー（fresh codex exec / gpt-5.6-sol）

初期化済みファイルのない状態で `validate` が成功し、ダミー credential の `plan` がローカル provider の read 経路まで到達することを確認。Registry / provider installation エラー、`.terraform/`、lock file、state の生成はなく、critical issue / implementation blocker は残っていないとの判定を得た。

### 2026-08-31 実装後 code review（fresh codex exec / gpt-5.6-sol）

実装後の review loop で次の P1 finding を検出し、すべて修正した。

| # | Finding | 対応 |
|---|---|---|
| 1 | ambient な `GOOGLE_OAUTH_ACCESS_TOKEN` が指定 credential と read-only scope を迂回する | provider 実行前に unset |
| 2 | ambient な `TF_CLI_ARGS` 系が plan file 保存などの挙動を追加できる | version / validate / plan を含む対象変数を最初の Terraform 呼び出し前に unset |
| 3 | provider build が呼び出し元ディレクトリに依存する | subshell で repository root に移動して `go build` |

mock Terraform による環境変数遮断の確認、および `/tmp` から実スクリプトを呼ぶ検証を実施した。後者はローカル provider の credential 読み込みまで到達し、caller working directory、Registry、provider installation、plan file 保存の問題がないことを確認した。

実装全体では `make generate`、`go mod verify`、`go vet ./...`、`make build`、`make test` が成功した。ローカルに入っていた `golangci-lint` は Go 1.26 で build されており、Go 1.27 の module を読み込む前に停止したため、同じ pinned version の `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/provider` を実行し、`0 issues` を確認した。acceptance test、sweeper、実 credential は使用していない。
