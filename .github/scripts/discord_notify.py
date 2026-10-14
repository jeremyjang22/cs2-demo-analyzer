"""Post what just happened on GitHub to a Discord channel.

Run by .github/workflows/discord.yml. Reads the triggering event the way every
Actions step can - GITHUB_EVENT_NAME and the JSON at GITHUB_EVENT_PATH - and
sends one embed to the webhook in DISCORD_WEBHOOK_URL:

  push          the commits that just landed on main, with links
  workflow_run  whether a deploy succeeded or failed, and for which commit

Standard library only, so the step needs no install. DRY_RUN=1 prints the
payload instead of sending it, which is how to check a change locally:

  GITHUB_EVENT_NAME=push GITHUB_EVENT_PATH=event.json DRY_RUN=1 python discord_notify.py
"""

import json
import os
import sys
import urllib.request

# Discord's own limits; past them the whole message is rejected, not trimmed.
DESCRIPTION_LIMIT = 4096
TITLE_LIMIT = 256
MAX_COMMITS = 10

BLUE, GREEN, RED, GREY = 0x3987E5, 0x4CC94C, 0xE5534B, 0x898781


def first_line(message: str) -> str:
    return (message or "").strip().splitlines()[0] if (message or "").strip() else "(no message)"


def clip(text: str, limit: int) -> str:
    return text if len(text) <= limit else text[: limit - 1] + "…"


def push_embed(event: dict) -> dict | None:
    commits = event.get("commits") or []
    if not commits:
        # A branch deleted, or a tag: nothing that reads as "what was done".
        return None
    repo = event["repository"]["full_name"]
    branch = event.get("ref", "").removeprefix("refs/heads/")
    lines = []
    for c in commits[:MAX_COMMITS]:
        author = (c.get("author") or {}).get("name", "someone")
        lines.append(f"[`{c['id'][:7]}`]({c['url']}) {first_line(c['message'])} — {author}")
    if len(commits) > MAX_COMMITS:
        lines.append(f"…and {len(commits) - MAX_COMMITS} more")
    n = len(commits)
    return {
        "title": clip(f"{n} commit{'s' if n != 1 else ''} pushed to {branch} · {repo}", TITLE_LIMIT),
        "url": event.get("compare"),
        "description": clip("\n".join(lines), DESCRIPTION_LIMIT),
        "color": BLUE,
    }


def run_embed(event: dict) -> dict | None:
    run = event["workflow_run"]
    conclusion = run.get("conclusion")
    if conclusion in (None, "skipped", "neutral"):
        return None
    outcome = {"success": ("succeeded", GREEN), "failure": ("failed", RED)}.get(conclusion, (conclusion, GREY))
    head = run.get("head_commit") or {}
    sha = (run.get("head_sha") or "")[:7]
    return {
        "title": clip(f"{run['name']} {outcome[0]} · {event['repository']['full_name']}", TITLE_LIMIT),
        "url": run.get("html_url"),
        "description": clip(f"`{sha}` {first_line(head.get('message', ''))}", DESCRIPTION_LIMIT),
        "color": outcome[1],
    }


def build(event_name: str, event: dict) -> dict | None:
    embed = {"push": push_embed, "workflow_run": run_embed}.get(event_name, lambda _: None)(event)
    if embed is None:
        return None
    # A commit message containing @everyone must not ping the whole server.
    return {"embeds": [embed], "allowed_mentions": {"parse": []}}


def main() -> int:
    event_name = os.environ.get("GITHUB_EVENT_NAME", "")
    with open(os.environ["GITHUB_EVENT_PATH"], encoding="utf-8") as f:
        payload = build(event_name, json.load(f))
    if payload is None:
        print(f"nothing to post for a {event_name or 'unknown'} event")
        return 0
    if os.environ.get("DRY_RUN"):
        print(json.dumps(payload, indent=2))
        return 0

    url = os.environ.get("DISCORD_WEBHOOK_URL", "")
    if not url:
        print("DISCORD_WEBHOOK_URL is not set; nothing sent")
        return 0
    req = urllib.request.Request(
        url,
        data=json.dumps(payload).encode(),
        # Discord sits behind Cloudflare, which turns away Python's default
        # user agent with a 403 that looks like a bad webhook.
        headers={"Content-Type": "application/json", "User-Agent": "cs2-demo-analyzer-ci (github actions)"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=15) as res:
        print(f"posted to Discord: HTTP {res.status}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
