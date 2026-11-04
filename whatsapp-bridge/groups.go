package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// groupAPI is the subset of *whatsmeow.Client used for groups. Keeping it
// narrow lets tests use a fake and keeps whatsmeow upgrades to one file.
type groupAPI interface {
	GetJoinedGroups(ctx context.Context) ([]*types.GroupInfo, error)
	GetGroupInfo(ctx context.Context, jid types.JID) (*types.GroupInfo, error)
	GetGroupInviteLink(ctx context.Context, jid types.JID, reset bool) (string, error)
	CreateGroup(ctx context.Context, req whatsmeow.ReqCreateGroup) (*types.GroupInfo, error)
	UpdateGroupParticipants(ctx context.Context, jid types.JID, participants []types.JID, action whatsmeow.ParticipantChange) ([]types.GroupParticipant, error)
	SetGroupName(ctx context.Context, jid types.JID, name string) error
	SetGroupTopic(ctx context.Context, jid types.JID, previousID, newID, topic string) error
	LeaveGroup(ctx context.Context, jid types.JID) error
	MarkRead(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID, receiptTypeExtra ...types.ReceiptType) error
}

type groupHandlers struct {
	api    groupAPI
	policy *writePolicy
	// ownIDs returns the paired account's user parts: phone number and, when
	// known, LID. Groups may list members by either.
	ownIDs func() []string
	// onRename, if set, is told about successful renames so the local chat
	// list shows the new name straight away.
	onRename func(jid types.JID, name string)
}

// Group names are limited to 25 characters by WhatsApp.
const maxGroupNameLen = 25

type GroupSummary struct {
	JID          string `json:"jid"`
	Name         string `json:"name"`
	Participants int    `json:"participants"`
	IsAdmin      bool   `json:"is_admin"`
}

type GroupMember struct {
	JID          string `json:"jid"`
	IsAdmin      bool   `json:"is_admin"`
	IsSuperAdmin bool   `json:"is_super_admin"`
}

type GroupDetail struct {
	JID         string        `json:"jid"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Owner       string        `json:"owner,omitempty"`
	Created     time.Time     `json:"created"`
	Locked      bool          `json:"locked"`
	Announce    bool          `json:"announce"`
	IsAdmin     bool          `json:"is_admin"`
	Members     []GroupMember `json:"members"`
}

type GroupRequest struct {
	GroupJID     string   `json:"group_jid"`
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	Action       string   `json:"action,omitempty"`
	Participants []string `json:"participants,omitempty"`
}

type MarkReadRequest struct {
	ChatJID    string   `json:"chat_jid"`
	MessageIDs []string `json:"message_ids"`
	Sender     string   `json:"sender,omitempty"`
}

type GroupResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	// Per-participant outcome for add/remove/promote/demote; WhatsApp can
	// accept some changes and reject others in one call.
	Failed []string `json:"failed,omitempty"`
	JID    string   `json:"jid,omitempty"`
	Link   string   `json:"link,omitempty"`
}

func (g *groupHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/groups", g.method(http.MethodGet, g.listGroups))
	mux.HandleFunc("/api/group/info", g.method(http.MethodPost, g.groupInfo))
	mux.HandleFunc("/api/group/invite-link", g.method(http.MethodPost, g.inviteLink))
	mux.HandleFunc("/api/group/create", g.method(http.MethodPost, g.createGroup))
	mux.HandleFunc("/api/group/participants", g.method(http.MethodPost, g.updateParticipants))
	mux.HandleFunc("/api/group/name", g.method(http.MethodPost, g.setName))
	mux.HandleFunc("/api/group/description", g.method(http.MethodPost, g.setDescription))
	mux.HandleFunc("/api/group/leave", g.method(http.MethodPost, g.leave))
	mux.HandleFunc("/api/mark-read", g.method(http.MethodPost, g.markRead))
}

func (g *groupHandlers) method(m string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != m {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, format string, args ...interface{}) {
	writeJSON(w, status, GroupResult{Success: false, Message: fmt.Sprintf(format, args...)})
}

func decode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "Invalid request format")
		return false
	}
	return true
}

func parseGroupJID(s string) (types.JID, error) {
	jid, err := types.ParseJID(strings.TrimSpace(s))
	if err != nil {
		return types.JID{}, fmt.Errorf("invalid group JID: %v", err)
	}
	if jid.Server != types.GroupServer {
		return types.JID{}, fmt.Errorf("%s is not a group JID (expected ...@g.us)", s)
	}
	return jid, nil
}

func parseUserJIDs(list []string) ([]types.JID, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("at least one participant is required")
	}
	out := make([]types.JID, 0, len(list))
	for _, p := range list {
		norm, err := normaliseRecipient(p)
		if err != nil {
			return nil, fmt.Errorf("invalid participant %q: %v", p, err)
		}
		jid, err := types.ParseJID(norm)
		if err != nil || jid.Server != types.DefaultUserServer {
			return nil, fmt.Errorf("participant %q must be a phone number or ...@s.whatsapp.net JID", p)
		}
		out = append(out, jid)
	}
	return out, nil
}

func (g *groupHandlers) isAdmin(info *types.GroupInfo) bool {
	own := map[string]bool{}
	for _, id := range g.ownIDs() {
		if id != "" {
			own[id] = true
		}
	}
	for _, p := range info.Participants {
		if !(p.IsAdmin || p.IsSuperAdmin) {
			continue
		}
		if own[p.JID.User] || own[p.PhoneNumber.User] || own[p.LID.User] {
			return true
		}
	}
	return false
}

// loadGroup fetches group info and, when needAdmin is set, refuses unless the
// paired account is an admin, so the user gets a clear error instead of a
// raw WhatsApp 403.
func (g *groupHandlers) loadGroup(ctx context.Context, w http.ResponseWriter, raw string, needAdmin bool) (*types.GroupInfo, bool) {
	jid, err := parseGroupJID(raw)
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return nil, false
	}
	info, err := g.api.GetGroupInfo(ctx, jid)
	if err != nil {
		fail(w, http.StatusBadGateway, "Failed to get group info: %v", err)
		return nil, false
	}
	if needAdmin && !g.isAdmin(info) {
		fail(w, http.StatusForbidden, "You are not an admin of %q, so this action is not allowed", info.Name)
		return nil, false
	}
	return info, true
}

func (g *groupHandlers) checkWrite(w http.ResponseWriter, targets ...string) bool {
	for _, t := range targets {
		if err := g.policy.checkWrite(t); err != nil {
			writeForbidden(w, err)
			return false
		}
	}
	return true
}

// Read endpoints

func (g *groupHandlers) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := g.api.GetJoinedGroups(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, "Failed to list groups: %v", err)
		return
	}
	out := make([]GroupSummary, 0, len(groups))
	for _, info := range groups {
		out = append(out, GroupSummary{
			JID:          info.JID.String(),
			Name:         info.Name,
			Participants: len(info.Participants),
			IsAdmin:      g.isAdmin(info),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *groupHandlers) groupInfo(w http.ResponseWriter, r *http.Request) {
	var req GroupRequest
	if !decode(w, r, &req) {
		return
	}
	info, ok := g.loadGroup(r.Context(), w, req.GroupJID, false)
	if !ok {
		return
	}
	detail := GroupDetail{
		JID:         info.JID.String(),
		Name:        info.Name,
		Description: info.Topic,
		Created:     info.GroupCreated,
		Locked:      info.IsLocked,
		Announce:    info.IsAnnounce,
		IsAdmin:     g.isAdmin(info),
	}
	if !info.OwnerJID.IsEmpty() {
		detail.Owner = info.OwnerJID.User
	}
	for _, p := range info.Participants {
		detail.Members = append(detail.Members, GroupMember{
			JID:          p.JID.String(),
			IsAdmin:      p.IsAdmin,
			IsSuperAdmin: p.IsSuperAdmin,
		})
	}
	writeJSON(w, http.StatusOK, detail)
}

func (g *groupHandlers) inviteLink(w http.ResponseWriter, r *http.Request) {
	var req GroupRequest
	if !decode(w, r, &req) {
		return
	}
	// WhatsApp only hands invite links to admins.
	info, ok := g.loadGroup(r.Context(), w, req.GroupJID, true)
	if !ok {
		return
	}
	// reset=false: never revoke the existing link from a read tool.
	link, err := g.api.GetGroupInviteLink(r.Context(), info.JID, false)
	if err != nil {
		fail(w, http.StatusBadGateway, "Failed to get invite link: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, GroupResult{Success: true, Message: "ok", Link: link})
}

// Write endpoints. Each goes through the write policy before touching WhatsApp.

func (g *groupHandlers) createGroup(w http.ResponseWriter, r *http.Request) {
	var req GroupRequest
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > maxGroupNameLen {
		fail(w, http.StatusBadRequest, "Group name must be 1-%d characters", maxGroupNameLen)
		return
	}
	participants, err := parseUserJIDs(req.Participants)
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	// With an allowlist active, everyone added must be on it.
	if !g.checkWrite(w, req.Participants...) {
		return
	}
	info, err := g.api.CreateGroup(r.Context(), whatsmeow.ReqCreateGroup{Name: name, Participants: participants})
	if err != nil {
		fail(w, http.StatusBadGateway, "Failed to create group: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, GroupResult{
		Success: true,
		Message: fmt.Sprintf("Created group %q", info.Name),
		JID:     info.JID.String(),
		Failed:  failedParticipants(info.Participants),
	})
}

var participantActions = map[string]whatsmeow.ParticipantChange{
	"add":     whatsmeow.ParticipantChangeAdd,
	"remove":  whatsmeow.ParticipantChangeRemove,
	"promote": whatsmeow.ParticipantChangePromote,
	"demote":  whatsmeow.ParticipantChangeDemote,
}

func (g *groupHandlers) updateParticipants(w http.ResponseWriter, r *http.Request) {
	var req GroupRequest
	if !decode(w, r, &req) {
		return
	}
	action, ok := participantActions[req.Action]
	if !ok {
		fail(w, http.StatusBadRequest, "action must be add, remove, promote or demote")
		return
	}
	participants, err := parseUserJIDs(req.Participants)
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	if !g.checkWrite(w, req.GroupJID) {
		return
	}
	// Adding someone also messages them into a group, so they must pass the
	// allowlist too.
	if action == whatsmeow.ParticipantChangeAdd && !g.checkWrite(w, req.Participants...) {
		return
	}
	info, ok := g.loadGroup(r.Context(), w, req.GroupJID, true)
	if !ok {
		return
	}
	result, err := g.api.UpdateGroupParticipants(r.Context(), info.JID, participants, action)
	if err != nil {
		fail(w, http.StatusBadGateway, "Failed to %s participants: %v", req.Action, err)
		return
	}
	failed := failedParticipants(result)
	writeJSON(w, http.StatusOK, GroupResult{
		Success: len(failed) < len(participants),
		Message: fmt.Sprintf("%s: %d ok, %d failed", req.Action, len(participants)-len(failed), len(failed)),
		Failed:  failed,
	})
}

func failedParticipants(ps []types.GroupParticipant) []string {
	var failed []string
	for _, p := range ps {
		if p.Error != 0 {
			failed = append(failed, fmt.Sprintf("%s (error %d)", p.JID.User, p.Error))
		}
	}
	return failed
}

// canEditInfo: admins always can; members can when the group is not locked.
func (g *groupHandlers) canEditInfo(info *types.GroupInfo) bool {
	return !info.IsLocked || g.isAdmin(info)
}

func (g *groupHandlers) setName(w http.ResponseWriter, r *http.Request) {
	var req GroupRequest
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > maxGroupNameLen {
		fail(w, http.StatusBadRequest, "Group name must be 1-%d characters", maxGroupNameLen)
		return
	}
	if !g.checkWrite(w, req.GroupJID) {
		return
	}
	info, ok := g.loadGroup(r.Context(), w, req.GroupJID, false)
	if !ok {
		return
	}
	if !g.canEditInfo(info) {
		fail(w, http.StatusForbidden, "Only admins can edit %q and you are not an admin", info.Name)
		return
	}
	if err := g.api.SetGroupName(r.Context(), info.JID, name); err != nil {
		fail(w, http.StatusBadGateway, "Failed to set group name: %v", err)
		return
	}
	if g.onRename != nil {
		g.onRename(info.JID, name)
	}
	writeJSON(w, http.StatusOK, GroupResult{Success: true, Message: fmt.Sprintf("Renamed to %q", name)})
}

func (g *groupHandlers) setDescription(w http.ResponseWriter, r *http.Request) {
	var req GroupRequest
	if !decode(w, r, &req) {
		return
	}
	if !g.checkWrite(w, req.GroupJID) {
		return
	}
	info, ok := g.loadGroup(r.Context(), w, req.GroupJID, false)
	if !ok {
		return
	}
	if !g.canEditInfo(info) {
		fail(w, http.StatusForbidden, "Only admins can edit %q and you are not an admin", info.Name)
		return
	}
	if err := g.api.SetGroupTopic(r.Context(), info.JID, info.TopicID, "", req.Description); err != nil {
		fail(w, http.StatusBadGateway, "Failed to set group description: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, GroupResult{Success: true, Message: "Description updated"})
}

func (g *groupHandlers) leave(w http.ResponseWriter, r *http.Request) {
	var req GroupRequest
	if !decode(w, r, &req) {
		return
	}
	if !g.checkWrite(w, req.GroupJID) {
		return
	}
	jid, err := parseGroupJID(req.GroupJID)
	if err != nil {
		fail(w, http.StatusBadRequest, "%v", err)
		return
	}
	if err := g.api.LeaveGroup(r.Context(), jid); err != nil {
		fail(w, http.StatusBadGateway, "Failed to leave group: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, GroupResult{Success: true, Message: "Left group"})
}

func (g *groupHandlers) markRead(w http.ResponseWriter, r *http.Request) {
	var req MarkReadRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.MessageIDs) == 0 {
		fail(w, http.StatusBadRequest, "message_ids is required")
		return
	}
	if !g.checkWrite(w, req.ChatJID) {
		return
	}
	chat, err := types.ParseJID(req.ChatJID)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid chat_jid: %v", err)
		return
	}
	// In groups WhatsApp needs the original sender of the messages.
	var sender types.JID
	if chat.Server == types.GroupServer {
		if req.Sender == "" {
			fail(w, http.StatusBadRequest, "sender is required for group chats")
			return
		}
		norm, err := normaliseRecipient(req.Sender)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid sender: %v", err)
			return
		}
		sender, _ = types.ParseJID(norm)
	}
	ids := make([]types.MessageID, len(req.MessageIDs))
	for i, id := range req.MessageIDs {
		ids[i] = types.MessageID(id)
	}
	if err := g.api.MarkRead(r.Context(), ids, time.Now(), chat, sender); err != nil {
		fail(w, http.StatusBadGateway, "Failed to mark read: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, GroupResult{Success: true, Message: fmt.Sprintf("Marked %d message(s) read", len(ids))})
}
