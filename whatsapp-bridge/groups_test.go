package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

const (
	me       = "84000000000"
	friend   = "84111111111"
	stranger = "84999999999"
	groupJID = "120363000000000000@g.us"
)

// fakeGroups records calls instead of talking to WhatsApp.
type fakeGroups struct {
	info  *types.GroupInfo
	calls []string
}

func user(u string) types.JID { return types.NewJID(u, types.DefaultUserServer) }

func newFakeGroup(meAdmin, locked bool) *fakeGroups {
	jid, _ := types.ParseJID(groupJID)
	info := &types.GroupInfo{JID: jid, Participants: []types.GroupParticipant{
		{JID: user(me), IsAdmin: meAdmin},
		{JID: user(friend)},
	}}
	info.Name = "Team"
	info.TopicID = "topic-1"
	info.IsLocked = locked
	return &fakeGroups{info: info}
}

func (f *fakeGroups) GetJoinedGroups() ([]*types.GroupInfo, error) {
	return []*types.GroupInfo{f.info}, nil
}
func (f *fakeGroups) GetGroupInfo(jid types.JID) (*types.GroupInfo, error) { return f.info, nil }
func (f *fakeGroups) GetGroupInviteLink(jid types.JID, reset bool) (string, error) {
	f.calls = append(f.calls, "invite reset="+map[bool]string{true: "true", false: "false"}[reset])
	return "https://chat.whatsapp.com/abc", nil
}
func (f *fakeGroups) CreateGroup(req whatsmeow.ReqCreateGroup) (*types.GroupInfo, error) {
	f.calls = append(f.calls, "create "+req.Name)
	return f.info, nil
}
func (f *fakeGroups) UpdateGroupParticipants(jid types.JID, ps []types.JID, action whatsmeow.ParticipantChange) ([]types.GroupParticipant, error) {
	f.calls = append(f.calls, string(action))
	out := make([]types.GroupParticipant, len(ps))
	for i, p := range ps {
		out[i] = types.GroupParticipant{JID: p}
		if p.User == stranger {
			out[i].Error = 403
		}
	}
	return out, nil
}
func (f *fakeGroups) SetGroupName(jid types.JID, name string) error {
	f.calls = append(f.calls, "name "+name)
	return nil
}
func (f *fakeGroups) SetGroupTopic(jid types.JID, prev, newID, topic string) error {
	f.calls = append(f.calls, "topic prev="+prev+" "+topic)
	return nil
}
func (f *fakeGroups) LeaveGroup(jid types.JID) error {
	f.calls = append(f.calls, "leave")
	return nil
}
func (f *fakeGroups) MarkRead(ids []types.MessageID, ts time.Time, chat, sender types.JID, _ ...types.ReceiptType) error {
	f.calls = append(f.calls, "read "+string(ids[0])+" sender="+sender.User)
	return nil
}

func serve(t *testing.T, fake *fakeGroups, policy *writePolicy, method, path string, body interface{}) (*httptest.ResponseRecorder, GroupResult) {
	t.Helper()
	mux := http.NewServeMux()
	(&groupHandlers{api: fake, policy: policy, ownUser: func() string { return me }}).register(mux)
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	var res GroupResult
	json.Unmarshal(rec.Body.Bytes(), &res)
	return rec, res
}

func writeOn() *writePolicy  { return &writePolicy{allowWrite: true, allowlist: map[string]bool{}} }
func writeOff() *writePolicy { return &writePolicy{allowlist: map[string]bool{}} }

// Every write endpoint must 403 with writes off and never reach WhatsApp.
func TestGroupWritesBlockedWhenFlagOff(t *testing.T) {
	writes := map[string]interface{}{
		"/api/group/create":       GroupRequest{Name: "New", Participants: []string{friend}},
		"/api/group/participants": GroupRequest{GroupJID: groupJID, Action: "add", Participants: []string{friend}},
		"/api/group/name":         GroupRequest{GroupJID: groupJID, Name: "X"},
		"/api/group/description":  GroupRequest{GroupJID: groupJID, Description: "d"},
		"/api/group/leave":        GroupRequest{GroupJID: groupJID},
		"/api/mark-read":          MarkReadRequest{ChatJID: groupJID, MessageIDs: []string{"m1"}, Sender: friend},
	}
	for path, body := range writes {
		fake := newFakeGroup(true, false)
		rec, _ := serve(t, fake, writeOff(), http.MethodPost, path, body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", path, rec.Code)
		}
		if len(fake.calls) != 0 {
			t.Errorf("%s: reached WhatsApp: %v", path, fake.calls)
		}
	}
}

func TestGroupReadsWorkWhenFlagOff(t *testing.T) {
	fake := newFakeGroup(true, false)
	rec, _ := serve(t, fake, writeOff(), http.MethodGet, "/api/groups", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"is_admin":true`) {
		t.Errorf("list groups: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = serve(t, fake, writeOff(), http.MethodPost, "/api/group/info", GroupRequest{GroupJID: groupJID})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Team"`) {
		t.Errorf("group info: %d %s", rec.Code, rec.Body.String())
	}
	rec, res := serve(t, fake, writeOff(), http.MethodPost, "/api/group/invite-link", GroupRequest{GroupJID: groupJID})
	if rec.Code != http.StatusOK || res.Link == "" {
		t.Errorf("invite link: %d %s", rec.Code, rec.Body.String())
	}
	if fake.calls[0] != "invite reset=false" {
		t.Errorf("invite link must never reset: %v", fake.calls)
	}
}

func TestAdminRequiredForParticipantsAndInvite(t *testing.T) {
	for _, action := range []string{"add", "remove", "promote", "demote"} {
		fake := newFakeGroup(false, false)
		rec, res := serve(t, fake, writeOn(), http.MethodPost, "/api/group/participants",
			GroupRequest{GroupJID: groupJID, Action: action, Participants: []string{friend}})
		if rec.Code != http.StatusForbidden || !strings.Contains(res.Message, "not an admin") {
			t.Errorf("%s as non-admin: %d %q", action, rec.Code, res.Message)
		}
		if len(fake.calls) != 0 {
			t.Errorf("%s: reached WhatsApp: %v", action, fake.calls)
		}
	}
	rec, _ := serve(t, newFakeGroup(false, false), writeOff(), http.MethodPost, "/api/group/invite-link", GroupRequest{GroupJID: groupJID})
	if rec.Code != http.StatusForbidden {
		t.Errorf("invite link as non-admin: %d", rec.Code)
	}
}

func TestParticipantsAsAdminReportsPartialFailure(t *testing.T) {
	fake := newFakeGroup(true, false)
	rec, res := serve(t, fake, writeOn(), http.MethodPost, "/api/group/participants",
		GroupRequest{GroupJID: groupJID, Action: "add", Participants: []string{friend, stranger}})
	if rec.Code != http.StatusOK || !res.Success {
		t.Fatalf("status %d %+v", rec.Code, res)
	}
	if len(res.Failed) != 1 || !strings.HasPrefix(res.Failed[0], stranger) {
		t.Errorf("failed = %v, want only %s", res.Failed, stranger)
	}
}

func TestEditInfoLockedVsUnlocked(t *testing.T) {
	// Unlocked group: any member may rename.
	fake := newFakeGroup(false, false)
	rec, _ := serve(t, fake, writeOn(), http.MethodPost, "/api/group/name", GroupRequest{GroupJID: groupJID, Name: "Renamed"})
	if rec.Code != http.StatusOK {
		t.Errorf("unlocked rename as member: %d", rec.Code)
	}
	// Locked group: members may not.
	fake = newFakeGroup(false, true)
	rec, _ = serve(t, fake, writeOn(), http.MethodPost, "/api/group/description", GroupRequest{GroupJID: groupJID, Description: "d"})
	if rec.Code != http.StatusForbidden || len(fake.calls) != 0 {
		t.Errorf("locked description as member: %d %v", rec.Code, fake.calls)
	}
	// Locked group as admin: allowed, and the previous topic id is passed on.
	fake = newFakeGroup(true, true)
	rec, _ = serve(t, fake, writeOn(), http.MethodPost, "/api/group/description", GroupRequest{GroupJID: groupJID, Description: "new"})
	if rec.Code != http.StatusOK || fake.calls[0] != "topic prev=topic-1 new" {
		t.Errorf("locked description as admin: %d %v", rec.Code, fake.calls)
	}
}

func TestGroupNameLength(t *testing.T) {
	long := strings.Repeat("x", maxGroupNameLen+1)
	rec, _ := serve(t, newFakeGroup(true, false), writeOn(), http.MethodPost, "/api/group/name", GroupRequest{GroupJID: groupJID, Name: long})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("long name: %d", rec.Code)
	}
	rec, _ = serve(t, newFakeGroup(true, false), writeOn(), http.MethodPost, "/api/group/create", GroupRequest{Name: long, Participants: []string{friend}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("long create name: %d", rec.Code)
	}
}

func TestGroupAllowlist(t *testing.T) {
	allow := func(entries ...string) *writePolicy {
		p := writeOn()
		for _, e := range entries {
			jid, _ := normaliseRecipient(e)
			p.allowlist[jid] = true
		}
		return p
	}
	// Group not on allowlist: rename blocked.
	fake := newFakeGroup(true, false)
	rec, _ := serve(t, fake, allow(friend), http.MethodPost, "/api/group/name", GroupRequest{GroupJID: groupJID, Name: "X"})
	if rec.Code != http.StatusForbidden || len(fake.calls) != 0 {
		t.Errorf("rename off-allowlist group: %d %v", rec.Code, fake.calls)
	}
	// Group allowed, but adding someone not on the allowlist is blocked.
	fake = newFakeGroup(true, false)
	rec, _ = serve(t, fake, allow(groupJID, friend), http.MethodPost, "/api/group/participants",
		GroupRequest{GroupJID: groupJID, Action: "add", Participants: []string{stranger}})
	if rec.Code != http.StatusForbidden || len(fake.calls) != 0 {
		t.Errorf("add off-allowlist person: %d %v", rec.Code, fake.calls)
	}
	// Removing someone only needs the group on the allowlist.
	fake = newFakeGroup(true, false)
	rec, _ = serve(t, fake, allow(groupJID), http.MethodPost, "/api/group/participants",
		GroupRequest{GroupJID: groupJID, Action: "remove", Participants: []string{stranger}})
	if rec.Code != http.StatusOK {
		t.Errorf("remove from allowed group: %d", rec.Code)
	}
	// Create needs every participant on the allowlist.
	fake = newFakeGroup(true, false)
	rec, _ = serve(t, fake, allow(friend), http.MethodPost, "/api/group/create",
		GroupRequest{Name: "New", Participants: []string{friend, stranger}})
	if rec.Code != http.StatusForbidden || len(fake.calls) != 0 {
		t.Errorf("create with off-allowlist person: %d %v", rec.Code, fake.calls)
	}
}

func TestBadInputs(t *testing.T) {
	cases := []struct {
		path string
		body interface{}
	}{
		{"/api/group/participants", GroupRequest{GroupJID: groupJID, Action: "ban", Participants: []string{friend}}},
		{"/api/group/participants", GroupRequest{GroupJID: groupJID, Action: "add"}},
		{"/api/group/name", GroupRequest{GroupJID: friend + "@s.whatsapp.net", Name: "X"}},
		{"/api/mark-read", MarkReadRequest{ChatJID: groupJID, MessageIDs: []string{"m1"}}},
		{"/api/mark-read", MarkReadRequest{ChatJID: groupJID}},
	}
	for _, c := range cases {
		rec, _ := serve(t, newFakeGroup(true, false), writeOn(), http.MethodPost, c.path, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %+v: status %d, want 400", c.path, c.body, rec.Code)
		}
	}
}

func TestMarkReadAndLeave(t *testing.T) {
	fake := newFakeGroup(false, false)
	rec, _ := serve(t, fake, writeOn(), http.MethodPost, "/api/mark-read",
		MarkReadRequest{ChatJID: groupJID, MessageIDs: []string{"m1"}, Sender: friend})
	if rec.Code != http.StatusOK || fake.calls[0] != "read m1 sender="+friend {
		t.Errorf("mark read: %d %v", rec.Code, fake.calls)
	}
	rec, _ = serve(t, fake, writeOn(), http.MethodPost, "/api/group/leave", GroupRequest{GroupJID: groupJID})
	if rec.Code != http.StatusOK || fake.calls[1] != "leave" {
		t.Errorf("leave: %d %v", rec.Code, fake.calls)
	}
}

func TestWrongMethod(t *testing.T) {
	rec, _ := serve(t, newFakeGroup(true, false), writeOn(), http.MethodGet, "/api/group/leave", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET leave: %d", rec.Code)
	}
}
