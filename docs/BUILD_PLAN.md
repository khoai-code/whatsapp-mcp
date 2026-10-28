# Build prompt: WhatsApp MCP (fork of lharries/whatsapp-mcp)

Paste everything below the line into a fresh Claude session in cmux.

---

You are building a WhatsApp MCP server for Khanh (GitHub: `khoai-code`) and their team.
It is a fork of https://github.com/lharries/whatsapp-mcp. It is **not** part of the Carlyle
wiki repo — never touch `~/Downloads/Carlyle`.

## Goal

Let Claude (Claude Code / Claude Desktop) read, search and summarise a person's WhatsApp
chats, and — only when that person opts in — send messages and do group admin. Use case:
internal team communication on WhatsApp.

## Fixed decisions (do not revisit)

1. **Unofficial linked-device protocol** via the upstream Go bridge (`whatsmeow`). Not Meta's
   Business Cloud API.
2. **One local install per person.** Each teammate pairs their own phone by QR. Nothing is
   hosted (no Dokploy, no shared server). The session store never leaves their machine.
3. **No LLM calls inside the server.** No Anthropic key, no LiteLLM. All reasoning happens in
   the user's own Claude session and counts against their own plan limits. The server only
   moves data.
4. **Read-only by default, writes are opt-in per person.** Fresh install exposes read tools
   only. `WHATSAPP_MCP_ALLOW_WRITE=true` turns on send + group-admin tools. Gradual rollout:
   teammates read/summarise first, enable sending once they trust it.
5. **Token-frugal tools.** Small default pages so one "summarise my chats" doesn't burn a
   teammate's session limit.

## Working rules

- Repo: fork to `khoai-code/whatsapp-mcp` with `gh repo fork lharries/whatsapp-mcp --clone`,
  clone into `~/code/whatsapp-mcp`. Never push to or open PRs against `lharries/*`.
- The fork is public. **Never commit** anything under the bridge's `store/` directory
  (`*.db`, session keys), `.env`, or QR images. Confirm `.gitignore` covers them before the
  first commit.
- One branch + PR per phase below, against the fork's `main`. Merge only after Khanh approves.
- Move this file into the repo as `docs/BUILD_PLAN.md` in Phase 0.
- My notes on upstream's layout and whatsmeow's API are from memory and may be stale.
  **Verify against the actual code and `go doc go.mau.fi/whatsmeow` before relying on any
  name or signature below.** If reality differs, follow reality and note it in the PR.
- Stop and report at every "STOP" marker.

## Expected upstream shape (verify in Phase 0)

- `whatsapp-bridge/` — Go, `main.go`, whatsmeow client. Persists messages to SQLite
  (`store/messages.db`) and the session to `store/whatsapp.db`. Serves a local REST API
  (port 8080) for send / download.
- `whatsapp-mcp-server/` — Python (uv, FastMCP). Read tools query the SQLite file directly;
  write tools call the bridge REST API.
- Tools roughly: `search_contacts`, `list_chats`, `get_chat`, `list_messages`,
  `get_message_context`, `get_direct_chat_by_contact`, `get_contact_chats`,
  `get_last_interaction`, `download_media` (read) and `send_message`, `send_file`,
  `send_audio_message` (write).

## Phase 0 — Fork and survey (no code changes)

1. Fork + clone as above. Run `go build` in the bridge and `uv sync` in the server to confirm
   both build on this Mac.
2. Report: actual file layout, every MCP tool with its params, every bridge HTTP endpoint,
   which address/port the bridge binds, whether the endpoints have any auth, the whatsmeow
   version in `go.mod`, the licence.
3. Commit `docs/BUILD_PLAN.md` (this file).

**STOP** — report findings before Phase 1.

## Phase 1 — Security hardening

1. Bridge binds `127.0.0.1` only, never `0.0.0.0`.
2. Bridge API auth: on first start the bridge generates a random token into
   `store/bridge.token` (mode 0600). Every request needs `Authorization: Bearer <token>`.
   The Python server reads the same file. Without this, any local process or web page that
   can reach localhost:8080 could send WhatsApp messages as the user.
3. `store/` created mode 0700, DB files 0600.
4. `.gitignore` covers `store/`, `*.db`, `.env`, `*.token`.
5. Never log message bodies or the token at info level.

## Phase 2 — Read-only default + write gating

1. `WHATSAPP_MCP_ALLOW_WRITE` (default `false`), enforced in **both** layers:
   - Python server: write tools are not registered at all unless the flag is true. Claude
     can't see them, so prompt injection can't call them.
   - Bridge: write endpoints return 403 unless the bridge was also started with the flag.
     Belt and braces.
2. Optional allowlist `WHATSAPP_MCP_SEND_ALLOWLIST` — comma-separated JIDs or phone numbers,
   enforced in the bridge. If set, any write to a chat not on it is rejected with a clear
   error. Unset = no allowlist.
3. Write tool descriptions say plainly that they act on the user's real WhatsApp account, so
   the per-call "allow this tool?" prompt is informative.
4. Add `get_status` (read): connected?, paired number, write enabled?, allowlist active?
5. Unit tests for both gates (flag off, flag on, allowlist hit, allowlist miss).

## Phase 3 — Token budget

1. Default `limit` 20 for message lists, 20 for chat lists; hard cap 100. Paginate with
   `before` / cursor.
2. Truncate any single message body over 1,000 chars, marking it `…[truncated, N chars]`.
3. Compact output: one line per message (`time · sender · text`) instead of verbose dicts.
4. Media is never inlined; return a reference, fetch only through `download_media`.
5. Add `summarise_hint` style helper only if it is pure data shaping (e.g. `unread_counts`).
   No model calls.

## Phase 4 — Group admin tools (write-gated)

Add bridge endpoints + MCP tools using whatsmeow (check current signatures — recent versions
take a `context.Context` first):

| MCP tool | whatsmeow (verify) |
|---|---|
| `list_groups` (read) | `GetJoinedGroups` |
| `get_group_info` (read) | `GetGroupInfo` |
| `get_group_invite_link` (read) | `GetGroupInviteLink` (reset=false) |
| `create_group` | `CreateGroup` |
| `add_participants` / `remove_participants` | `UpdateGroupParticipants` add/remove |
| `promote_admin` / `demote_admin` | `UpdateGroupParticipants` promote/demote |
| `set_group_name` | `SetGroupName` |
| `set_group_description` | `SetGroupTopic` |
| `leave_group` | `LeaveGroup` |
| `mark_read` | `MarkRead` |

Every write tool goes through the Phase 2 flag and allowlist. Group tools check the user is
admin first and return a clear error if not.

## Phase 5 — Install + teammate docs

1. Bridge has to stay running to receive messages, so install it as a macOS LaunchAgent
   (`~/Library/LaunchAgents/com.khoai.whatsapp-bridge.plist`, KeepAlive, logs to
   `~/Library/Logs/whatsapp-bridge.log`).
2. `make install` (or `./install.sh`): checks Go + uv, builds the bridge, installs the
   LaunchAgent, runs `uv sync`, then prints the exact commands below.
3. Claude Code registration:
   `claude mcp add --scope user whatsapp -- uv --directory ~/code/whatsapp-mcp/whatsapp-mcp-server run main.py`
   plus a Claude Desktop `claude_desktop_config.json` snippet.
4. First run: pairing flow prints the QR in the terminal; README explains WhatsApp → Linked
   devices → Link a device.
5. `make uninstall`: unloads the LaunchAgent, optionally wipes `store/` (asks first), and
   tells the user to remove the linked device from their phone too.
6. `README.md` for teammates, short:
   - what it does / doesn't do
   - read-only by default; how to turn on writing (`WHATSAPP_MCP_ALLOW_WRITE=true`) and the
     allowlist
   - usage counts against **your own** Claude plan
   - risks in plain words: unofficial API (ban risk is low for normal use, high for bulk
     sending — never bulk-send), anyone who messages you can try to steer Claude via message
     text, keep the read-only default until you trust it, don't run it in the same session
     as web-fetching tools when writing is on
   - troubleshooting: re-pair, bridge not running, logs location

## Phase 6 — Live test (needs Khanh)

**STOP** before this phase. Khanh must scan the QR, ideally with a spare number first.
Then test with them: list chats, read a group, summarise, confirm write tools are invisible
with the flag off, turn the flag on, send one message to themself, try a send outside the
allowlist (must fail), one group admin action in a test group.

## Report back after each phase

- PR link, what changed, test output (pass/fail with the real output)
- anything where upstream differed from this plan and what you did instead
- open questions for Khanh
