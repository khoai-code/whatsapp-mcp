"""Shared settings file (repo-root .env), read by both the bridge and this server.

Same rules as the bridge's config.go: KEY=VALUE lines, optional `export`,
# comments, surrounding quotes stripped, and real environment variables win.
"""
import os
from pathlib import Path
from typing import Optional, Tuple

CONFIG_FILE = Path(__file__).resolve().parent.parent / ".env"


def parse_line(line: str) -> Optional[Tuple[str, str]]:
    line = line.strip()
    if not line or line.startswith("#"):
        return None
    if line.startswith("export "):
        line = line[len("export "):]
    if "=" not in line:
        return None
    key, value = (part.strip() for part in line.split("=", 1))
    if len(value) >= 2 and value[0] in "\"'" and value[-1] == value[0]:
        value = value[1:-1]
    elif " #" in value:
        value = value.split(" #", 1)[0].strip()
    return (key, value) if key else None


def load(path: Optional[Path] = None) -> None:
    try:
        text = (path or CONFIG_FILE).read_text()
    except FileNotFoundError:
        return
    for line in text.splitlines():
        parsed = parse_line(line)
        if parsed and parsed[0] not in os.environ:
            os.environ[parsed[0]] = parsed[1]
