import os
from datetime import datetime
from typing import List, Dict, Any, Optional
from mcp.server.fastmcp import FastMCP
import formatting as fmt
from whatsapp import (
    search_contacts as whatsapp_search_contacts,
    list_messages as whatsapp_list_messages,
    list_chats as whatsapp_list_chats,
    get_chat as whatsapp_get_chat,
    get_direct_chat_by_contact as whatsapp_get_direct_chat_by_contact,
    get_contact_chats as whatsapp_get_contact_chats,
    get_last_interaction as whatsapp_get_last_interaction,
    get_message_context as whatsapp_get_message_context,
    send_message as whatsapp_send_message,
    send_file as whatsapp_send_file,
    send_audio_message as whatsapp_audio_voice_message,
    download_media as whatsapp_download_media,
    get_status as whatsapp_get_status,
    get_sender_name as whatsapp_get_sender_name,
    recent_activity as whatsapp_recent_activity
)

# Write tools (send, group admin) are only registered when this is "true".
# Unregistered tools are invisible to Claude, so message text can't trick it
# into calling them. The bridge enforces the same flag independently.
WRITE_ENABLED = os.environ.get("WHATSAPP_MCP_ALLOW_WRITE", "").strip().lower() == "true"

# Initialize FastMCP server
mcp = FastMCP("whatsapp")

def _name_lookup():
    """Sender display names, cached for the duration of one tool call."""
    cache: Dict[str, str] = {}

    def name(message) -> str:
        if message.is_from_me:
            return "Me"
        if message.sender not in cache:
            cache[message.sender] = whatsapp_get_sender_name(message.sender)
        return cache[message.sender]
    return name


def _chat_text(chat) -> str:
    if chat is None:
        return "No chat found."
    last_sender = None
    if chat.last_message:
        last_sender = "Me" if chat.last_is_from_me else whatsapp_get_sender_name(chat.last_sender or "")
    return fmt.chat_line(chat, last_sender)


def _chats_text(chats, limit: int, hint: str) -> str:
    if not chats:
        return "No chats found."
    return "\n".join([_chat_text(c) for c in chats] + fmt.more_hint(len(chats), limit, hint))


@mcp.tool()
def search_contacts(query: str, limit: int = fmt.DEFAULT_LIMIT) -> str:
    """Search WhatsApp contacts by name or phone number.
    One line per contact: `name · phone · jid`.

    Args:
        query: Search term to match against contact names or phone numbers
        limit: Maximum contacts to return (default 20, max 100)
    """
    limit = fmt.clamp_limit(limit)
    contacts = whatsapp_search_contacts(query, limit)
    if not contacts:
        return "No contacts found."
    return "\n".join(f"{c.name or '-'} · {c.phone_number} · {c.jid}" for c in contacts)

@mcp.tool()
def list_messages(
    after: Optional[str] = None,
    before: Optional[str] = None,
    sender_phone_number: Optional[str] = None,
    chat_jid: Optional[str] = None,
    query: Optional[str] = None,
    limit: int = fmt.DEFAULT_LIMIT,
    include_context: bool = False,
    context_before: int = 1,
    context_after: int = 1,
    show_ids: bool = False
) -> str:
    """Get WhatsApp messages, newest page first, shown oldest-to-newest within the page.
    One line per message: `time · sender · text`, grouped under a `## chat (jid)` header.
    Bodies over 1,000 chars are truncated. Media shows as `[type id=...]`; fetch it with download_media.

    To page further back, pass the `before` value from the "(more available: ...)" line.

    Args:
        after: Optional ISO-8601 datetime; only messages after this
        before: Optional ISO-8601 datetime; only messages before this (also the paging cursor)
        sender_phone_number: Optional sender JID/phone to filter by
        chat_jid: Optional chat JID to filter by
        query: Optional text to search for in message bodies
        limit: Maximum matching messages (default 20, max 100)
        include_context: Also show messages around each match, marking matches with `>` (default False)
        context_before: Messages before each match when include_context is set (default 1, max 20)
        context_after: Messages after each match when include_context is set (default 1, max 20)
        show_ids: Append message ids, needed for get_message_context (default False)
    """
    limit = fmt.clamp_limit(limit)
    matches = whatsapp_list_messages(
        after=after,
        before=before,
        sender_phone_number=sender_phone_number,
        chat_jid=chat_jid,
        query=query,
        limit=limit,
    )
    if not matches:
        return "No messages found."

    shown = list(reversed(matches))
    highlight = None
    if include_context:
        seen, expanded = set(), []
        for m in shown:
            ctx = whatsapp_get_message_context(m.id, fmt.clamp_context(context_before), fmt.clamp_context(context_after))
            for c in list(reversed(ctx.before)) + [ctx.message] + ctx.after:
                if (c.id, c.chat_jid) not in seen:
                    seen.add((c.id, c.chat_jid))
                    expanded.append(c)
        shown, highlight = expanded, {m.id for m in matches}

    lines = fmt.messages_block(shown, _name_lookup(), show_ids, highlight)
    oldest = matches[-1].timestamp.isoformat()
    lines += fmt.more_hint(len(matches), limit, f'before="{oldest}"')
    return "\n".join(lines)

@mcp.tool()
def list_chats(
    query: Optional[str] = None,
    limit: int = fmt.DEFAULT_LIMIT,
    page: int = 0,
    include_last_message: bool = True,
    sort_by: str = "last_active"
) -> str:
    """Get WhatsApp chats. One line per chat:
    `name · jid · group|direct · last time · sender: preview` (preview capped at 100 chars).

    Args:
        query: Optional search term to filter chats by name or JID
        limit: Maximum chats to return (default 20, max 100)
        page: Page number for pagination (default 0)
        include_last_message: Whether to include a preview of the last message (default True)
        sort_by: "last_active" or "name" (default "last_active")
    """
    limit = fmt.clamp_limit(limit)
    chats = whatsapp_list_chats(
        query=query,
        limit=limit,
        page=max(page, 0),
        include_last_message=include_last_message,
        sort_by=sort_by
    )
    return _chats_text(chats, limit, f"page={max(page, 0) + 1}")

@mcp.tool()
def recent_activity(since: str, limit: int = fmt.DEFAULT_LIMIT) -> str:
    """Which chats had messages since a time, busiest first. Cheap first step before
    reading or summarising: one line per chat, `name · jid · N msgs · last time`.

    Args:
        since: ISO-8601 datetime, e.g. "2026-10-29T00:00:00"
        limit: Maximum chats to return (default 20, max 100)
    """
    try:
        after = datetime.fromisoformat(since)
    except ValueError:
        return f"Invalid 'since': {since}. Use ISO-8601, e.g. 2026-10-29T09:00:00."
    rows = whatsapp_recent_activity(after, fmt.clamp_limit(limit))
    if not rows:
        return "No messages in that period."
    return "\n".join(
        f"{name or '-'} · {jid} · {count} msgs · last {fmt.fmt_time(datetime.fromisoformat(last)) if last else '-'}"
        for jid, name, count, last in rows
    )

@mcp.tool()
def get_chat(chat_jid: str, include_last_message: bool = True) -> str:
    """Get one WhatsApp chat by JID, as a single line.

    Args:
        chat_jid: The JID of the chat to retrieve
        include_last_message: Whether to include a preview of the last message (default True)
    """
    return _chat_text(whatsapp_get_chat(chat_jid, include_last_message))

@mcp.tool()
def get_direct_chat_by_contact(sender_phone_number: str) -> str:
    """Find the direct chat with a phone number, as a single line.

    Args:
        sender_phone_number: The phone number to search for
    """
    return _chat_text(whatsapp_get_direct_chat_by_contact(sender_phone_number))

@mcp.tool()
def get_contact_chats(jid: str, limit: int = fmt.DEFAULT_LIMIT, page: int = 0) -> str:
    """Get chats involving a contact (their direct chat and groups they posted in). One line per chat.

    Args:
        jid: The contact's JID to search for
        limit: Maximum chats to return (default 20, max 100)
        page: Page number for pagination (default 0)
    """
    limit = fmt.clamp_limit(limit)
    chats = whatsapp_get_contact_chats(jid, limit, max(page, 0))
    return _chats_text(chats, limit, f"page={max(page, 0) + 1}")

@mcp.tool()
def get_last_interaction(jid: str) -> str:
    """Get the most recent WhatsApp message involving a contact, as one line.

    Args:
        jid: The JID of the contact to search for
    """
    message = whatsapp_get_last_interaction(jid)
    if message is None:
        return "No messages found."
    return "\n".join(fmt.messages_block([message], _name_lookup()))

@mcp.tool()
def get_message_context(
    message_id: str,
    before: int = 5,
    after: int = 5
) -> str:
    """Get messages around one message (found via list_messages with show_ids). The target is marked `>`.

    Args:
        message_id: The ID of the message to get context for
        before: Messages before the target (default 5, max 20)
        after: Messages after the target (default 5, max 20)
    """
    try:
        ctx = whatsapp_get_message_context(message_id, fmt.clamp_context(before), fmt.clamp_context(after))
    except ValueError as e:
        return str(e)
    messages = list(reversed(ctx.before)) + [ctx.message] + ctx.after
    return "\n".join(fmt.messages_block(messages, _name_lookup(), highlight_ids={ctx.message.id}))

if WRITE_ENABLED:
    @mcp.tool()
    def send_message(
        recipient: str,
        message: str
    ) -> Dict[str, Any]:
        """Send a WhatsApp message to a person or group. For group chats use the JID.

        WARNING: This sends from the user's real WhatsApp account. The recipient sees it as coming from them and it cannot be unsent.

        Args:
            recipient: The recipient - either a phone number with country code but no + or other symbols,
                     or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
            message: The message text to send
    
        Returns:
            A dictionary containing success status and a status message
        """
        # Validate input
        if not recipient:
            return {
                "success": False,
                "message": "Recipient must be provided"
            }
    
        # Call the whatsapp_send_message function with the unified recipient parameter
        success, status_message = whatsapp_send_message(recipient, message)
        return {
            "success": success,
            "message": status_message
        }

    @mcp.tool()
    def send_file(recipient: str, media_path: str) -> Dict[str, Any]:
        """Send a file such as a picture, raw audio, video or document via WhatsApp to the specified recipient. For group messages use the JID.

        WARNING: This sends from the user's real WhatsApp account. The recipient sees it as coming from them and it cannot be unsent.
    
        Args:
            recipient: The recipient - either a phone number with country code but no + or other symbols,
                     or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
            media_path: The absolute path to the media file to send (image, video, document)
    
        Returns:
            A dictionary containing success status and a status message
        """
    
        # Call the whatsapp_send_file function
        success, status_message = whatsapp_send_file(recipient, media_path)
        return {
            "success": success,
            "message": status_message
        }

    @mcp.tool()
    def send_audio_message(recipient: str, media_path: str) -> Dict[str, Any]:
        """Send any audio file as a WhatsApp audio message to the specified recipient. For group messages use the JID. If it errors due to ffmpeg not being installed, use send_file instead.

        WARNING: This sends from the user's real WhatsApp account. The recipient sees it as coming from them and it cannot be unsent.
    
        Args:
            recipient: The recipient - either a phone number with country code but no + or other symbols,
                     or a JID (e.g., "123456789@s.whatsapp.net" or a group JID like "123456789@g.us")
            media_path: The absolute path to the audio file to send (will be converted to Opus .ogg if it's not a .ogg file)
    
        Returns:
            A dictionary containing success status and a status message
        """
        success, status_message = whatsapp_audio_voice_message(recipient, media_path)
        return {
            "success": success,
            "message": status_message
        }

@mcp.tool()
def download_media(message_id: str, chat_jid: str) -> Dict[str, Any]:
    """Download media from a WhatsApp message and get the local file path.
    
    Args:
        message_id: The ID of the message containing the media
        chat_jid: The JID of the chat containing the message
    
    Returns:
        A dictionary containing success status, a status message, and the file path if successful
    """
    file_path = whatsapp_download_media(message_id, chat_jid)
    
    if file_path:
        return {
            "success": True,
            "message": "Media downloaded successfully",
            "file_path": file_path
        }
    else:
        return {
            "success": False,
            "message": "Failed to download media"
        }

@mcp.tool()
def get_status() -> Dict[str, Any]:
    """Check the WhatsApp connection: whether the bridge is running and connected,
    the paired phone number, and whether sending is enabled and limited by an allowlist.
    """
    status = whatsapp_get_status()
    status["write_tools_registered"] = WRITE_ENABLED
    return status

if __name__ == "__main__":
    # Initialize and run the server
    mcp.run(transport='stdio')