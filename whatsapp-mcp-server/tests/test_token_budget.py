import importlib
import sqlite3
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import formatting as fmt

TZ = timezone(timedelta(hours=7))
BASE = datetime(2026, 10, 29, 9, 0, tzinfo=TZ)
GROUP = "120363000000000000@g.us"
ALICE = "84111111111@s.whatsapp.net"


def ts(minutes):
    # Same text format the Go bridge writes (time.Time via go-sqlite3).
    return str(BASE + timedelta(minutes=minutes))


@pytest.fixture
def server(monkeypatch, tmp_path):
    db = tmp_path / "messages.db"
    conn = sqlite3.connect(db)
    conn.executescript("""
        CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
        CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP,
            is_from_me BOOLEAN, media_type TEXT, filename TEXT, url TEXT, media_key BLOB,
            file_sha256 BLOB, file_enc_sha256 BLOB, file_length INTEGER, PRIMARY KEY (id, chat_jid));
    """)
    conn.execute("INSERT INTO chats VALUES (?, ?, ?)", (ALICE, "Alice", ts(5)))
    conn.execute("INSERT INTO chats VALUES (?, ?, ?)", (GROUP, "Team", ts(149)))
    for i in range(150):
        conn.execute(
            "INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me) VALUES (?, ?, ?, ?, ?, ?)",
            (f"g{i:03}", GROUP, "84111111111", f"group msg {i}", ts(i), False),
        )
    conn.execute(
        "INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me) VALUES (?, ?, ?, ?, ?, ?)",
        ("long", ALICE, "84111111111", "x" * 5000 + "\nsecond line", ts(3), False),
    )
    conn.execute(
        "INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, media_type) VALUES (?, ?, ?, ?, ?, ?, ?)",
        ("pic", ALICE, "84111111111", "look", ts(5), True, "image"),
    )
    conn.commit()
    conn.close()

    import whatsapp
    monkeypatch.setattr(whatsapp, "MESSAGES_DB_PATH", str(db))
    import main
    return importlib.reload(main)


def message_lines(text):
    return [l for l in text.splitlines() if " · " in l and not l.startswith("##")]


def test_clamp_limit():
    assert fmt.clamp_limit(None) == 20
    assert fmt.clamp_limit(0) == 20
    assert fmt.clamp_limit(-5) == 20
    assert fmt.clamp_limit(50) == 50
    assert fmt.clamp_limit(10_000) == 100


def test_truncate():
    assert fmt.truncate("short") == "short"
    out = fmt.truncate("y" * 1500)
    assert out == "y" * 1000 + "…[truncated, 1500 chars]"


def test_default_page_is_20_and_cap_is_100(server):
    assert len(message_lines(server.list_messages(chat_jid=GROUP))) == 20
    assert len(message_lines(server.list_messages(chat_jid=GROUP, limit=500))) == 100


def test_one_line_per_message_oldest_first(server):
    out = server.list_messages(chat_jid=GROUP, limit=3)
    lines = out.splitlines()
    assert lines[0] == f"## Team ({GROUP})"
    assert lines[1] == "2026-10-29 11:27 · Alice · group msg 147"
    assert lines[3] == "2026-10-29 11:29 · Alice · group msg 149"
    assert lines[4].startswith('(more available: before="')


def test_cursor_paging_has_no_gaps_or_overlap(server):
    seen = []
    before = None
    for _ in range(10):
        out = server.list_messages(chat_jid=GROUP, limit=40, before=before)
        seen += [l.rsplit(" · ", 1)[1] for l in message_lines(out)]
        hint = [l for l in out.splitlines() if l.startswith("(more available")]
        if not hint:
            break
        before = hint[0].split('before="')[1].rstrip('")')
    assert len(seen) == 150
    assert len(set(seen)) == 150


def test_long_body_truncated_and_kept_on_one_line(server):
    out = server.list_messages(chat_jid=ALICE)
    long_line = [l for l in out.splitlines() if "truncated" in l][0]
    assert "…[truncated, 5012 chars]" in long_line
    assert "second line" not in out
    assert len(long_line) < 1100


def test_media_is_a_reference(server):
    out = server.list_messages(chat_jid=ALICE)
    assert "2026-10-29 09:05 · Me · [image id=pic] look" in out


def test_context_marks_matches(server):
    out = server.list_messages(chat_jid=GROUP, query="msg 100", include_context=True)
    assert "> 2026-10-29 10:40 · Alice · group msg 100" in out
    assert "· group msg 99" in out and "· group msg 101" in out


def test_list_chats_compact_and_without_last_message(server):
    out = server.list_chats()
    assert out.splitlines()[0].startswith(f"Team · {GROUP} · group · last 2026-10-29 11:29 · Alice: group msg 149")
    preview = [l for l in out.splitlines() if l.startswith("Alice")][0]
    assert "look" in preview
    # Upstream raised a SQL error here.
    bare = server.list_chats(include_last_message=False)
    assert "Alice" in bare and "msg" not in bare


def test_recent_activity(server):
    out = server.recent_activity("2026-10-29T09:00:00+07:00")
    lines = out.splitlines()
    assert lines[0] == f"Team · {GROUP} · 149 msgs · last 2026-10-29 11:29"
    assert lines[1] == f"Alice · {ALICE} · 2 msgs · last 2026-10-29 09:05"
    assert "Invalid" in server.recent_activity("yesterday")


def test_message_context_marks_target(server):
    out = server.get_message_context("g050", before=2, after=2)
    lines = out.splitlines()
    assert len(lines) == 6
    assert lines[3].startswith("> ") and lines[3].endswith("group msg 50")


def test_diagnostics_never_hit_stdout(server, capsys, monkeypatch):
    import whatsapp
    monkeypatch.setattr(whatsapp, "MESSAGES_DB_PATH", "/nonexistent/dir/messages.db")
    server.list_chats()
    assert capsys.readouterr().out == ""
