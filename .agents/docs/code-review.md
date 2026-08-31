# Code Review 規約

このドキュメントは、fresh context reviewer が見るべき観点と output 形式を定義する。
Review は style nit ではなく、merge 前に潰すべき critical issue の検出を目的にする。

## Review の対象

Critical finding として扱う観点は次の通り。

- correctness bug。
- behavioral regression。
- schema の意図しない breaking change（既存 state / 既存利用側との互換性破壊）。
- `ForceNew` の誤設定による、apply 時の意図しない実リソース削除・再作成。
- CRUD の対応漏れ（schema に field を足したのに Create / Update / Read で反映されない等）。
- import（`Importer`）が壊れる変更。
- eventual consistency の考慮漏れ（作成直後の読み取りで `eventual_consistency.go` のパターンを使っていない）。
- credential、token、sensitive 値を log、diff、error message に出す問題。
- Google API のエラーを握りつぶす、または誤ったリトライをする問題。
- missing critical tests（新規 resource / data source に対する test の欠如）。
- `templates/` や schema `Description` の変更に対する `make generate` 漏れ、`docs/` の手編集。
- plan との矛盾。
- scope creep。

Critical finding に含めない観点は次の通り。

- style nit。
- 命名の好み。
- 将来の過剰抽象化提案。
- plan 外の speculative feature。
- test の網羅性を上げるだけで挙動リスクが低い提案。

## Review bundle

reviewer には以下の bundle を渡す。

```md
# Review Bundle

## Goal

## Scope / Non-goals

## Relevant Plans

## Stop Condition Checklist

## Schema / State Compatibility Checklist

## Diff Summary

## Touched Files

## Verification Commands and Results

## Known Risks / Open Questions
```

bundle には credential、token、実組織のユーザー情報を含めない。
必要な場合は redacted summary に置き換える。

## Reviewer output schema

reviewer output は以下に寄せる。

```md
## Critical Findings

- [P1] title
  - File / area:
  - Requirement violated:
  - Why critical:
  - Suggested fix:

## Non-critical Notes

## Verdict

critical_issues_remain | no_critical_issues_remain
```

priority の目安は次の通り。

- `P0`: 実 Workspace 環境のリソース破壊・データ損失につながる問題、credential 漏洩、release を止めるべき破壊的問題。
- `P1`: merge 前に直すべき correctness、state 互換性、missing critical test。
- `P2`: 近い将来直すべきだが、今回の merge を必ず止めるほどではない問題。

fresh context review では、原則として `P0` と `P1` だけを critical finding とする。

## 実装 agent の扱い

- Critical finding がある場合は、修正後に verifier と fresh context review を再実行する。
- Critical finding の修正 loop は最大 2 周までとする。
- Finding を採用しない判断は原則として人間だけが行う。
- 明らかな誤読として採用しない場合でも、理由を作業報告に残す。

## Self-review checklist

fresh context review に出す前に、実装 agent は以下を確認する。

- goal / scope / non-goals にない変更をしていないか。
- stop condition（`.agents/docs/planning-review.md`）に該当する変更を進めていないか。
- schema 変更が既存 state と互換か。`ForceNew` / 型 / Required の変更を plan の合意なしにしていないか。
- `make test` / `make lint` を実行し、結果を報告に含めたか。
- `templates/` や `Description` を変更した場合、`make generate` を実行して `docs/` の差分を含めたか。
- `make testacc` / `make sweep` を明示的な依頼なしに実行していないか。
- credential や sensitive 値が log / error message / 報告に出ていないか。
