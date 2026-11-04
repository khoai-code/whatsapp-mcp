"""Compact text rendering for tool output.

Every tool result lands in the user's Claude context, so output is one line per
item with no repeated keys. Long bodies are truncated and media is referenced,
never inlined.
"""
from datetime import datetime
from typing import Callable, Iterable, List, Optional

DEFAULT_LIMIT = 20
MAX_LIMIT = 100
MAX_BODY_CHARS = 1000
PREVIEW_CHARS = 100
MAX_CONTEXT = 20


def clamp_limit(limit: Optional[int], default: int = DEFAULT_LIMIT, cap: int = MAX_LIMIT) -> int:
    if limit is None or limit < 1:
        return default
    return min(limit, cap)


def clamp_context(n: Optional[int]) -> int:
    if n is None or n < 0:
        return 0
    return min(n, MAX_CONTEXT)


def truncate(text: Optional[str], max_chars: int = MAX_BODY_CHARS) -> str:
    if not text:
        return ""
    if len(text) <= max_chars:
        return text
    return f"{text[:max_chars]}…[truncated, {len(text)} chars]"


def one_line(text: Optional[str]) -> str:
    """Keep each message on one output line."""
    return (text or "").replace("\r\n", "\n").replace("\n", " ⏎ ")


def fmt_time(ts: Optional[datetime]) -> str:
    return f"{ts:%Y-%m-%d %H:%M}" if ts else "-"


def message_line(message, sender_name: str, show_id: bool = False, marker: str = "") -> str:
    """`time · sender · text`, with a media reference instead of media bytes."""
    body = one_line(truncate(message.content))
    if message.media_type:
        ref = f"[{message.media_type} id={message.id}]"
        body = f"{ref} {body}".rstrip()
    elif show_id:
        body = f"{body} (id={message.id})"
    return f"{marker}{fmt_time(message.timestamp)} · {sender_name} · {body}"


def messages_block(
    messages: Iterable,
    sender_name: Callable[[object], str],
    show_ids: bool = False,
    highlight_ids: Optional[set] = None,
) -> List[str]:
    """Render messages in order, adding a header line whenever the chat changes."""
    lines: List[str] = []
    current_chat = None
    for m in messages:
        if m.chat_jid != current_chat:
            current_chat = m.chat_jid
            lines.append(f"## {m.chat_name or m.chat_jid} ({m.chat_jid})")
        marker = "> " if highlight_ids and m.id in highlight_ids else ""
        lines.append(message_line(m, sender_name(m), show_ids, marker))
    return lines


def chat_line(chat, last_sender_name: Optional[str] = None) -> str:
    kind = "group" if chat.jid.endswith("@g.us") else "direct"
    line = f"{chat.name or '-'} · {chat.jid} · {kind} · last {fmt_time(chat.last_message_time)}"
    if chat.last_message:
        who = last_sender_name or "-"
        line += f" · {who}: {one_line(truncate(chat.last_message, PREVIEW_CHARS))}"
    return line


def more_hint(count: int, limit: int, hint: str) -> List[str]:
    if count < limit:
        return []
    return [f"(more available: {hint})"]
