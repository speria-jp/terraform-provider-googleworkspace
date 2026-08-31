---
paths:
  - ".agents/plans/**"
---

# Implementation Planning

`.agents/plans/**` の planning 正本は `.agents/docs/planning-review.md` とする。
この rule は、Codex / Claude が plan file を編集したときに自動で思い出すための短い入口である。

- plan の status は `.agents/docs/planning-review.md` の定型語彙（`Draft` / `Human Review Pending` / `AI Critical Review Pending` / `Ready for Implementation` / `Blocked` / `Implemented`）に寄せる。
- 未決事項は `Open Questions` のまま残さず、`Default Decisions` / `Human Decisions Required` / `Agent-resolvable Decisions` に分類する。
- schema の breaking change、実 Workspace 環境への影響、credential、リリースに関わる判断は `Human Decisions Required` に置き、独断で確定しない。

Before presenting a plan to the user, review it with the `codex` command when `.agents/docs/planning-review.md` requires AI critical review. Adjust the review prompt as needed, but always include the instruction to only flag critical issues and blockers to implementation readiness.

```bash
# Initial plan review
codex exec -m gpt-5.6-sol "Review this plan. Don't nitpick trivial things. Only flag critical issues and blockers to implementation readiness: {plan_full_path} (ref: {AGENTS.md full_path})"

# Updated plan review (resume --last preserves prior review context)
codex exec resume --last -m gpt-5.6-sol "Plan updated. Review again. Don't nitpick trivial things. Only flag critical issues and blockers to implementation readiness: {plan_full_path} (ref: {AGENTS.md full_path})"
```

When running `codex` from inside an active Codex session, use `--ephemeral` from the start and isolate Codex state with a temporary `CODEX_HOME` (copy the parent auth / config into it first). If `codex exec resume --last` fails because of local Codex state, continue with a fresh `codex exec --ephemeral` and include the previous critical findings in the prompt. See `.agents/docs/planning-review.md` for the full commands.
