#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import re
import sys
from pathlib import Path
from typing import Any


# MCP server names as they appear in Claude Code tool names:
# mcp__<server_name>__<tool_name>
PREFERRED_MCP_SERVERS: tuple[str, ...] = (
    "serena",
    "codebase-memory",
    "codebase_memory",
)


# Detect rg/grep when they are actually being executed as commands.
# Examples caught:
#   rg foo
#   grep -R foo .
#   cd src && rg foo
#   cat file | grep foo
#   sudo rg foo
#   command grep foo
#   git grep foo
BASH_TEXT_SEARCH_PATTERN: re.Pattern[str] = re.compile(
    r"""
    (?:
        ^ |
        && |
        \|\| |
        ; |
        \| |
        \n
    )
    \s*
    (?:
        sudo\s+ |
        command\s+ |
        env(?:\s+[A-Za-z_][A-Za-z0-9_]*=[^\s]+)*\s+
    )*
    (?:
        rg\b |
        grep\b |
        git\s+grep\b
    )
    """,
    re.VERBOSE,
)


def get_state_dir() -> Path:
    """Return directory used to store per-session policy state."""
    runtime_dir: str = os.environ.get("XDG_RUNTIME_DIR", "/tmp")

    state_dir: Path = Path(runtime_dir) / "claude-code-search-policy"
    state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)

    return state_dir


def sanitize_session_id(session_id: str) -> str:
    """Make session ID safe for use as a file name."""
    return re.sub(r"[^A-Za-z0-9_.-]", "_", session_id)


def get_marker_path(session_id: str) -> Path:
    """Return marker indicating that preferred MCP search was used."""
    safe_session_id: str = sanitize_session_id(session_id)
    return get_state_dir() / f"{safe_session_id}.mcp-used"


def is_preferred_mcp_tool(tool_name: str) -> bool:
    """Return True when tool belongs to Serena or codebase-memory."""
    return any(
        tool_name.startswith(f"mcp__{server_name}__")
        for server_name in PREFERRED_MCP_SERVERS
    )


def is_text_search(tool_name: str, tool_input: dict[str, Any]) -> bool:
    """Detect built-in Grep or rg/grep executed through Bash."""
    if tool_name == "Grep":
        return True

    if tool_name != "Bash":
        return False

    command: str = str(tool_input.get("command", ""))

    return BASH_TEXT_SEARCH_PATTERN.search(command) is not None


def mark_preferred_mcp_used(session_id: str, tool_name: str) -> None:
    """Record successful use of a preferred MCP tool for this session."""
    marker_path: Path = get_marker_path(session_id)
    marker_path.write_text(tool_name + "\n", encoding="utf-8")


def preferred_mcp_was_used(session_id: str) -> bool:
    """Check whether preferred codebase tooling was used this session."""
    return get_marker_path(session_id).exists()


def deny_text_search() -> None:
    """Deny grep/rg and explain the required search workflow to Claude."""
    output: dict[str, Any] = {
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": "deny",
            "permissionDecisionReason": (
                "Codebase search policy: do not start codebase exploration "
                "with Grep, rg, grep, or git grep. "
                "Use Serena MCP for symbol/reference/structural navigation "
                "or codebase-memory MCP for semantic and architectural search first. "
                "After at least one successful Serena or codebase-memory MCP call "
                "in this session, literal text search is allowed for precise "
                "verification."
            ),
            "additionalContext": (
                "IMPORTANT SEARCH POLICY: The attempted text search was blocked. "
                "For understanding unfamiliar code, dependencies, symbols, references, "
                "implementations, architecture, or behavior, use Serena or "
                "codebase-memory first. Grep/rg is a secondary verification tool, "
                "not the primary codebase discovery mechanism."
            ),
        }
    }

    print(json.dumps(output))


def main() -> int:
    """Handle Claude Code hook input."""
    try:
        data: dict[str, Any] = json.load(sys.stdin)
    except (json.JSONDecodeError, TypeError):
        # Do not break Claude Code because of malformed hook input.
        return 0

    event_name: str = str(data.get("hook_event_name", ""))
    session_id: str = str(data.get("session_id", "unknown"))
    tool_name: str = str(data.get("tool_name", ""))

    raw_tool_input: Any = data.get("tool_input", {})
    tool_input: dict[str, Any] = (
        raw_tool_input if isinstance(raw_tool_input, dict) else {}
    )

    # A successful preferred MCP call unlocks rg/grep for the session.
    if event_name == "PostToolUse":
        if is_preferred_mcp_tool(tool_name):
            mark_preferred_mcp_used(session_id, tool_name)

        return 0

    if event_name != "PreToolUse":
        return 0

    if not is_text_search(tool_name, tool_input):
        return 0

    if preferred_mcp_was_used(session_id):
        return 0

    deny_text_search()
    return 0


if __name__ == "__main__":
    sys.exit(main())