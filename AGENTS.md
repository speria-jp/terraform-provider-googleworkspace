# AGENTS.md - Terraform Provider Google Workspace Agent Instructions

このファイルは AI agent がこのリポジトリで作業するための指示書です。

## このリポジトリについて

- upstream の [hashicorp/terraform-provider-googleworkspace](https://github.com/hashicorp/terraform-provider-googleworkspace) はアーカイブ済み。この fork は speria-jp がメンテナンスする
- Terraform Registry の community provider `speria-jp/googleworkspace` として公開する
- Go 製、terraform-plugin-sdk v2 ベースの Terraform provider

## 基本方針

- `AGENTS.md` は入口に留める。詳細な規約は `.agents/docs/`、実装計画は `docs/plans/` を読む
- `docs/` は `go generate` による生成物。**直接編集しない**（編集するのは `templates/` と `examples/`）。例外は `docs/plans/`（手書きの実装計画置き場。tfplugindocs の生成対象外）
- 非自明な変更は `.agents/docs/planning-review.md` に従って plan を作成してから実装する

## 構造 index

| パス | 内容 |
|---|---|
| `internal/provider/` | provider 実装の全て（フラット構成）。`resource_*.go` / `data_source_*.go` と対応する `*_test.go` |
| `internal/provider/provider.go` | provider 定義。resource / data source の登録箇所 |
| `internal/provider/eventual_consistency.go` | Google API の結果整合性を吸収する仕組み |
| `templates/` | ドキュメントの編集元（tfplugindocs テンプレート） |
| `examples/` | ドキュメントに埋め込まれる HCL サンプル |
| `docs/` | 生成物（Terraform Registry 形式のドキュメント）。`docs/plans/` を除き直接編集禁止 |
| `docs/plans/` | 実装計画（`YYYYMMDD-<topic>.md`）。手書き。生成対象外 |
| `.agents/docs/planning-review.md` | plan の status lifecycle、レビューフロー、AI critical review |
| `.agents/docs/code-review.md` | code review の観点と出力形式 |
| `RELEASING.md` | release runbook。GPG 鍵と secrets の初回設定、preflight、tag、finalize、Registry publish の手順 |

## 開発コマンド

| コマンド | 内容 |
|---|---|
| `make build` | fmtcheck + `go install` |
| `make test` | unit test（`-timeout=30s`） |
| `make lint` | golangci-lint（`internal/provider` 対象） |
| `make fmt` | `gofmt -w -s` |
| `make generate` | ドキュメント生成（`templates/` + `examples/` → `docs/`） |

## 実行してはいけないコマンド

明示的に依頼されない限り、以下は実行しない。

- `make testacc`（および `TF_ACC=1` を伴う `go test`）: 実際の Google Workspace 組織にリソースを作成・変更・削除する acceptance test。実環境と課金に影響する
- `make sweep`: テスト用リソースの一括削除。インフラ破壊コマンド

## Git / コミット運用

- コミットメッセージは Conventional Commits 形式に統一する
- 基本形は `<type>(<scope>): <summary>`。`scope` は省略可（付ける場合は resource 名や `docs` / `ci` など変更対象領域）
- `type` は原則 `feat` / `fix` / `refactor` / `docs` / `test` / `chore` / `perf` / `ci` を使う
- 破壊的変更は `!` を付け、必要に応じて本文に `BREAKING CHANGE:` を書く
- summary は英語の命令形または短い動詞句で書き、末尾に句点を付けない

## Terraform provider 固有の注意

- **schema 変更は既存 state / 既存利用側との互換性に直結する**。`ForceNew` の追加、型変更、Required 化、attribute 削除は breaking change であり、plan で明示的に合意しない限り行わない
- `ForceNew` が付いた field の変更は、apply 時に**実リソースの削除と再作成**を引き起こす。ユーザーやグループのような実体では特に慎重に扱う
- Google Directory API は eventual consistency がある。作成直後の読み取りには `eventual_consistency.go` の既存パターンを使う
- `templates/` や schema の `Description` を変更したら `make generate` を実行し、`docs/` の差分もコミットに含める
