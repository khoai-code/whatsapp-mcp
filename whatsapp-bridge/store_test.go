package main

import (
	"net/http"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
)

func newTestStore(t *testing.T) *MessageStore {
	t.Helper()
	chdirTemp(t)
	s, err := NewMessageStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRecordSentStoresMessageAndKeepsChatName(t *testing.T) {
	s := newTestStore(t)
	old := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	if err := s.StoreChat(groupJID, "Admin", old); err != nil {
		t.Fatal(err)
	}
	sent := old.Add(time.Hour)
	if err := s.RecordSent("ABC", groupJID, me, "test", sent, "", ""); err != nil {
		t.Fatal(err)
	}

	var content string
	var fromMe bool
	if err := s.db.QueryRow("SELECT content, is_from_me FROM messages WHERE id = 'ABC'").Scan(&content, &fromMe); err != nil {
		t.Fatal(err)
	}
	if content != "test" || !fromMe {
		t.Errorf("stored (%q, %t), want (test, true)", content, fromMe)
	}

	var name string
	var last time.Time
	if err := s.db.QueryRow("SELECT name, last_message_time FROM chats WHERE jid = ?", groupJID).Scan(&name, &last); err != nil {
		t.Fatal(err)
	}
	if name != "Admin" {
		t.Errorf("chat name = %q, want Admin (must not be blanked)", name)
	}
	if !last.Equal(sent) {
		t.Errorf("last_message_time = %v, want %v", last, sent)
	}
}

func TestRecordSentCreatesUnknownChat(t *testing.T) {
	s := newTestStore(t)
	chat := friend + "@s.whatsapp.net"
	if err := s.RecordSent("XYZ", chat, me, "", time.Now(), "image", "a.jpg"); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM chats WHERE jid = ?", chat).Scan(&n)
	if n != 1 {
		t.Errorf("chat rows = %d, want 1", n)
	}
	var media string
	s.db.QueryRow("SELECT media_type FROM messages WHERE id = 'XYZ'").Scan(&media)
	if media != "image" {
		t.Errorf("media_type = %q, want image", media)
	}
}

func TestRenameUpdatesLocalChat(t *testing.T) {
	s := newTestStore(t)
	s.StoreChat(groupJID, "Admin", time.Now())

	fake := newFakeGroup(true, false)
	mux := http.NewServeMux()
	(&groupHandlers{
		api: fake, policy: writeOn(), ownIDs: func() []string { return []string{me} },
		onRename: func(jid types.JID, name string) { s.RenameChat(jid.String(), name) },
	}).register(mux)
	rec := serveOn(mux, http.MethodPost, "/api/group/name", GroupRequest{GroupJID: groupJID, Name: "ST.319 | ADMIN"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var name string
	s.db.QueryRow("SELECT name FROM chats WHERE jid = ?", groupJID).Scan(&name)
	if name != "ST.319 | ADMIN" {
		t.Errorf("local name = %q, want the new name", name)
	}
}
