# Orchestrator

Tooling for the agent that assigns backlog sub-issues to Jules and merges the results.

- **Live state, runbook and log:** the pinned **Orchestrator Logs** issue (#67). Read that issue first.
- **Full handover (history, state, decisions, review checklists, lessons):** [HANDOVER.md](HANDOVER.md)
- **Resume skill for a successor agent:** [`.claude/skills/gargi-orchestrator/SKILL.md`](../.claude/skills/gargi-orchestrator/SKILL.md)
- **Onboarding ticket:** #68

    source ~/.config/jules/env
    python3 .orchestrator/jules.py get <session-id>
