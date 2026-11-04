import sqlite3
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import whatsapp

LID = "257010893332504"
PN = "84901234567"


@pytest.fixture
def dbs(monkeypatch, tmp_path):
    messages = tmp_path / "messages.db"
    conn = sqlite3.connect(messages)
    conn.executescript("""
        CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
        INSERT INTO chats VALUES ('84111111111@s.whatsapp.net', 'Alice', NULL);
        INSERT INTO chats VALUES ('84222222222@s.whatsapp.net', '84222222222', NULL);
        INSERT INTO chats VALUES ('99999@s.whatsapp.net', 'Wrong Person', NULL);
    """)
    conn.commit(); conn.close()

    session = tmp_path / "whatsapp.db"
    conn = sqlite3.connect(session)
    conn.executescript(f"""
        CREATE TABLE whatsmeow_lid_map (lid TEXT PRIMARY KEY, pn TEXT UNIQUE NOT NULL);
        INSERT INTO whatsmeow_lid_map VALUES ('{LID}', '{PN}');
        CREATE TABLE whatsmeow_contacts (our_jid TEXT, their_jid TEXT, first_name TEXT, full_name TEXT,
            push_name TEXT, business_name TEXT, redacted_phone TEXT, PRIMARY KEY (our_jid, their_jid));
        INSERT INTO whatsmeow_contacts VALUES ('me', '{PN}@s.whatsapp.net', 'Bob', 'Bob Tran', 'bobby', '', '');
        INSERT INTO whatsmeow_contacts VALUES ('me', '84222222222@s.whatsapp.net', '', '', 'Carol', '', '');
    """)
    conn.commit(); conn.close()

    monkeypatch.setattr(whatsapp, "MESSAGES_DB_PATH", str(messages))
    monkeypatch.setattr(whatsapp, "SESSION_DB_PATH", str(session))


@pytest.mark.parametrize("sender", [f"{LID}@lid", LID])
def test_lid_sender_resolves_to_contact_name(dbs, sender):
    # History sync stores "<lid>@lid"; live messages store the bare LID.
    assert whatsapp.get_sender_name(sender) == "Bob Tran"


def test_phone_sender_uses_chat_name(dbs):
    assert whatsapp.get_sender_name("84111111111") == "Alice"
    assert whatsapp.get_sender_name("84111111111@s.whatsapp.net") == "Alice"


def test_chat_named_after_its_number_falls_through_to_contact(dbs):
    assert whatsapp.get_sender_name("84222222222") == "Carol"


def test_unknown_lid_is_shown_raw(dbs):
    assert whatsapp.get_sender_name("123456789012345@lid") == "123456789012345@lid"


def test_unknown_phone_shown_as_plus_number(dbs):
    assert whatsapp.get_sender_name("84333333333") == "+84333333333"


def test_no_substring_matching(dbs):
    # Upstream matched jid LIKE '%9999%' and could return someone else's name.
    assert whatsapp.get_sender_name("9999") == "+9999"


def test_missing_session_db_is_harmless(dbs, monkeypatch, tmp_path):
    monkeypatch.setattr(whatsapp, "SESSION_DB_PATH", str(tmp_path / "missing.db"))
    assert whatsapp.get_sender_name(f"{LID}@lid") == f"{LID}@lid"
    assert whatsapp.get_sender_name("84111111111") == "Alice"
