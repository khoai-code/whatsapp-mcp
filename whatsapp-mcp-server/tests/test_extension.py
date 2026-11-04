import importlib
import os
import socket
import stat
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import whatsapp


@pytest.fixture
def server(monkeypatch, tmp_path):
    monkeypatch.delenv("WHATSAPP_MCP_ALLOW_WRITE", raising=False)
    import main
    server = importlib.reload(main)
    responses = {}
    monkeypatch.setattr(server, "bridge_call", lambda m, p, payload=None: responses[p])
    monkeypatch.setattr(whatsapp, "DATA_DIR", str(tmp_path))
    opened = []
    monkeypatch.setattr(server.subprocess, "run", lambda args, check: opened.append(args))
    server.responses, server.opened = responses, opened
    return server


def test_connect_is_always_available(server):
    import asyncio
    names = {t.name for t in asyncio.run(server.mcp.list_tools())}
    assert "connect_whatsapp" in names


def test_connect_with_phone_number_gives_code(server):
    server.responses["/pair/phone"] = (True, {"state": "waiting", "code": "ABCD1234", "expires_in": 140})
    out = server.connect_whatsapp("84901234567")
    assert "ABCD-1234" in out
    assert "Link with phone number instead" in out
    assert "140 seconds" in out


def test_connect_without_number_shows_qr(server, tmp_path):
    server.responses["/pair"] = (True, {"state": "waiting", "qr": "2@abc,def,ghi", "expires_in": 150})
    out = server.connect_whatsapp()
    png = tmp_path / "link-whatsapp.png"
    assert png.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n"
    assert server.opened == [["open", str(png)]]
    assert "Scan this QR code" in out[1]


def test_connect_when_already_paired(server):
    server.responses["/pair"] = (True, {"state": "paired", "number": "84829432888"})
    assert server.connect_whatsapp() == "Already linked to WhatsApp as +84829432888. You can read your chats now."


def test_connect_reports_errors(server):
    server.responses["/pair/phone"] = (False, "phone must be your number with country code")
    assert server.connect_whatsapp("bob").startswith("error: phone must be")
    server.responses["/pair"] = (True, {"state": "error", "message": "outdated"})
    assert server.connect_whatsapp() == "error: outdated"


# --- launcher -------------------------------------------------------------

def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def stub_bridge(tmp_path, body):
    path = tmp_path / "fake-bridge"
    path.write_text(f"#!{sys.executable}\n{body}\n")
    path.chmod(path.stat().st_mode | stat.S_IEXEC)
    return str(path)


@pytest.fixture
def launcher(monkeypatch, tmp_path):
    port = free_port()
    monkeypatch.setattr(whatsapp, "DATA_DIR", str(tmp_path / "data"))
    monkeypatch.setattr(whatsapp, "BRIDGE_LOG_PATH", str(tmp_path / "data" / "bridge.log"))
    monkeypatch.setattr(whatsapp, "WHATSAPP_API_BASE_URL", f"http://127.0.0.1:{port}/api")
    monkeypatch.setattr(whatsapp, "_bridge_proc", None)
    yield port
    whatsapp.stop_bridge()


def test_repo_install_never_spawns(launcher, monkeypatch):
    monkeypatch.setattr(whatsapp, "BRIDGE_BIN", None)
    assert whatsapp.ensure_bridge() is None
    assert whatsapp._bridge_proc is None


def test_spawns_bridge_and_waits_until_reachable(launcher, monkeypatch, tmp_path):
    port = launcher
    monkeypatch.setattr(whatsapp, "BRIDGE_BIN", stub_bridge(tmp_path, (
        "import http.server, os\n"
        "assert os.environ['WHATSAPP_MCP_PAIR_API'] == 'true'\n"
        f"http.server.HTTPServer(('127.0.0.1', {port}), http.server.BaseHTTPRequestHandler).serve_forever()"
    )))
    assert whatsapp.ensure_bridge() is None
    assert whatsapp._bridge_proc.poll() is None
    first = whatsapp._bridge_proc
    # Already running: reused, not started twice.
    assert whatsapp.ensure_bridge() is None
    assert whatsapp._bridge_proc is first
    whatsapp.stop_bridge()
    assert first.poll() is not None
    assert oct(os.stat(whatsapp.DATA_DIR).st_mode & 0o777) == "0o700"
    assert oct(os.stat(whatsapp.BRIDGE_LOG_PATH).st_mode & 0o777) == "0o600"


def test_bridge_that_dies_is_reported(launcher, monkeypatch, tmp_path):
    monkeypatch.setattr(whatsapp, "BRIDGE_BIN", stub_bridge(tmp_path, "import sys; sys.exit(3)"))
    error = whatsapp.ensure_bridge(wait_seconds=3)
    assert error.startswith("The WhatsApp bridge stopped unexpectedly")
    assert whatsapp.BRIDGE_LOG_PATH in error
