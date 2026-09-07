#!/usr/bin/env python3

from __future__ import annotations

import json
import shlex
import sys
from typing import Any


TARGET_HOST: str = "ryzen"

# SSH options that consume the following argument.
SSH_OPTIONS_WITH_VALUE: frozenset[str] = frozenset(
    {
        "-B",
        "-b",
        "-c",
        "-D",
        "-E",
        "-e",
        "-F",
        "-I",
        "-i",
        "-J",
        "-L",
        "-l",
        "-m",
        "-O",
        "-o",
        "-P",
        "-p",
        "-Q",
        "-R",
        "-S",
        "-W",
        "-w",
    }
)

SHELL_SEPARATORS: frozenset[str] = frozenset(
    {
        ";",
        "&&",
        "||",
        "|",
        "&",
    }
)


def tokenize_command(command: str) -> list[str]:
    """Tokenize a Bash command while preserving command separators."""
    try:
        lexer: shlex.shlex = shlex.shlex(
            command,
            posix=True,
            punctuation_chars=";&|",
        )
        lexer.whitespace_split = True
        lexer.commenters = ""

        return list(lexer)
    except ValueError:
        # Malformed shell quoting: do nothing rather than break Claude Code.
        return []


def is_target_host(destination: str) -> bool:
    """Check whether an SSH destination points to the Ryzen host."""
    if destination == TARGET_HOST:
        return True

    # Also support:
    #   ssh user@ryzen
    return destination.endswith(f"@{TARGET_HOST}")


def command_uses_ssh_to_ryzen(command: str) -> bool:
    """Detect an SSH invocation whose destination is Ryzen."""
    tokens: list[str] = tokenize_command(command)

    for ssh_index, token in enumerate(tokens):
        if token != "ssh":
            continue

        index: int = ssh_index + 1

        while index < len(tokens):
            token = tokens[index]

            # Stop if this SSH invocation ends.
            if token in SHELL_SEPARATORS:
                break

            # Explicit end of SSH options.
            if token == "--":
                index += 1

                if index < len(tokens):
                    return is_target_host(tokens[index])

                break

            if token.startswith("-"):
                # Option and value are separate:
                #   ssh -p 22 ryzen
                #   ssh -i ~/.ssh/id_ed25519 ryzen
                if token in SSH_OPTIONS_WITH_VALUE:
                    index += 2
                    continue

                # Options with attached values:
                #   ssh -p22 ryzen
                #   ssh -oBatchMode=yes ryzen
                index += 1
                continue

            # The first non-option argument is the SSH destination.
            if is_target_host(token):
                return True

            # Destination is another host.
            break

    return False


def emit_reminder() -> None:
    """Inject a non-blocking reminder into Claude's context."""
    output: dict[str, Any] = {
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "additionalContext": (
                "RYZEN ACCESS REMINDER: You are using raw SSH to access the "
                "'ryzen' host. An MCP server named 'ssh-ryzen' is available. "
                "Prefer ssh-ryzen MCP for ordinary remote inspection and operations "
                "when it provides the capability you need, because it gives you "
                "structured remote tools and avoids unnecessary shell interaction. "
                "Raw SSH is NOT forbidden. Continue using SSH whenever the required "
                "operation is unavailable, awkward, interactive, or otherwise better "
                "suited to SSH. Do not stop the task solely because of this reminder."
            ),
        }
    }

    print(json.dumps(output))


def main() -> int:
    """Process Claude Code PreToolUse hook input."""
    try:
        data: dict[str, Any] = json.load(sys.stdin)
    except (json.JSONDecodeError, TypeError):
        return 0

    if data.get("hook_event_name") != "PreToolUse":
        return 0

    if data.get("tool_name") != "Bash":
        return 0

    raw_tool_input: Any = data.get("tool_input", {})
    if not isinstance(raw_tool_input, dict):
        return 0

    command: str = str(raw_tool_input.get("command", ""))

    if command_uses_ssh_to_ryzen(command):
        emit_reminder()

    return 0


if __name__ == "__main__":
    sys.exit(main())