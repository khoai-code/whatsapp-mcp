import importlib
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

GROUP = "120363000000000000@g.us"


@pytest.fixture
def server(monkeypatch):
    monkeypatch.setenv("WHATSAPP_MCP_ALLOW_WRITE", "true")
    import main
    server = importlib.reload(main)
    calls = []
    responses = {}

    def fake_bridge_call(method, path, payload=None):
        calls.append((method, path, payload))
        return responses.get(path, (True, {"success": True, "message": "done"}))

    monkeypatch.setattr(server, "bridge_call", fake_bridge_call)
    server.calls, server.responses = calls, responses
    return server


def test_list_groups_compact_and_sorted(server):
    server.responses["/groups"] = (True, [
        {"jid": "2@g.us", "name": "zeta", "participants": 3, "is_admin": False},
        {"jid": GROUP, "name": "Alpha", "participants": 12, "is_admin": True},
    ])
    assert server.list_groups().splitlines() == [
        f"Alpha · {GROUP} · 12 members · admin",
        "zeta · 2@g.us · 3 members · member",
    ]


def test_group_info_caps_members(server):
    members = [{"jid": f"84{i:09}@s.whatsapp.net", "is_admin": i == 0, "is_super_admin": False} for i in range(150)]
    server.responses["/group/info"] = (True, {
        "jid": GROUP, "name": "Team", "description": "line1\nline2", "locked": True,
        "announce": False, "is_admin": True, "members": members,
    })
    lines = server.get_group_info(GROUP, member_limit=10).splitlines()
    assert lines[0] == f"Team · {GROUP} · 150 members · you: admin · locked"
    assert lines[1] == "description: line1 ⏎ line2"
    assert lines[2] == "84000000000 · admin"
    assert lines[3] == "84000000001"
    assert lines[-1] == "(+140 more members not shown)"
    assert len(lines) == 13


def test_participant_tools_send_right_action(server):
    for tool, action in [
        (server.add_participants, "add"),
        (server.remove_participants, "remove"),
        (server.promote_admin, "promote"),
        (server.demote_admin, "demote"),
    ]:
        tool(GROUP, ["84111111111"])
        assert server.calls[-1] == ("POST", "/group/participants",
                                    {"group_jid": GROUP, "action": action, "participants": ["84111111111"]})


def test_write_result_lines(server):
    server.responses["/group/participants"] = (True, {
        "success": True, "message": "add: 1 ok, 1 failed", "failed": ["84999999999 (error 403)"]
    })
    assert server.add_participants(GROUP, ["a", "b"]) == "ok: add: 1 ok, 1 failed · failed: 84999999999 (error 403)"
    server.responses["/group/create"] = (True, {"success": True, "message": 'Created group "New"', "jid": GROUP})
    assert server.create_group("New", ["84111111111"]) == f'ok: Created group "New" · jid {GROUP}'
    server.responses["/group/leave"] = (False, "You are not an admin of \"Team\", so this action is not allowed")
    assert server.leave_group(GROUP).startswith("error: You are not an admin")


def test_invite_link_and_errors(server):
    server.responses["/group/invite-link"] = (True, {"link": "https://chat.whatsapp.com/abc"})
    assert server.get_group_invite_link(GROUP) == "https://chat.whatsapp.com/abc"
    server.responses["/groups"] = (False, "WhatsApp bridge is not reachable: refused")
    assert server.list_groups() == "error: WhatsApp bridge is not reachable: refused"


def test_mark_read_payload(server):
    server.mark_read(GROUP, ["m1", "m2"], sender="84111111111")
    assert server.calls[-1] == ("POST", "/mark-read",
                                {"chat_jid": GROUP, "message_ids": ["m1", "m2"], "sender": "84111111111"})


def test_bridge_call_surfaces_policy_message(monkeypatch, tmp_path):
    import whatsapp
    token = tmp_path / "bridge.token"
    token.write_text("t")
    monkeypatch.setattr(whatsapp, "BRIDGE_TOKEN_PATH", str(token))

    class Resp:
        status_code = 403
        text = ""
        def json(self):
            return {"success": False, "message": "recipient x is not on WHATSAPP_MCP_SEND_ALLOWLIST"}

    monkeypatch.setattr(whatsapp.requests, "request", lambda *a, **k: Resp())
    assert whatsapp.bridge_call("POST", "/group/name", {}) == (
        False, "recipient x is not on WHATSAPP_MCP_SEND_ALLOWLIST")
