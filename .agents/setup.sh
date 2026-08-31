#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# AGENTS.md を CLAUDE.md としてシンボリックリンク
ln -sf AGENTS.md "$REPO_ROOT/CLAUDE.md"

# .agents/skills を .claude/skills としてシンボリックリンク
mkdir -p "$REPO_ROOT/.claude"
ln -sfn "../.agents/skills" "$REPO_ROOT/.claude/skills"

# .agents/rules を .claude/rules としてシンボリックリンク
ln -sfn "../.agents/rules" "$REPO_ROOT/.claude/rules"

echo "Setup complete: CLAUDE.md -> AGENTS.md, .claude/skills -> .agents/skills, .claude/rules -> .agents/rules"
