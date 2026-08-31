# Plan レビュー規約

このドキュメントは、`docs/plans/` 配下の実装計画を作成または大きく更新するときのレビュー手順を定義する。

## 基本フロー

非自明な変更（新規 resource / data source の追加、schema 変更、認証・API スコープまわり、upstream からの取り込み、依存の大規模更新、リリース）は、原則として次の順で進める。

1. Agent が plan draft を `docs/plans/YYYYMMDD-<topic>.md` に作成する。
2. 人間が scope、実 Workspace 環境への影響、互換性方針をレビューする。
3. Agent が人間レビューを反映する。
4. AI critical review を実行する。
5. Critical finding があれば plan を修正し、必要に応じて再レビューする。

人間レビューは AI review より先に行う。
AI review は表現の好みではなく、実装前に潰すべき重大な抜け漏れ、矛盾、リスクの検出に使う。

## Plan status lifecycle

`docs/plans/` の status は、実装可否が読み取れる定型語彙に寄せる。
補足が必要な場合は、status 行の後に説明を書く。

| status | 意味 |
|---|---|
| `Draft` | 作成中。実装不可。 |
| `Human Review Pending` | 人間の scope、互換性、実環境影響レビュー待ち。実装不可。 |
| `AI Critical Review Pending` | 人間レビュー後、AI critical review 待ち。実装不可。 |
| `Ready for Implementation` | scope、decisions、verifier、stop condition が閉じており、実装可能。 |
| `Blocked` | 人間判断、外部依存、設計矛盾、credential で停止中。 |
| `Implemented` | 実装済み。 |

複数状態を自由文で混ぜない。
たとえば `Draft。Human review pending。` ではなく、主状態を `Human Review Pending` にする。

## Implementation readiness review

AI critical review 後に実装へ進める前に、plan が実装 agent に渡せる粒度まで閉じているかを確認する。

`Ready for Implementation` の条件は次の通り。

- goal、non-goals、scope が明記されている。
- 実装順序、主な touched files、verifier（`make test` / `make lint` / `make generate` の差分確認など）が明記されている。
- stop condition が明記されているか、関連規約に委譲されている。
- unresolved question が実装を止めるものかどうか分類されている。
- state 互換性、実 Workspace 環境への影響、認証情報の扱い、リリースに関わる未決判断が残っていない。

## Question classification

実装前の plan では、未決事項を `Open Questions` のまま残さず、次のいずれかに分類する。

### Default Decisions

実装時に採用する初期判断を書く。
ここに入れてよいのは、人間レビュー済みの判断だけである。

agent は、未分類の `Open Questions` や互換性・実環境影響・credential に近い判断を、独断で `Default Decisions` に昇格しない。

### Human Decisions Required

人間判断が必要な項目を書く。
ここに項目が残っている plan は、実装に渡さない。

例は次の通り。

- schema の breaking change（`ForceNew` 追加、型変更、Required 化、attribute 削除）を許容するか。
- acceptance test を実 Workspace 組織で実行するか。どの組織・credential を使うか。
- 新しい Google API スコープや API 有効化を要求するか。
- upstream（アーカイブ済み）の未取り込み変更や community fork の変更をどこまで取り込むか。
- リリース（タグ発行・バイナリ配布）を行うか。バージョンをどう採番するか。

### Agent-resolvable Decisions

agent が既存規約と plan の範囲内で自己解決してよい判断を書く。

例は次の通り。

- test helper を既存 file に足すか、新規 file に分けるか。
- 既存の resource / data source 実装パターンへの合わせ方。
- エラーメッセージや `Description` の文言。
- unit test のケース分割。

## Stop condition（標準）

plan で個別に上書きしない限り、実装 agent は以下に触れる場合に停止して人間に確認する。

- `make testacc` / `make sweep` / `TF_ACC=1` を伴う実行。
- 実際の Google Workspace 組織・credential・service account を使う操作。
- schema の breaking change（state 互換性を壊す変更）。
- 新規依存の追加、依存のメジャーバージョン更新。
- タグ発行、リリース、GitHub Actions workflow の変更。
- plan と実装の矛盾。
- verifier が同じ理由で 3 回失敗。

## AI critical review の実行

人間レビュー反映後、plan を提示または実装着手する前に、`codex` コマンドで critical review を実行する。
prompt は必要に応じて調整してよいが、critical issue だけを指摘することと、implementation readiness を確認することを含める。

```bash
# 初回 AI review
codex exec -m gpt-5.6-sol "Review this plan. Don't nitpick trivial things. Only flag critical issues and blockers to implementation readiness: {plan_full_path} (ref: {AGENTS.md full_path})"

# 更新後の再 review
codex exec resume --last -m gpt-5.6-sol "Plan updated. Review again. Don't nitpick trivial things. Only flag critical issues and blockers to implementation readiness: {plan_full_path} (ref: {AGENTS.md full_path})"
```

## Codex session 内で実行する場合

active な Codex session の中からさらに `codex exec` を実行する場合は、最初から `--ephemeral` を使い、一時 `CODEX_HOME` で状態を分離する。
親の auth と config は一時 home にコピーする。
これにより、親 `~/.codex` 配下の sqlite state、session persistence、plugin sync state による nested-session failure を避ける。

```bash
tmp_codex_home="$(mktemp -d)"
cp "${CODEX_HOME:-$HOME/.codex}/auth.json" "$tmp_codex_home/auth.json"
cp "${CODEX_HOME:-$HOME/.codex}/config.toml" "$tmp_codex_home/config.toml"
CODEX_HOME="$tmp_codex_home" codex exec --ephemeral -m gpt-5.6-sol \
  "Review this plan. Don't nitpick trivial things. Only flag critical issues and blockers to implementation readiness: {plan_full_path} (ref: {AGENTS.md full_path})"
```

## 再 review の fallback

`codex exec resume --last` が local Codex state、thread persistence、sandbox、readonly database、plugin sync、network issue などで失敗した場合は、fresh な `codex exec --ephemeral` でレビューを継続する。
この場合、同じ Codex session を保つことより、前回の critical finding と修正内容を prompt に明示して review context を保つことを優先する。

```bash
tmp_codex_home="$(mktemp -d)"
cp "${CODEX_HOME:-$HOME/.codex}/auth.json" "$tmp_codex_home/auth.json"
cp "${CODEX_HOME:-$HOME/.codex}/config.toml" "$tmp_codex_home/config.toml"
CODEX_HOME="$tmp_codex_home" codex exec --ephemeral -m gpt-5.6-sol "
Review this updated plan. Don't nitpick trivial things. Only flag critical issues and blockers to implementation readiness.

Plan:
{plan_full_path}

Reference:
{AGENTS.md full_path}

Previous critical review findings, already addressed:
1. {finding_1} -> {change_1}
2. {finding_2} -> {change_2}

Please verify whether any critical issues remain. If none remain, say clearly that no critical issues remain.
"
```

## 実装着手条件

- 人間レビュー待ちの plan は、明示的に依頼されない限り実装に進めない。
- AI critical review で critical finding が出た場合は、finding を反映するか、採用しない理由を plan に残してから実装する。
- `Human Decisions Required` が残っている plan は、明示的な readiness override がない限り実装に進めない。
- Critical finding と readiness blocker がない場合は、その結果を作業報告に含めて実装に進む。
