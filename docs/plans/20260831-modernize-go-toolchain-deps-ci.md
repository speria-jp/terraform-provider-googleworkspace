# 技術スタックのモダン化（Go / 依存パッケージ / 開発ツール / CI）

- Status: `Implemented`
  - 補足: 2026-08-31 に実装とローカル verifier を完了。実 Workspace の read-only スモークテストは、PR merge 前に credential を持つ人間が実施するゲートとして残す。
- 作成日: 2026-08-31
- 関連: AGENTS.md の TODO（Go 更新、golangci-lint / goreleaser / GitHub Actions のモダン化）

## 背景

このリポジトリは upstream（hashicorp/terraform-provider-googleworkspace、アーカイブ済み）の fork であり、技術スタックが 2022 年頃の状態で止まっている。さらに 2026-08-31 時点の調査で、**main ブランチはビルド・テスト・lint がすべて失敗する状態**であることが判明した。単なる「古い」ではなく「壊れている」が起点である。

### 壊れているもの（調査で確認済みの事実）

1. **main がコンパイル不能**。Dependabot PR #504（`google.golang.org/api` v0.79.0 → v0.226.0）で chromepolicy API の型名が `GoogleChromePolicyV1*` から `GoogleChromePolicyVersionsV1*` に変わったが、コードが未追随。`internal/provider/resource_chrome_policy.go`、`data_source_chrome_policy_schema.go`、`resource_chrome_policy_test.go` の計約 25 箇所が undefined になる。
2. **go.mod / go.sum が不整合**。`go 1.16` directive のまま新しい依存が入っており、`go build` / `make test` が `missing go.sum entry` で即失敗する。`go mod tidy` を実行すると依存グラフの要求により directive は自動的に `1.23.0` 以上へ引き上げられる（= `go 1.16` はもはや維持不能）。
3. **`make lint` が実行不能**。`.golangci.yml` は golangci-lint v1 形式だが、開発環境の golangci-lint は v2 系（v2.10.1）であり、設定バージョン非対応エラーで落ちる。
4. **CI が実質機能していない**。`unit_tests.yaml` は HashiCorp 社内の Vault（`secrets.VAULT_ADDR` / `ROLE_ID` / `SECRET_ID`）から credential を取得する前提で、fork にはこれらの secrets が存在しないため常に失敗する。加えてトリガーが `paths: ['**.go']` のため、go.mod / go.sum のみを変更する Dependabot PR ではそもそも走らない。この 2 点の結果、コンパイル不能な #504 が検知されずにマージされた。
5. **Dependabot は daily で稼働中**。CI が機能しないまま放置すると、今後も壊れた更新が入り続ける構造的リスクがある。

### 現状バージョンと最新バージョン（2026-08-31 調査時点）

| 項目 | 現状 | 最新 | 本 plan での目標 |
|---|---|---|---|
| Go（go.mod directive） | 1.16（+ `toolchain go1.24.1`） | 1.27 | 1.27.0（Default Decision 1） |
| Go（`.go-version` / CI） | 1.16.2 / 1.16・1.18 | 1.27.0（1.26.7 / 1.25.14 がサポート中） | 1.27.0（Default Decision 1） |
| terraform-plugin-sdk/v2 | v2.16.0 | v2.40.1（v2.40.0 で Go 1.25 要求） | v2.40.1 |
| terraform-plugin-go（間接） | v0.9.0 | SDK 追随 | SDK 追随で自動更新 |
| terraform-plugin-docs | v0.21.0 | v0.25.0 | v0.25.0 |
| google.golang.org/api | v0.226.0（コード未追随） | v0.295.0 | 最新 |
| golang.org/x/oauth2 ほか x/ 系 | v0.28.0 ほか | - | 最新 |
| golangci-lint | 設定は v1 形式 | v2.13.2 | v2 系 CLI + v2 設定 |
| goreleaser | 設定は v1 形式 | v2.18.0 | **scope 外**（配布方法未決のため） |
| actions/checkout | v4 | v6 | v6 |
| actions/setup-go | v5.3.0 | v6 | v6 + `go-version-file` |
| hashicorp/vault-action | v3.3.0 | - | 削除（fork では使用不能） |
| ghaction-terraform-provider-release | v5.0.0 | - | scope 外（残置を決定、Default Decision 3） |

バージョン番号は Web 調査に基づく。実装時に `go list -m -versions <module>` と各リリースノートで最終確認すること。

### コード側の影響範囲（調査済み）

- chromepolicy 型リネーム: 旧 `GoogleChromePolicyV1*` は v0.226.0 に存在せず（0 件）、使用中の全 10 型に `GoogleChromePolicyVersionsV1*` の 1:1 対応が存在することを module cache で確認済み。機械的リネームで対応可能な見込み。ただしフィールド構造の同一性は実装時に要確認。
- `helper/resource` の retry 系 API（`resource.RetryContext` / `RetryableError` / `RetryError`）を `retry_utils.go` / `retry_transport.go` で使用。SDK v2.18.0 で `helper/retry` へ移動し旧位置は deprecated（動作はする）。
- `github.com/hashicorp/go-cty` を `provider.go` / `resource_role.go` / `resource_role_assignment.go` / `resource_group_members.go` が直接 import。SDK v2 が同 fork を使い続ける限り維持が必要。
- `mitchellh/go-homedir` / `hashicorp/errwrap` / `go-cleanhttp` も直接依存（`provider.go` / `retry_utils.go` / `utils.go` / `provider_config.go`）。置き換えは本 plan の scope 外。

## Goal

1. main ブランチを「クリーン checkout から `make build` / `make test` / `make lint` / `make generate` がすべて成功する」状態に戻す。
2. Go・主要依存（terraform-plugin-sdk/v2、google.golang.org/api ほか）・golangci-lint・GitHub Actions を最新（または最新に近い）バージョンへ更新する。
3. fork で実際に機能する CI（unit test + build + lint）を確立し、以後の依存更新が自動検証される状態にする。

## Non-goals

- リリースフローの再構築（goreleaser v2 化、release.yml の再設計、タグ発行、バイナリ配布）。社内配布方法が未決のため、決定後の別 plan とする。
- upstream / community fork の未取り込み変更の取り込み。
- schema 変更、resource / data source の機能追加・変更。provider の外部挙動（schema、CRUD の意味論）を意図的に変える変更は行わない。依存更新による非意図的な挙動変化は実環境スモークテスト（Default Decision 4）で検証する。
- 直接依存の小物置き換え（go-homedir → `os.UserHomeDir` など）。動くものを壊さない方針を優先し、必要なら別 plan。
- acceptance test（`make testacc` / `TF_ACC=1`）の実行、および **agent による**実 Workspace 組織を使う操作。ただし Default Decision 4 の read-only スモークテスト（人間が実施し、credential は agent に渡さない）は例外として本 plan の scope に含む。

## 戦略（基本原則)

1. **修復 → 防御 → 更新 → 磨き込み の順で進める**。まずビルドを直し（Phase 1）、次に CI を機能させて以後の変更が検証される状態を作り（Phase 2）、その保護下で大きい依存更新を行い（Phase 3）、最後に開発ツールを整える（Phase 4）。
2. **各 Phase の完了時点で常に green を維持する**。Phase = 独立してマージ・revert 可能な単位（原則 1 Phase = 1 PR）。
3. **acceptance test を実行できない制約を前提にリスクを削る**。SDK v2.16 → v2.40 の実挙動変化は unit test では捕捉できないため、(a) 変更を小さいコミットに分割して bisect 可能にする、(b) SDK の CHANGELOG（v2.17.0〜v2.40.1）を通読して挙動変更を洗い出し PR 説明に記録する、(c) 実環境スモークテスト（Default Decision 4）で更新前後の plan 挙動を人間が比較検証する。
4. **コミットは Conventional Commits**（AGENTS.md 準拠）。例: `fix(chrome_policy): follow chromepolicy API type rename`、`chore(deps): update terraform-plugin-sdk/v2 to v2.40.1`、`ci: replace vault-based unit test workflow`。

## 実装計画

### Phase 1: ビルド修復と Go 更新

main を green に戻す最小単位。chromepolicy リネームと go.mod 正常化は不可分（go.sum を直すと directive が上がり、コンパイルエラーが顕在化する）なので 1 PR で行うが、コミットは分ける。

手順:

1. `go mod tidy` を実行し、go directive を `1.27.0` へ明示的に設定（Default Decision 1）。`toolchain` 行は directive と重複するため削除。
2. `.go-version` を `1.27.0` へ更新。
3. chromepolicy 型リネーム: `GoogleChromePolicyV1` → `GoogleChromePolicyVersionsV1` の機械的置換（約 25 箇所）。置換後、リクエスト/レスポンス型のフィールド構造が旧型と同一かを module cache の定義で確認し、差異があれば個別対応。
4. `gofmt -w -s`（directive 引き上げで新しい簡約が入る可能性）と `go vet ./...` の指摘対応。
5. verifier を実行。

- 主な touched files: `go.mod`, `go.sum`, `.go-version`, `internal/provider/resource_chrome_policy.go`, `internal/provider/data_source_chrome_policy_schema.go`, `internal/provider/resource_chrome_policy_test.go`
- verifier: `make build` / `make test` が成功。`make test` が **credential 環境変数なしで**成功することを確認する（Phase 2 で Vault を外す前提の実証。TF_ACC 未設定なら acceptance test はスキップされ、sweeper も `-sweep` フラグなしでは走らない設計のため、通る見込み）。credential を要求する unit test が見つかった場合の扱いは Agent-resolvable 3。

### Phase 2: CI の修復（fork で機能する CI の確立)

Phase 3 の大型更新を CI の保護下で行うため、依存更新より先に実施する。

手順:

1. `unit_tests.yaml` を全面改修:
   - Vault ステップ（hashicorp/vault-action と credential export）を削除。
   - `actions/checkout@v6` / `actions/setup-go@v6` に更新し、Go バージョンは `go-version-file: 'go.mod'` で指定（ハードコード撤廃。`.go-version` 参照でも可、Agent-resolvable 4）。
   - ジョブ内容: `go mod verify` → `go build ./...` → `make test`（現行 test.yml の build ジョブ相当を統合）。
   - トリガーの paths を `'**.go'` から `go.mod` / `go.sum` / `.github/workflows/**` / `GNUmakefile` を含む形へ拡大（Dependabot PR で必ず走るように）。
2. `test.yml`（Acceptance Tests。Vault 前提で fork では永遠に動かない）を削除し、`.github/vault/` / `.github/infra/`（HashiCorp 社内インフラ定義）も削除する（Default Decision 2）。
3. `release.yml` / `.goreleaser.yml` / `.release/` は変更しない（Default Decision 3。リリースフローは配布方法決定後の別 plan で再構築）。
4. `dependabot.yml` の gomod 更新を weekly + grouped updates（minor/patch をまとめる）に変更する（Default Decision 5）。

- 主な touched files: `.github/workflows/unit_tests.yaml`, `.github/workflows/test.yml`, `.github/dependabot.yml`,（決定次第）`.github/vault/`, `.github/infra/`
- verifier: この PR 自体の CI が green になること。go.mod のみ変更するダミー変更でも workflow が発火することをトリガー定義で確認。
- 注意: GitHub Actions workflow の変更は標準 stop condition の対象。本 plan で合意された範囲（上記 1〜4）に限り実施可、範囲外の workflow 変更が必要になったら停止する。

### Phase 3: 依存パッケージの更新

CI 保護下で、リスクの大きい順に独立コミットで更新する。

手順:

1. **terraform-plugin-sdk/v2 v2.16.0 → v2.40.1**（最重要・最大リスク）:
   - 事前に v2.17.0〜v2.40.1 の CHANGELOG を通読し、挙動変更（diff 計算、timeouts、terraform-plugin-log 導入によるログ変化など）を洗い出して PR 説明に記録する。protocol は v5 のまま、state エンコーディングの互換性は SDK v2 内で維持される想定（変化を示す記述を見つけたら停止して報告）。
   - `go get github.com/hashicorp/terraform-plugin-sdk/v2@v2.40.1 && go mod tidy`。terraform-plugin-go / terraform-plugin-log 等の間接依存も追随して大きく上がる。
   - コンパイルエラーの追随修正。
   - deprecated となった `helper/resource` の retry 系を `helper/retry` へ移行（`retry_utils.go` / `retry_transport.go`。機械的な import/識別子変更で挙動同一）。その他の deprecation 警告は「ビルドを妨げるもの・単純置換で済むもの」のみ対応し、残りは記録に留める（Agent-resolvable 5）。
2. **google.golang.org/api → 最新（v0.295.x）**: `go get google.golang.org/api@latest && go mod tidy`。生成コードの型・フィールド変化によるコンパイルエラーを追随修正。使用サービス（admin/directory/v1, groupssettings/v1, chromepolicy/v1, gmail/v1, licensing/v1 等）で削除フィールドに当たった場合は停止して報告（schema 互換性に波及しうるため）。
3. **golang.org/x/oauth2 ほか残りの直接依存**: `go get -u` は使わない（依存グラフを広く更新し、直前に固定・レビューした SDK / google api まで再更新しうるため）。`go list -m -u <module>` で直接依存ごとの更新候補を確認し、`go get <module>@<version>` とモジュール・バージョンを明示して個別に更新 → `go mod tidy` → verifier、を依存ごとに繰り返す。新規の直接依存やメジャーバージョン更新が発生する場合は標準 stop condition に従い停止。
4. **terraform-plugin-docs v0.21.0 → v0.25.0**: 更新後 `make generate` を実行し、`docs/` の差分をレビューして生成結果の変化（フォーマット変更のみか、内容欠落がないか)を確認の上コミットに含める。

- 主な touched files: `go.mod`, `go.sum`, `internal/provider/retry_utils.go`, `internal/provider/retry_transport.go`, ほかコンパイルエラー追随箇所, `docs/`（生成差分）
- verifier: 各ステップ後に `make build` / `make test`。ステップ 4 後に `make generate` して `git diff docs/` が説明可能な差分のみであること。CI green。マージ前に実環境スモークテスト（Default Decision 4、人間が実施）に合格していること。

### Phase 4: 開発ツールのモダン化

手順:

1. **golangci-lint v2 対応**: `.golangci.yml` を v2 形式へ移行（`golangci-lint migrate` を利用可）。現行設定の意図（errcheck で `schema.Set` と `fmt` 系の未チェックを無視）を v2 の `linters.exclusions` 相当で維持する。lint 実行して出た指摘は「明白な修正のみ対応、それ以外は設定で明示的に除外して理由をコメント」（Agent-resolvable 6）。
2. **CI に lint ジョブ追加**: golangci-lint 公式 action（golangci-lint-action、実装時に最新メジャーを確認)で `make lint` 相当を実行するジョブを unit_tests.yaml（または新規 lint.yml）に追加。golangci-lint のバージョンは action 側で pin する。
3. **tools.go → go.mod `tool` directive への移行**（Go 1.24+ の標準機能): `tools/tools.go` を削除し `go get -tool github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs`、`main.go` の `//go:generate go run ...` を `//go:generate go tool tfplugindocs` へ変更。`make generate` で動作確認。
4. `GNUmakefile` の必要最小限の追随（lint ターゲット等。ターゲット名と挙動は変えない）。

- 主な touched files: `.golangci.yml`, `.github/workflows/unit_tests.yaml`（または新規 lint workflow）, `tools/tools.go`（削除）, `go.mod`, `main.go`, `GNUmakefile`
- verifier: `make lint` がローカルと CI の両方で成功。`make generate` で docs 差分が出ない（または説明可能な差分のみ）。`make build` / `make test` green 維持。

## リスクと緩和

| リスク | 緩和策 |
|---|---|
| SDK v2.16 → v2.40 の実挙動変化（diff 計算、retry、ログ等）を unit test で捕捉できない | CHANGELOG 通読と記録、コミット分割による bisect 可能性。実環境スモークテスト（Default Decision 4 に手順・合格条件を定義）を人間が実施し、合格まで Phase 3 をマージしない |
| google api の生成コード変化が schema にマップされたフィールドへ波及 | コンパイルエラーだけでなく、使用フィールドの削除・意味変更を発見したら停止して報告 |
| chromepolicy リネームが型名だけでなく構造変化を含む | 置換後にフィールド定義を新旧比較。`resource_chrome_policy` は unit test（policy schema の変換ロジック）が比較的厚いことも確認済み |
| go directive 引き上げによる gofmt / vet の差分ノイズ | 機械的整形は独立コミットに分離し、レビューしやすくする |
| Dependabot が今後も非互換更新を持ち込む | Phase 2 のトリガー拡大で必ず CI が走る状態にし、grouped updates で頻度を制御 |

## Question classification

### Default Decisions

2026-08-31 の人間レビューで以下が決定された（旧 Human Decisions Required 1〜5 から昇格）。

1. **Go の着地バージョン**: go directive / `.go-version` / CI とも `1.27.0` で統一する。`toolchain` 行は削除する。
2. **`test.yml`（Acceptance Tests workflow）**: 削除する。`.github/vault/` / `.github/infra/` も削除する。acceptance test 運用を始める際は別 plan で新規設計する。
3. **`release.yml` / `.goreleaser.yml` / `.release/`**: 今回は変更せず残置する（タグを push しない限り発火しない）。リリースフローは配布方法決定後の別 plan で再構築する。
4. **SDK 大幅更新後の実環境スモークテスト**: 実施する。実施者は人間で、credential は agent に渡さない。手順: (1) 更新前の main でビルドしたバイナリ + dev_overrides で、実環境の既存 config / state に対し read-only の `terraform plan` を実行し出力を保存 → (2) Phase 3 完了後のバイナリで同一 config / state に対し `terraform plan` → (3) 合格条件 = 更新前に存在しなかった diff・エラーが新たに発生しないこと。合格するまで Phase 3 をマージしない。
5. **Dependabot 運用**: gomod を weekly + grouped updates（minor/patch をまとめる）へ変更する。

### Human Decisions Required

なし（2026-08-31 の人間レビューですべて Default Decisions へ昇格済み）。

### Agent-resolvable Decisions

1. chromepolicy リネームの置換手段（エディタ一括置換か sed か）と、コミットの分け方の細部。
2. go mod tidy 後の間接依存リストの整理のしかた。
3. credential を要求する unit test が見つかった場合に、環境変数未設定時 skip へ改める最小修正（テストの意味を変えない範囲）。
4. CI の Go バージョン指定を `go.mod` 参照にするか `.go-version` 参照にするか。
5. SDK 更新時の deprecated API のうち、単純置換で済まないものの扱い（記録して残すか、その場で移行するか）。
6. golangci-lint v2 設定の細部（exclusions の書き方、有効 linter の選定は現状維持を基本とする）。
7. エラーメッセージ・コメント等の文言。

## Stop condition

`.agents/docs/planning-review.md` の標準 stop condition に委譲する。特に本 plan で顕在化しやすいもの:

- GitHub Actions workflow の変更は Phase 2 / Phase 4 で合意された範囲のみ。範囲外に及ぶ場合は停止。
- 依存更新で新規直接依存の追加やメジャーバージョン更新（v2 → v3 等）が必要になった場合は停止。
- google api 更新で schema にマップされたフィールドの削除・意味変更を検出した場合は停止（state 互換性リスク）。
- タグ発行・リリースは行わない。
- verifier が同一原因で 3 回失敗したら停止。

## Verifier（まとめ)

- 各 Phase 完了時: `make build` && `make test`（credential 環境変数なし）。
- Phase 3: 上記に加え、マージ前に実環境スモークテスト合格（人間が実施。手順・合格条件は Default Decision 4）。
- Phase 3 以降: `make generate` 後の `git diff docs/` が説明可能な差分のみ。
- Phase 4 以降: `make lint`。
- 最終確認: クリーンな checkout（`git clean -xdf` 相当の状態）から `make build` / `make test` / `make lint` / `make generate` が一括で成功し、CI が green であること。

## 進め方（レビューフロー）

`.agents/docs/planning-review.md` に従う（AI critical review はユーザー指示により先行実施した）:

1. AI critical review（実施済み。finding と対応は「AI critical review 履歴」参照）。
2. 本 plan の人間レビュー（2026-08-31 実施。Human Decisions 1〜5 を決定し Default Decisions へ昇格）。
3. 決定反映後の `codex` 最終再レビュー。
4. Critical finding を解消し、`Ready for Implementation` にしてから実装に着手。実装も Phase ごとに PR を分け、各 PR を人間がレビューする。

## AI critical review 履歴

### 2026-08-31 初回（codex exec / gpt-5.6-sol。ユーザー指示により人間レビューに先行して実施）

| # | Finding | 対応 |
|---|---|---|
| 1 | [Blocker] plan の配置先 `docs/plans/` が当時の規約（`.agents/plans/`）に違反 | ユーザー決定により正式な置き場所を `docs/plans/` に変更。AGENTS.md と `.agents/docs/planning-review.md` を追随更新して解消 |
| 2 | [Blocker] Human Decisions Required 1〜5 が未決のまま | agent には解消不能（人間の決定待ち）。status 補足に記録。決定後に Default Decisions へ昇格し再レビューする |
| 3 | [Critical] SDK 更新の互換性検証が不足（スモークテストの手順・合格条件が未定義、不実施時のリスク受容が不明確） | Human Decision 4 に手順・合格条件・リスク受容の選択肢を明記。Non-goals の「外部挙動を一切変えない」を「意図的には変えない」へ修正 |
| 4 | [Critical] `go get -u` が更新範囲を制御できない | Phase 3 手順 3 を `go get <module>@<version>` の明示更新方式へ書き換え |

### 2026-08-31 再レビュー（codex exec resume / gpt-5.6-sol）

新たな critical issue / blocker なし。前回 4 件の解消を確認。残る前提は Human Decisions 1〜5 の確定のみで、確定・反映後に最終再レビューへ進む。

### 2026-08-31 最終レビュー（codex exec resume / gpt-5.6-sol、人間レビュー反映後）

| # | Finding | 対応 |
|---|---|---|
| 5 | [Blocker] Non-goals の「実 Workspace 組織を使う操作」の全面除外が、Default Decision 4 の人間実施スモークテストと矛盾 | Non-goals を「agent による実環境操作は scope 外。人間実施の read-only スモークテスト（Default Decision 4）は例外として scope に含む」へ明確化 |

修正後の再レビューで「critical issue / blocker は残っていない。plan は `Ready for Implementation`」の判定を得た。

### 2026-08-31 実装後レビュー（codex exec / gpt-5.6-sol）

コード上の critical finding はなし。未完了事項として、Default Decision 4 の実 Workspace read-only スモークテストが P1 で指摘された。agent は credential を扱わないため、PR の未完チェックとし、合格するまで merge しない。

## 調査ソース

- ローカル調査: go.mod / go.sum / `.go-version` / GNUmakefile / `.golangci.yml` / `.goreleaser.yml` / `.github/workflows/*` / `.github/dependabot.yml`、`go build` / `make test` / `make lint` の実行結果、module cache の chromepolicy v0.226.0 生成コード。
- Go 最新バージョン: https://go.dev/dl/
- terraform-plugin-sdk releases: https://github.com/hashicorp/terraform-plugin-sdk/releases
- terraform-plugin-docs releases: https://github.com/hashicorp/terraform-plugin-docs/releases
- goreleaser releases: https://github.com/goreleaser/goreleaser/releases
- google.golang.org/api: https://pkg.go.dev/google.golang.org/api
- golangci-lint changelog: https://golangci-lint.run/docs/product/changelog/
