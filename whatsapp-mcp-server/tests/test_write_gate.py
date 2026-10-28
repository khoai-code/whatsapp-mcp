import asyncio
import importlib
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

WRITE_TOOLS = {"send_message", "send_file", "send_audio_message"}
READ_TOOLS = {"list_chats", "list_messages", "download_media", "get_status"}


def load_server(monkeypatch, allow_write):
    if allow_write is None:
        monkeypatch.delenv("WHATSAPP_MCP_ALLOW_WRITE", raising=False)
    else:
        monkeypatch.setenv("WHATSAPP_MCP_ALLOW_WRITE", allow_write)
    import main
    return importlib.reload(main)


def tool_names(server):
    return {t.name for t in asyncio.run(server.mcp.list_tools())}


@pytest.mark.parametrize("value", [None, "", "false", "1", "yes"])
def test_write_tools_hidden_when_flag_off(monkeypatch, value):
    names = tool_names(load_server(monkeypatch, value))
    assert not names & WRITE_TOOLS
    assert READ_TOOLS <= names


@pytest.mark.parametrize("value", ["true", "TRUE", " True "])
def test_write_tools_registered_when_flag_on(monkeypatch, value):
    names = tool_names(load_server(monkeypatch, value))
    assert WRITE_TOOLS <= names
    assert READ_TOOLS <= names


def test_write_tool_descriptions_warn(monkeypatch):
    server = load_server(monkeypatch, "true")
    for tool in asyncio.run(server.mcp.list_tools()):
        if tool.name in WRITE_TOOLS:
            assert "real WhatsApp account" in tool.description


def test_get_status_without_bridge(monkeypatch, tmp_path):
    server = load_server(monkeypatch, None)
    import whatsapp
    monkeypatch.setattr(whatsapp, "BRIDGE_TOKEN_PATH", str(tmp_path / "missing.token"))
    status = server.get_status()
    assert status["bridge_running"] is False
    assert status["write_tools_registered"] is False


def test_get_status_passes_bridge_state(monkeypatch, tmp_path):
    server = load_server(monkeypatch, "true")
    import whatsapp
    token = tmp_path / "bridge.token"
    token.write_text("abc\n")
    monkeypatch.setattr(whatsapp, "BRIDGE_TOKEN_PATH", str(token))
    seen = {}

    class FakeResponse:
        status_code = 200
        def json(self):
            return {"connected": True, "write_enabled": False, "allowlist_active": True}

    def fake_get(url, headers, timeout):
        seen["url"], seen["headers"] = url, headers
        return FakeResponse()

    monkeypatch.setattr(whatsapp.requests, "get", fake_get)
    status = server.get_status()
    assert seen["url"].endswith("/api/status")
    assert seen["headers"] == {"Authorization": "Bearer abc"}
    assert status["bridge_running"] and status["connected"]
    assert status["write_tools_registered"] is True
    assert status["write_enabled"] is False
