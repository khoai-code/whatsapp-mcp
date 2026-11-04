import importlib
import os
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import config


@pytest.mark.parametrize("line,expected", [
    ("WHATSAPP_MCP_ALLOW_WRITE=true", ("WHATSAPP_MCP_ALLOW_WRITE", "true")),
    ("export A=1", ("A", "1")),
    ('B="84 111, 84 222"', ("B", "84 111, 84 222")),
    ("C='x'", ("C", "x")),
    ("D=false # off for now", ("D", "false")),
    ("  # comment", None),
    ("", None),
    ("no equals", None),
])
def test_parse_line_matches_bridge(line, expected):
    assert config.parse_line(line) == expected


def test_env_wins_over_file(monkeypatch, tmp_path):
    f = tmp_path / ".env"
    f.write_text("WHATSAPP_MCP_ALLOW_WRITE=true\nWHATSAPP_MCP_SEND_ALLOWLIST=84111\n")
    monkeypatch.setenv("WHATSAPP_MCP_ALLOW_WRITE", "false")
    monkeypatch.delenv("WHATSAPP_MCP_SEND_ALLOWLIST", raising=False)
    config.load(f)
    assert os.environ["WHATSAPP_MCP_ALLOW_WRITE"] == "false"
    assert os.environ["WHATSAPP_MCP_SEND_ALLOWLIST"] == "84111"
    monkeypatch.delenv("WHATSAPP_MCP_SEND_ALLOWLIST")


def test_file_turns_on_write_tools(monkeypatch, tmp_path):
    f = tmp_path / ".env"
    f.write_text("WHATSAPP_MCP_ALLOW_WRITE=true\n")
    monkeypatch.delenv("WHATSAPP_MCP_ALLOW_WRITE", raising=False)
    monkeypatch.setattr(config, "CONFIG_FILE", f)
    import main
    server = importlib.reload(main)
    assert server.WRITE_ENABLED
    monkeypatch.delenv("WHATSAPP_MCP_ALLOW_WRITE")
