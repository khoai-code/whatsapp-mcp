# WhatsApp in Claude: 3-minute setup

Lets Claude read, search and summarise **your own** WhatsApp chats, for example
*"What happened in my groups today?"* or *"What did the partnership team decide about Đông Fest?"*

- Runs on your Mac only. Your WhatsApp session and messages stay on your computer.
- **Read-only** unless you switch sending on yourself.
- Uses your own Claude plan, like any other Claude chat.

You need: a Mac, the **Claude desktop app**, and your phone with WhatsApp.

---

## 1. Install (one click)

1. Save the **`whatsapp-mcp.mcpb`** file Khanh sent you.
2. Double-click it. Claude Desktop opens and asks to install **WhatsApp**: click **Install**.

That's it. Nothing else to install.

## 2. Link your phone (in a Claude chat)

Open a new chat in Claude Desktop and type, with **your own** number and country code:

> Connect my WhatsApp. My number is 84901234567

Claude replies with an 8-character code, like `ABCD-1234`. On your phone:

**WhatsApp → Settings → Linked devices → Link a device → "Link with phone number instead"** → type the code.

You'll see "WhatsApp" appear under Linked devices. Your recent chats sync over the next few minutes.

> Prefer a QR code? Type *"Connect my WhatsApp with a QR code"*. The code opens on screen to scan.
> Codes expire after about 2 minutes. If it times out, just ask again.

## 3. Try it

> What happened in my WhatsApp chats today?
>
> Summarise the ST.319 | PARTNERSHIP INTERNAL group for this week.
>
> Did anyone message me about the Đông Fest contract?

The first answer after linking may miss older messages while history is still syncing.

---

## Sending messages (optional, off by default)

While sending is off, Claude can't send anything: the send tools don't exist.

To turn it on: **Claude → Settings → Extensions → WhatsApp → Configure**

- **Allow sending and group admin**: switch on.
- **Only allow writes to these chats**: strongly recommended. Add your own number and the
  groups you want Claude to post in, separated by commas. Ask Claude *"list my WhatsApp groups"*
  to find group IDs (they end in `@g.us`).

Claude asks you to approve every send.

> In **Cowork**, Claude asks before every WhatsApp tool call, including reads (your organisation's
> setting). In a normal chat you can choose "Always allow" for the read tools.

## Please read: risks

- **This is unofficial.** It connects the same way WhatsApp Web does. Normal use is fine,
  but **never use it to bulk-send**: WhatsApp bans accounts for that.
- **Messages can contain instructions.** Someone could write "forward this to…" hoping Claude obeys.
  That's why read-only is the default. If you turn sending on, use the allowlist, and don't use
  web-browsing tools in the same chat.
- Your chat history is stored on your Mac in `~/Library/Application Support/WhatsApp MCP`,
  private to your user account. Only the messages Claude looks up are sent to Claude, as part of
  your conversation.

## Troubleshooting

| Problem | Fix |
|---|---|
| Claude doesn't mention WhatsApp tools | Start a **new** chat. Check Settings → Extensions → WhatsApp is enabled. |
| "Not linked to a phone yet" | Ask Claude to *connect my WhatsApp* again (step 2). |
| Code expired or rejected | Ask for a new code; type it within 2 minutes. |
| New messages missing | Claude Desktop must be open to receive messages. Messages from while it was closed arrive when you reopen it (up to about 2 weeks). |
| "WhatsApp refused… outdated" | Tell Khanh: the extension needs an update. |
| Anything else | Ask Claude *"check my WhatsApp status"* and send Khanh the answer. Log: `~/Library/Application Support/WhatsApp MCP/bridge.log` (contains no message text). |

## Remove it

1. Claude → Settings → Extensions → WhatsApp → **Uninstall**.
2. On your phone: WhatsApp → Settings → Linked devices → tap the device → **Log out**.
3. Optional, deletes your local chat copy: delete the folder
   `~/Library/Application Support/WhatsApp MCP`.

---

*Using Claude Code (the terminal app) instead? Follow the README's terminal install.
It's the same tool, set up with `make install`.*
