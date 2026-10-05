# Orchestrator

Tooling for the agent that assigns backlog sub-issues to Jules and merges the results.
The runbook, build order and live status are in the pinned **Orchestrator Logs** issue (#67).
Read that issue first. To hand orchestration to another agent, see #68.

    source ~/.config/jules/env
    python3 .orchestrator/jules.py start <issue-number>
