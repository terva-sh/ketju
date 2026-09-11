#!/usr/bin/env python3
"""Generate the four Neoforum mockup artboards (front page and thread page,
light and dark) from one markup source so the themes never drift.

Run from this directory:  python3 generate.py
Outputs desktop artboards Main.dc.html (front, light), FrontDark, ThreadLight,
ThreadDark, and mobile artboards MobileFrontLight/Dark, MobileThreadLight/Dark. Tokens mirror the design overview page and docs/06.
"""
from string import Template
from pathlib import Path

LIGHT = dict(
    bg="#F6F7FA", surface="#FFFFFF", surface2="#EEF1F6", ink="#1B2130",
    ink2="#4A5568", muted="#65707F", rule="#D9DEE7", rule2="#C4CBD8",
    accent="#2F5FB3", accent_ink="#FFFFFF", accent_soft="#E4ECFA",
    unread="#96600F", unread_soft="#FBF1E0", code_bg="#EEF1F6",
    shadow="0 1px 2px rgba(27,33,48,.06)", thumb="#DDE3EC",
)
DARK = dict(
    bg="#12161E", surface="#1A1F2A", surface2="#222835", ink="#E6E9EF",
    ink2="#B8C0CE", muted="#8B95A8", rule="#2B3242", rule2="#3A4356",
    accent="#7FA3E8", accent_ink="#0F1622", accent_soft="#1E2A42",
    unread="#E0A650", unread_soft="#2A2416", code_bg="#1E2430",
    shadow="0 1px 2px rgba(0,0,0,.35)", thumb="#2A3140",
)

FONTS = '<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Newsreader:opsz,wght@6..72,400;6..72,500;6..72,600&amp;family=IBM+Plex+Sans:wght@400;500;600&amp;family=IBM+Plex+Mono:wght@400;500&amp;display=swap">'

# --- icons (16px, stroke-based) ------------------------------------------
def icon(name, size=16, color="currentColor"):
    paths = {
        "search": '<circle cx="7" cy="7" r="4.5"/><path d="M10.5 10.5 14 14"/>',
        "inbox": '<path d="M2 9h3.5l1 2h3l1-2H14"/><path d="M3.5 3h9l1.5 6v4H2V9z"/>',
        "chevron": '<path d="M4 6l4 4 4-4"/>',
        "reply": '<path d="M6 4 2 8l4 4"/><path d="M2 8h7a5 5 0 0 1 5 5v1"/>',
        "clip": '<path d="M10.5 5.5 6 10a1.8 1.8 0 0 0 2.5 2.5l5-5a3.2 3.2 0 0 0-4.5-4.5l-5 5a4.6 4.6 0 0 0 6.5 6.5l4-4"/>',
        "lock": '<rect x="3.5" y="7" width="9" height="7" rx="1"/><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2"/>',
        "bold": '<path d="M5 3h4a2.5 2.5 0 0 1 0 5H5zM5 8h4.5a2.5 2.5 0 0 1 0 5H5z"/>',
        "italic": '<path d="M7 3h6M3 13h6M10 3 6 13"/>',
        "quote": '<path d="M4 4h3v4H5v3H3V6zM10 4h3v4h-2v3H9V6z"/>',
        "code": '<path d="m5 5-3 3 3 3M11 5l3 3-3 3M9.5 3l-3 10"/>',
        "link": '<path d="M6.5 9.5 9.5 6.5M7 4.5l1-1a2.5 2.5 0 0 1 3.5 3.5l-1 1M9 11.5l-1 1a2.5 2.5 0 0 1-3.5-3.5l1-1"/>',
        "at": '<circle cx="8" cy="8" r="2.5"/><path d="M10.5 8v1a1.5 1.5 0 0 0 3 0V8a5.5 5.5 0 1 0-2.2 4.4"/>',
        "pin": '<path d="M6 2h4l-.5 4 2.5 2.5V10H4V8.5L6.5 6zM8 10v4"/>',
        "eye": '<path d="M1.5 8s2.5-4.5 6.5-4.5S14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z"/><circle cx="8" cy="8" r="2"/>',
        "home": '<path d="M2.5 7.5 8 3l5.5 4.5V13a.5.5 0 0 1-.5.5H3a.5.5 0 0 1-.5-.5z"/><path d="M6.5 13.5v-4h3v4"/>',
        "list": '<path d="M5 4h9M5 8h9M5 12h9"/><circle cx="2.5" cy="4" r=".75"/><circle cx="2.5" cy="8" r=".75"/><circle cx="2.5" cy="12" r=".75"/>',
        "clock": '<circle cx="8" cy="8" r="5.5"/><path d="M8 5v3.5l2.5 1.5"/>',
        "bell": '<path d="M4 11V7.5a4 4 0 0 1 8 0V11l1 1.5H3z"/><path d="M6.5 13.5a1.5 1.5 0 0 0 3 0"/>',
        "user": '<circle cx="8" cy="5.5" r="2.75"/><path d="M2.75 14a5.25 5.25 0 0 1 10.5 0"/>',
        "pen": '<path d="m3 13 .8-3.2L10.6 3l2.4 2.4-6.8 6.8z"/><path d="m9.5 4.2 2.3 2.3"/>',
        "more": '<circle cx="3.5" cy="8" r="1"/><circle cx="8" cy="8" r="1"/><circle cx="12.5" cy="8" r="1"/>',
        "back": '<path d="m10 3-5 5 5 5"/>',
        "menu": '<path d="M2.5 4.5h11M2.5 8h11M2.5 11.5h11"/>',
    }[name]
    return (f'<svg width="{size}" height="{size}" viewBox="0 0 16 16" fill="none" '
            f'stroke="{color}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" '
            f'style="flex-shrink: 0">{paths}</svg>')

# --- shared chrome ----------------------------------------------------------
def nav(t, active):
    def item(label, badge=None, is_active=False):
        color = t["ink"] if is_active else t["ink2"]
        border = t["accent"] if is_active else "transparent"
        b = (f'<span style="font-family: IBM Plex Mono, ui-monospace, monospace; font-size: 11px; '
             f'background: {t["unread"]}; color: {t["surface"]}; padding: 1px 6px; border-radius: 9px; '
             f'font-weight: 500">{badge}</span>' if badge else "")
        return (f'<a href="#" style="display: flex; align-items: center; gap: 6px; padding: 18px 2px 16px; '
                f'color: {color}; text-decoration: none; font-weight: 500; font-size: 14px; '
                f'border-bottom: 2px solid {border}">{label}{b}</a>')
    return f'''
<header style="height: 56px; background: {t["surface"]}; border-bottom: 1px solid {t["rule"]}; display: flex; align-items: stretch; padding: 0 40px; gap: 32px">
  <a href="#" style="display: flex; align-items: center; font-family: Newsreader, Georgia, serif; font-size: 22px; font-weight: 500; color: {t["ink"]}; text-decoration: none; letter-spacing: -0.01em">Neoforum</a>
  <nav style="display: flex; align-items: stretch; gap: 22px">
    {item("Forums", is_active=active=="forums")}
    {item("Unread", "12", active=="unread")}
    {item("New", "4")}
    {item("Participated")}
    {item("Watching")}
  </nav>
  <div style="flex-grow: 1"></div>
  <div style="display: flex; align-items: center; gap: 16px">
    <div style="display: flex; align-items: center; gap: 8px; width: 240px; height: 34px; padding: 0 12px; border: 1px solid {t["rule"]}; border-radius: 4px; background: {t["bg"]}; color: {t["muted"]}; font-size: 13px">{icon("search")}<span>Search threads and posts</span><span style="margin-left: auto; font-family: IBM Plex Mono, ui-monospace, monospace; font-size: 11px; border: 1px solid {t["rule2"]}; padding: 0 5px; border-radius: 3px">/</span></div>
    <a href="#" title="Moderator queue" style="display: flex; align-items: center; gap: 6px; height: 34px; padding: 0 10px; border-radius: 4px; color: {t["ink2"]}; text-decoration: none; font-size: 13px; font-weight: 500">{icon("inbox")}<span style="font-family: IBM Plex Mono, ui-monospace, monospace; font-size: 11px; background: {t["accent_soft"]}; color: {t["accent"]}; padding: 1px 6px; border-radius: 9px">2</span></a>
    <a href="#" style="display: flex; align-items: center; gap: 8px; color: {t["ink"]}; text-decoration: none; font-size: 14px; font-weight: 500"><span style="width: 28px; height: 28px; border-radius: 50%; background: {t["accent"]}; color: {t["accent_ink"]}; display: flex; align-items: center; justify-content: center; font-size: 13px; font-weight: 600">D</span>drew</a>
  </div>
</header>'''

def helmet(t):
    return f'''<helmet>
  {FONTS}
  <style>
    body {{ margin: 0; background: {t["bg"]}; color: {t["ink"]}; font-family: "IBM Plex Sans", system-ui, sans-serif; font-size: 15px; line-height: 1.55; -webkit-font-smoothing: antialiased; }}
    a {{ color: {t["accent"]}; }} a:hover {{ color: {t["accent"]}; text-decoration: underline; }}
    .tnum {{ font-variant-numeric: tabular-nums; }}
  </style>
</helmet>'''

def doc(t, body, height):
    return f'''<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <script src="./support.js"></script>
</head>
<body>
<x-dc>
{helmet(t)}
<div style="width: 1440px; min-height: {height}px; background: {t["bg"]}; color: {t["ink"]}; display: flex; flex-direction: column">
{body}
</div>
</x-dc>
</body>
</html>
'''

# --- front page -----------------------------------------------------------
def front(t):
    mono = "IBM Plex Mono, ui-monospace, monospace"
    def group(name, rows):
        return f'''
<section style="display: flex; flex-direction: column; gap: 0">
  <div style="font-family: {mono}; font-size: 11px; letter-spacing: 0.12em; text-transform: uppercase; color: {t["muted"]}; padding: 0 0 8px 4px">{name}</div>
  <div style="background: {t["surface"]}; border: 1px solid {t["rule"]}; border-radius: 4px; box-shadow: {t["shadow"]}; display: flex; flex-direction: column">
    {"".join(rows)}
  </div>
</section>'''
    def row(name, desc, threads, posts, unread, last_title, last_by, last_when, restricted=False, last=False):
        border = "" if last else f"border-bottom: 1px solid {t['rule']};"
        lock = icon("lock", 14, t["muted"]) if restricted else ""
        badge = (f'<a href="#" style="font-family: {mono}; font-size: 12px; background: {t["unread_soft"]}; color: {t["unread"]}; '
                 f'border: 1px solid {t["unread"]}; padding: 1px 8px; border-radius: 10px; text-decoration: none; font-weight: 500">{unread} new</a>'
                 if unread else f'<span style="font-size: 12px; color: {t["muted"]}">—</span>')
        return f'''
    <div style="display: grid; grid-template-columns: minmax(0, 1fr) 150px 110px 300px; gap: 24px; align-items: center; padding: 14px 20px; {border}">
      <div style="display: flex; flex-direction: column; gap: 2px; min-width: 0">
        <div style="display: flex; align-items: center; gap: 8px"><a href="#" style="font-size: 16px; font-weight: 600; color: {t["ink"]}; text-decoration: none">{name}</a>{lock}</div>
        <div style="font-size: 13px; color: {t["muted"]}">{desc}</div>
      </div>
      <div class="tnum" style="font-family: {mono}; font-size: 12px; color: {t["ink2"]}; display: flex; flex-direction: column; gap: 2px; align-items: flex-end"><span>{threads} threads</span><span style="color: {t["muted"]}">{posts} posts</span></div>
      <div style="display: flex; justify-content: center">{badge}</div>
      <div style="display: flex; flex-direction: column; gap: 2px; min-width: 0; font-size: 13px">
        <a href="#" style="color: {t["ink"]}; text-decoration: none; white-space: nowrap; overflow: hidden; text-overflow: ellipsis">{last_title}</a>
        <span style="color: {t["muted"]}">{last_by} · {last_when}</span>
      </div>
    </div>'''
    head = f'''
    <div style="display: grid; grid-template-columns: minmax(0, 1fr) 150px 110px 300px; gap: 24px; padding: 0 20px 6px; font-family: {mono}; font-size: 11px; letter-spacing: 0.08em; text-transform: uppercase; color: {t["muted"]}">
      <span>Forum</span><span style="text-align: right">Activity</span><span style="text-align: center">Unread</span><span>Last post</span>
    </div>'''
    body = nav(t, "forums") + f'''
<main style="width: 1100px; margin: 0 auto; padding: 36px 0 48px; display: flex; flex-direction: column; gap: 32px; flex-grow: 1">
  <div style="display: flex; align-items: flex-end; justify-content: space-between; gap: 24px">
    <div style="display: flex; flex-direction: column; gap: 4px">
      <h1 style="margin: 0; font-family: Newsreader, Georgia, serif; font-weight: 500; font-size: 32px; line-height: 1.1; letter-spacing: -0.01em; color: {t["ink"]}">The Thursday Group</h1>
      <p style="margin: 0; color: {t["ink2"]}; font-size: 15px">12 unread posts across 5 threads since Tuesday evening.</p>
    </div>
    <div style="display: flex; gap: 10px; align-items: center">
      <a href="#" style="display: flex; align-items: center; gap: 6px; height: 36px; padding: 0 14px; border: 1px solid {t["rule2"]}; border-radius: 4px; color: {t["ink2"]}; text-decoration: none; font-size: 14px; font-weight: 500; background: {t["surface"]}">Mark all read</a>
      <a href="#" style="display: flex; align-items: center; gap: 6px; height: 36px; padding: 0 14px; border-radius: 4px; background: {t["accent"]}; color: {t["accent_ink"]}; text-decoration: none; font-size: 14px; font-weight: 600">New thread</a>
    </div>
  </div>
  <div style="display: flex; flex-direction: column; gap: 28px">
    {head}
    {group("Community", [
        row("General", "Anything that does not fit elsewhere", "148", "3,912", 7, "Naming scheme for the monthly threads", "mara", "14 min ago"),
        row("Introductions", "Say hello when you join; one thread per person", "31", "402", 0, "Hi from the far side of the river", "priya", "yesterday"),
        row("Meetups and events", "Planning, RSVPs, and the photos afterwards", "27", "1,130", 3, "October bonfire: date poll closes Friday", "tomas", "2 h ago", last=True),
    ])}
    {group("Projects", [
        row("The garden build", "Raised beds, irrigation, and who is watering this week", "19", "688", 2, "Drip line pressure is dropping at the far bed", "ines", "5 h ago"),
        row("Board game nights", "What we are playing, what we should buy, house rules", "42", "1,504", 0, "Rules clarification: trading during the auction", "sam", "3 days ago"),
        row("Reading group", "One book a month, spoilers behind the cut", "14", "377", 0, "November pick: nominations open", "mara", "6 days ago", last=True),
    ])}
    {group("Meta", [
        row("Site feedback", "Bugs, requests, and how the forum itself should work", "22", "301", 0, "Keyboard shortcut for jump to unread", "drew", "1 week ago"),
        row("Moderators", "Visible to the moderators group", "9", "156", 0, "Two applications waiting in the queue", "mara", "2 h ago", restricted=True, last=True),
    ])}
  </div>
  <footer style="margin-top: auto; display: flex; gap: 20px; font-size: 12px; color: {t["muted"]}; padding-top: 8px">
    <span>34 members · 1,208 posts</span><span>Press <span style="font-family: {mono}; border: 1px solid {t["rule2"]}; padding: 0 5px; border-radius: 3px">?</span> for keyboard shortcuts</span><a href="#" style="color: {t["muted"]}; text-decoration: none">Guidelines</a><a href="#" style="color: {t["muted"]}; text-decoration: none">Markdown help</a>
  </footer>
</main>'''
    return doc(t, body, 1100)

# --- thread page ----------------------------------------------------------
def thread(t):
    mono = "IBM Plex Mono, ui-monospace, monospace"
    def avatar(letter, color):
        return (f'<span style="width: 36px; height: 36px; border-radius: 50%; background: {color}; color: {t["surface"]}; '
                f'display: flex; align-items: center; justify-content: center; font-size: 15px; font-weight: 600; flex-shrink: 0">{letter}</span>')
    def actions(extra=""):
        a = lambda s: f'<a href="#" style="color: {t["muted"]}; text-decoration: none; font-size: 12.5px">{s}</a>'
        return (f'<div style="display: flex; gap: 16px; align-items: center; padding-top: 8px">{extra}{a("quote")}{a("reply")}{a("link")}{a("flag")}</div>')
    def post(ord_, who, letter, color, when, body_html, reply_to=None, role=None, edited=False, expander=None, unread=False, last=False):
        border = "" if last else f"border-bottom: 1px solid {t['rule']};"
        rt = (f'<a href="#" style="margin-left: auto; display: flex; align-items: center; gap: 4px; color: {t["accent"]}; text-decoration: none; font-family: {mono}; font-size: 12px">{icon("reply", 14)}#{reply_to}</a>' if reply_to else '')
        badge = (f'<span style="font-family: {mono}; font-size: 10px; letter-spacing: 0.06em; text-transform: uppercase; border: 1px solid {t["rule2"]}; color: {t["muted"]}; padding: 0 5px; border-radius: 2px">{role}</span>' if role else '')
        ed = f'<span style="color: {t["muted"]}">· edited</span>' if edited else ''
        exp = (f'<a href="#" style="display: flex; align-items: center; gap: 4px; color: {t["accent"]}; text-decoration: none; font-size: 12.5px; font-weight: 500">{expander} {icon("chevron", 12)}</a>' if expander else '')
        marker = f'border-left: 3px solid {t["unread"]}; padding-left: 21px;' if unread else 'padding-left: 24px;'
        return f'''
    <article style="display: flex; gap: 16px; padding: 18px 24px 16px; {marker} {border}">
      {avatar(letter, color)}
      <div style="display: flex; flex-direction: column; gap: 4px; flex-grow: 1; min-width: 0">
        <div style="display: flex; align-items: baseline; gap: 10px; font-size: 13px; color: {t["ink2"]}">
          <a href="#" style="font-family: {mono}; color: {t["muted"]}; text-decoration: none; font-size: 12px">#{ord_}</a>
          <a href="#" style="color: {t["ink"]}; font-weight: 600; text-decoration: none; font-size: 14px">{who}</a>{badge}
          <span>{when}</span>{ed}{rt}
        </div>
        <div style="font-size: 15px; line-height: 1.6; color: {t["ink"]}; display: flex; flex-direction: column; gap: 8px">{body_html}</div>
        {actions(exp)}
      </div>
    </article>'''
    def quote(ord_, who, text):
        return (f'<blockquote style="margin: 0; padding: 8px 14px; border-left: 3px solid {t["accent"]}; background: {t["surface2"]}; color: {t["ink2"]}; font-size: 14px; display: flex; flex-direction: column; gap: 2px">'
                f'<a href="#" style="font-family: {mono}; font-size: 11.5px; color: {t["accent"]}; text-decoration: none">#{ord_} · {who}</a><span>{text}</span></blockquote>')
    tomb = f'''
    <div style="display: flex; gap: 16px; padding: 12px 24px; border-bottom: 1px solid {t["rule"]}; align-items: center; color: {t["muted"]}; font-size: 13px">
      <span style="width: 36px; height: 36px; border-radius: 50%; border: 1px dashed {t["rule2"]}; flex-shrink: 0"></span>
      <span style="font-family: {mono}; font-size: 12px">#63</span><span style="font-style: italic">Post removed by its author.</span>
    </div>'''
    divider = f'''
    <div style="display: flex; align-items: center; gap: 12px; padding: 6px 24px; background: {t["unread_soft"]}; color: {t["unread"]}; border-top: 1px solid {t["unread"]}; border-bottom: 1px solid {t["unread"]}; font-size: 12px; font-weight: 600; letter-spacing: 0.04em">
      <span>3 new posts below</span><span style="flex-grow: 1; border-top: 1px dashed {t["unread"]}"></span><span style="font-weight: 400">last read #63 · Tue 14:10</span>
    </div>'''
    attachment = f'''
    <a href="#" style="display: flex; align-items: center; gap: 10px; width: 320px; padding: 8px 10px; border: 1px solid {t["rule"]}; border-radius: 4px; background: {t["surface2"]}; text-decoration: none; color: {t["ink"]}">
      <span style="width: 48px; height: 36px; border-radius: 2px; background: {t["thumb"]}; flex-shrink: 0"></span>
      <span style="display: flex; flex-direction: column; min-width: 0; font-size: 13px"><span style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis">naming-proposal.png</span><span style="color: {t["muted"]}; font-size: 12px; font-family: {mono}">240 KB · 1200×640</span></span>
    </a>'''
    def pager():
        def p(n, cur=False):
            if cur:
                return f'<span style="min-width: 32px; height: 32px; padding: 0 8px; display: flex; align-items: center; justify-content: center; border-radius: 4px; background: {t["ink"]}; color: {t["bg"]}; font-weight: 600">{n}</span>'
            return f'<a href="#" style="min-width: 32px; height: 32px; padding: 0 8px; display: flex; align-items: center; justify-content: center; border-radius: 4px; color: {t["ink2"]}; text-decoration: none; border: 1px solid {t["rule"]}; background: {t["surface"]}">{n}</a>'
        return (f'<div class="tnum" style="display: flex; gap: 6px; align-items: center; font-family: {mono}; font-size: 13px">'
                f'{p("‹")}{p(1)}{p(2)}{p(3, True)}{p(4)}{p(5)}<span style="color: {t["muted"]}; padding: 0 4px">…</span>{p(9)}{p("›")}'
                f'<span style="color: {t["muted"]}; margin-left: 10px; font-size: 12px">posts 51–75 of 212</span></div>')
    def tool(label, kbd=None, primary=False):
        k = f'<span style="font-family: {mono}; font-size: 11px; border: 1px solid {t["rule2"]}; padding: 0 5px; border-radius: 3px; color: {t["muted"]}">{kbd}</span>' if kbd else ''
        return (f'<a href="#" style="display: flex; align-items: center; gap: 8px; height: 32px; padding: 0 12px; border: 1px solid {t["rule2"]}; '
                f'border-radius: 4px; background: {t["surface"]}; color: {t["ink2"]}; text-decoration: none; font-size: 13px; font-weight: 500">{label}{k}</a>')
    body = nav(t, "forums") + f'''
<main style="width: 900px; margin: 0 auto; padding: 28px 0 56px; display: flex; flex-direction: column; gap: 20px">
  <nav style="display: flex; gap: 8px; font-size: 13px; color: {t["muted"]}"><a href="#" style="color: {t["muted"]}; text-decoration: none">Forums</a><span>›</span><a href="#" style="color: {t["muted"]}; text-decoration: none">General</a></nav>
  <div style="display: flex; flex-direction: column; gap: 10px">
    <div style="display: flex; align-items: center; gap: 12px">
      <h1 style="margin: 0; font-family: Newsreader, Georgia, serif; font-weight: 500; font-size: 32px; line-height: 1.15; letter-spacing: -0.01em; color: {t["ink"]}; text-wrap: balance">Naming scheme for the monthly threads</h1>
      <a href="#" style="font-family: {mono}; font-size: 11.5px; padding: 2px 8px; border-radius: 10px; background: {t["accent_soft"]}; color: {t["accent"]}; text-decoration: none; align-self: center">meta</a>
    </div>
    <div style="display: flex; gap: 14px; align-items: center; font-size: 13px; color: {t["muted"]}">
      <span>started by <a href="#" style="color: {t["ink2"]}; text-decoration: none; font-weight: 500">mara</a> · 3 days ago</span><span>·</span><span class="tnum">212 posts</span><span>·</span><span style="display: flex; align-items: center; gap: 5px">{icon("pin", 14)}pinned until Oct 1</span><span>·</span><a href="#" style="display: flex; align-items: center; gap: 5px; color: {t["accent"]}; text-decoration: none">{icon("eye", 14)}watching {icon("chevron", 12)}</a>
    </div>
  </div>
  <div style="display: flex; justify-content: space-between; align-items: center; gap: 16px; padding-top: 4px">
    {pager()}
    <div style="display: flex; gap: 8px">{tool("Only mara")}{tool("Lens", None)}{tool("First unread", "u")}</div>
  </div>
  <section style="background: {t["surface"]}; border: 1px solid {t["rule"]}; border-radius: 4px; box-shadow: {t["shadow"]}; display: flex; flex-direction: column">
    {post(61, "mara", "M", "#5B7FBF" if t is LIGHT else "#6F93D6", "Tue 14:02", '<p style="margin: 0">Could we agree on a naming scheme for the monthly threads before next week? "September 2026 — general" and "Sept chat" are both open right now and people are posting in both.</p>', role="moderator")}
    {post(62, "drew", "D", t["accent"], "Tue 14:10", '<p style="margin: 0">Agreed. Month name, four-digit year, then the topic. I will merge the two open ones tonight and leave a pointer in the old one.</p>', expander="2 replies")}
    {tomb}
    {divider}
    {post(64, "ines", "I", "#8A6BB5" if t is LIGHT else "#A78BD6", "Tue 15:31", quote(37, "mara", "…the old board did this with sticky threads and it got noisy fast.") + '<p style="margin: 0">It got noisy because the stickies never expired. Pin with an end date and the problem goes away. The first post of each month gets pinned for a week, then drops back into the list.</p>', reply_to=37, unread=True)}
    {post(65, "tomas", "T", "#3E8E7E" if t is LIGHT else "#5FB3A1", "Tue 15:40", '<p style="margin: 0">Mocked up what the forum list would look like with that scheme. The month name first keeps them sorting properly when you scan.</p>' + attachment, unread=True)}
    {post(66, "mara", "M", "#5B7FBF" if t is LIGHT else "#6F93D6", "Tue 15:44", '<p style="margin: 0">Works for me. Pinned until October 1 and I have merged the two September threads. Anyone who wants the old titles back, say so here.</p>', role="moderator", edited=True, unread=True, last=True)}
  </section>
  <div style="display: flex; justify-content: space-between; align-items: center">{pager()}<span style="font-size: 12px; color: {t["muted"]}">Press <span style="font-family: {mono}; border: 1px solid {t["rule2"]}; padding: 0 5px; border-radius: 3px">j</span> <span style="font-family: {mono}; border: 1px solid {t["rule2"]}; padding: 0 5px; border-radius: 3px">k</span> to move between posts</span></div>

  <section style="margin-top: 12px; background: {t["surface"]}; border: 1px solid {t["rule"]}; border-radius: 4px; box-shadow: {t["shadow"]}; display: flex; flex-direction: column">
    <div style="display: flex; align-items: center; gap: 12px; padding: 12px 20px; border-bottom: 1px solid {t["rule"]}">
      <span style="font-weight: 600; font-size: 14px">Reply</span>
      <span style="display: flex; align-items: center; gap: 6px; font-family: {mono}; font-size: 12px; padding: 2px 8px; border-radius: 10px; background: {t["accent_soft"]}; color: {t["accent"]}">{icon("reply", 12)}#62 drew <span style="color: {t["muted"]}; padding-left: 4px">×</span></span>
      <div style="flex-grow: 1"></div>
      <div style="display: flex; gap: 2px; font-size: 13px">
        <span style="padding: 4px 10px; border-radius: 4px; background: {t["surface2"]}; color: {t["ink"]}; font-weight: 500">Write</span>
        <a href="#" style="padding: 4px 10px; color: {t["muted"]}; text-decoration: none">Preview</a>
      </div>
    </div>
    <div style="display: flex; gap: 4px; padding: 8px 16px; border-bottom: 1px solid {t["rule"]}; color: {t["ink2"]}">
      {"".join(f'<span style="width: 30px; height: 30px; display: flex; align-items: center; justify-content: center; border-radius: 4px">{icon(n)}</span>' for n in ["bold", "italic", "quote", "code", "link", "at", "clip"])}
    </div>
    <div style="padding: 16px 20px; min-height: 120px; font-size: 15px; line-height: 1.6; color: {t["ink"]}; white-space: pre-wrap">One thing to add before we lock this in: the meetups forum already uses "Oct 2026 bonfire" style titles. Should the scheme apply there too, or only to the general monthly threads?</div>
    <div style="display: flex; align-items: center; gap: 16px; padding: 12px 20px; border-top: 1px solid {t["rule"]}; font-size: 12.5px; color: {t["muted"]}">
      <span>Draft saved 12 seconds ago</span><span>Markdown supported</span>
      <div style="flex-grow: 1"></div>
      <a href="#" style="height: 36px; padding: 0 12px; display: flex; align-items: center; color: {t["ink2"]}; text-decoration: none; font-size: 14px; font-weight: 500">Discard</a>
      <a href="#" style="height: 36px; padding: 0 16px; display: flex; align-items: center; border-radius: 4px; background: {t["accent"]}; color: {t["accent_ink"]}; text-decoration: none; font-size: 14px; font-weight: 600">Post reply</a>
    </div>
  </section>
</main>'''
    return doc(t, body, 1650)


# --- mobile -----------------------------------------------------------------
MONO = "IBM Plex Mono, ui-monospace, monospace"

def mdoc(t, body, height):
    return f"""<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <script src="./support.js"></script>
</head>
<body>
<x-dc>
{helmet(t)}
<div style="width: 390px; min-height: {height}px; background: {t["bg"]}; color: {t["ink"]}; display: flex; flex-direction: column; position: relative">
{body}
</div>
</x-dc>
</body>
</html>
"""

def mtopbar(t, left_html, title_html, right_html):
    return f"""
<header style="height: 52px; background: {t["surface"]}; border-bottom: 1px solid {t["rule"]}; display: flex; align-items: center; padding: 0 8px; gap: 4px; position: sticky; top: 0">
  {left_html}
  <div style="flex-grow: 1; min-width: 0; display: flex; align-items: center; justify-content: center">{title_html}</div>
  {right_html}
</header>"""

def mbtn(t, name, badge=None, label=None):
    b = (f'<span style="position: absolute; top: 6px; right: 6px; font-family: {MONO}; font-size: 10px; background: {t["unread"]}; color: {t["surface"]}; padding: 0 5px; border-radius: 8px; font-weight: 500">{badge}</span>' if badge else "")
    return (f'<a href="#" aria-label="{label or name}" style="position: relative; width: 44px; height: 44px; display: flex; align-items: center; justify-content: center; border-radius: 6px; color: {t["ink2"]}; text-decoration: none">{icon(name, 20)}{b}</a>')

def mtabbar(t, active):
    def tab(name, label, badge=None):
        on = active == label
        color = t["accent"] if on else t["muted"]
        b = (f'<span style="position: absolute; top: 4px; left: 50%; margin-left: 4px; font-family: {MONO}; font-size: 10px; background: {t["unread"]}; color: {t["surface"]}; padding: 0 5px; border-radius: 8px; font-weight: 500">{badge}</span>' if badge else "")
        return (f'<a href="#" style="position: relative; flex: 1 1 0; height: 56px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px; color: {color}; text-decoration: none; font-size: 11px; font-weight: 500; letter-spacing: 0.01em">{icon(name, 22)}<span>{label}</span>{b}</a>')
    return f"""
<nav style="position: sticky; bottom: 0; margin-top: auto; height: 56px; background: {t["surface"]}; border-top: 1px solid {t["rule"]}; display: flex; align-items: stretch">
  {tab("home", "Forums")}{tab("list", "Unread", "12")}{tab("clock", "New", "4")}{tab("bell", "Alerts", "3")}{tab("user", "Me")}
</nav>"""

def mobile_front(t):
    def group(name, rows):
        return f"""
<section style="display: flex; flex-direction: column">
  <div style="font-family: {MONO}; font-size: 11px; letter-spacing: 0.12em; text-transform: uppercase; color: {t["muted"]}; padding: 0 16px 8px">{name}</div>
  <div style="background: {t["surface"]}; border-top: 1px solid {t["rule"]}; border-bottom: 1px solid {t["rule"]}; display: flex; flex-direction: column">{"".join(rows)}</div>
</section>"""
    def row(name, desc, unread, last_title, last_by, last_when, restricted=False, last=False):
        border = "" if last else f"border-bottom: 1px solid {t['rule']};"
        lock = icon("lock", 14, t["muted"]) if restricted else ""
        badge = (f'<span style="font-family: {MONO}; font-size: 12px; background: {t["unread_soft"]}; color: {t["unread"]}; border: 1px solid {t["unread"]}; padding: 1px 8px; border-radius: 10px; font-weight: 500; white-space: nowrap">{unread} new</span>'
                 if unread else "")
        return f"""
    <a href="#" style="display: flex; flex-direction: column; gap: 4px; padding: 12px 16px; min-height: 44px; text-decoration: none; color: {t["ink"]}; {border}">
      <div style="display: flex; align-items: center; gap: 8px"><span style="font-size: 16px; font-weight: 600; flex-grow: 1; min-width: 0">{name}</span>{lock}{badge}</div>
      <div style="font-size: 13px; color: {t["muted"]}">{desc}</div>
      <div style="font-size: 13px; color: {t["ink2"]}; display: flex; gap: 6px; min-width: 0"><span style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis">{last_title}</span><span style="color: {t["muted"]}; white-space: nowrap">· {last_when}</span></div>
    </a>"""
    top = mtopbar(t,
        f'<span style="font-family: Newsreader, Georgia, serif; font-size: 20px; font-weight: 500; color: {t["ink"]}; padding-left: 10px; letter-spacing: -0.01em">Neoforum</span>',
        '', mbtn(t, "search") + mbtn(t, "inbox", "2", "Moderator queue"))
    body = top + f"""
<main style="display: flex; flex-direction: column; gap: 20px; padding: 18px 0 24px">
  <div style="padding: 0 16px; display: flex; flex-direction: column; gap: 2px">
    <h1 style="margin: 0; font-family: Newsreader, Georgia, serif; font-weight: 500; font-size: 26px; line-height: 1.15; letter-spacing: -0.01em; color: {t["ink"]}">The Thursday Group</h1>
    <p style="margin: 0; color: {t["ink2"]}; font-size: 14px">12 unread posts across 5 threads since Tuesday evening.</p>
  </div>
  {group("Community", [
      row("General", "Anything that does not fit elsewhere", 7, "Naming scheme for the monthly threads", "mara", "14 min"),
      row("Introductions", "One thread per person", 0, "Hi from the far side of the river", "priya", "yesterday"),
      row("Meetups and events", "Planning, RSVPs, and the photos afterwards", 3, "October bonfire: date poll closes Friday", "tomas", "2 h", last=True),
  ])}
  {group("Projects", [
      row("The garden build", "Raised beds, irrigation, who is watering", 2, "Drip line pressure is dropping at the far bed", "ines", "5 h"),
      row("Board game nights", "What we are playing and house rules", 0, "Rules clarification: trading during the auction", "sam", "3 d"),
      row("Reading group", "One book a month, spoilers behind the cut", 0, "November pick: nominations open", "mara", "6 d", last=True),
  ])}
  {group("Meta", [
      row("Site feedback", "Bugs, requests, how the forum should work", 0, "Keyboard shortcut for jump to unread", "drew", "1 w"),
      row("Moderators", "Visible to the moderators group", 0, "Two applications waiting in the queue", "mara", "2 h", restricted=True, last=True),
  ])}
</main>
<a href="#" aria-label="New thread" style="position: absolute; right: 16px; bottom: 72px; width: 56px; height: 56px; border-radius: 50%; background: {t["accent"]}; color: {t["accent_ink"]}; display: flex; align-items: center; justify-content: center; box-shadow: 0 4px 12px rgba(0,0,0,.18)">{icon("pen", 22)}</a>
{mtabbar(t, "Forums")}"""
    return mdoc(t, body, 1040)

def mobile_thread(t):
    def avatar(letter, color, size=30):
        return (f'<span style="width: {size}px; height: {size}px; border-radius: 50%; background: {color}; color: {t["surface"]}; display: flex; align-items: center; justify-content: center; font-size: 13px; font-weight: 600; flex-shrink: 0">{letter}</span>')
    def post(ord_, who, letter, color, when, body_html, reply_to=None, role=None, edited=False, expander=None, unread=False, last=False):
        border = "" if last else f"border-bottom: 1px solid {t['rule']};"
        rt = (f'<a href="#" style="display: flex; align-items: center; gap: 3px; color: {t["accent"]}; text-decoration: none; font-family: {MONO}; font-size: 12px">{icon("reply", 13)}#{reply_to}</a>' if reply_to else '')
        badge = (f'<span style="font-family: {MONO}; font-size: 10px; letter-spacing: 0.06em; text-transform: uppercase; border: 1px solid {t["rule2"]}; color: {t["muted"]}; padding: 0 4px; border-radius: 2px">{role}</span>' if role else '')
        ed = f'<span style="color: {t["muted"]}">edited</span>' if edited else ''
        exp = (f'<a href="#" style="display: flex; align-items: center; gap: 4px; color: {t["accent"]}; text-decoration: none; font-size: 13px; font-weight: 500; min-height: 44px">{expander} {icon("chevron", 12)}</a>' if expander else '')
        marker = f'border-left: 3px solid {t["unread"]}; padding-left: 13px;' if unread else 'padding-left: 16px;'
        act = lambda s: f'<a href="#" style="color: {t["muted"]}; text-decoration: none; font-size: 13px; min-height: 44px; display: flex; align-items: center">{s}</a>'
        return f"""
    <article style="display: flex; flex-direction: column; gap: 6px; padding: 14px 16px 2px; {marker} {border}">
      <div style="display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: {t["ink2"]}">
        {avatar(letter, color)}
        <div style="display: flex; flex-direction: column; min-width: 0; flex-grow: 1">
          <div style="display: flex; align-items: center; gap: 6px"><a href="#" style="color: {t["ink"]}; font-weight: 600; text-decoration: none; font-size: 14px">{who}</a>{badge}</div>
          <div style="display: flex; gap: 6px; color: {t["muted"]}"><span style="font-family: {MONO}; font-size: 11.5px">#{ord_}</span><span>{when}</span>{ed}</div>
        </div>
        {rt}
      </div>
      <div style="font-size: 15px; line-height: 1.55; color: {t["ink"]}; display: flex; flex-direction: column; gap: 8px">{body_html}</div>
      <div style="display: flex; gap: 18px; align-items: center">{exp}{act("reply")}{act("quote")}<span style="flex-grow: 1"></span><a href="#" aria-label="More" style="width: 44px; height: 44px; display: flex; align-items: center; justify-content: center; color: {t["muted"]}">{icon("more", 16)}</a></div>
    </article>"""
    def quote(ord_, who, text):
        return (f'<blockquote style="margin: 0; padding: 6px 12px; border-left: 3px solid {t["accent"]}; background: {t["surface2"]}; color: {t["ink2"]}; font-size: 13.5px; display: flex; flex-direction: column; gap: 2px">'
                f'<a href="#" style="font-family: {MONO}; font-size: 11px; color: {t["accent"]}; text-decoration: none">#{ord_} · {who}</a><span>{text}</span></blockquote>')
    tomb = f"""
    <div style="display: flex; gap: 10px; padding: 10px 16px; border-bottom: 1px solid {t["rule"]}; align-items: center; color: {t["muted"]}; font-size: 13px">
      <span style="font-family: {MONO}; font-size: 11.5px">#63</span><span style="font-style: italic">Post removed by its author.</span>
    </div>"""
    divider = f"""
    <div style="display: flex; align-items: center; gap: 10px; padding: 6px 16px; background: {t["unread_soft"]}; color: {t["unread"]}; border-top: 1px solid {t["unread"]}; border-bottom: 1px solid {t["unread"]}; font-size: 12px; font-weight: 600; letter-spacing: 0.04em">
      <span>3 new posts below</span><span style="flex-grow: 1; border-top: 1px dashed {t["unread"]}"></span><span style="font-weight: 400">read to #63</span>
    </div>"""
    attachment = f"""
    <a href="#" style="display: flex; align-items: center; gap: 10px; padding: 8px 10px; border: 1px solid {t["rule"]}; border-radius: 4px; background: {t["surface2"]}; text-decoration: none; color: {t["ink"]}">
      <span style="width: 44px; height: 34px; border-radius: 2px; background: {t["thumb"]}; flex-shrink: 0"></span>
      <span style="display: flex; flex-direction: column; min-width: 0; font-size: 13px"><span style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis">naming-proposal.png</span><span style="color: {t["muted"]}; font-size: 12px; font-family: {MONO}">240 KB</span></span>
    </a>"""
    pager = f"""
  <div style="display: flex; align-items: center; justify-content: space-between; padding: 0 16px">
    <a href="#" aria-label="Previous page" style="width: 44px; height: 44px; display: flex; align-items: center; justify-content: center; border: 1px solid {t["rule"]}; border-radius: 4px; background: {t["surface"]}; color: {t["ink2"]}">{icon("back", 16)}</a>
    <a href="#" style="display: flex; align-items: center; gap: 6px; height: 44px; padding: 0 16px; border: 1px solid {t["rule"]}; border-radius: 4px; background: {t["surface"]}; color: {t["ink"]}; text-decoration: none; font-size: 14px; font-weight: 500"><span class="tnum">Page 3 of 9</span>{icon("chevron", 12, t["muted"])}</a>
    <a href="#" aria-label="Next page" style="width: 44px; height: 44px; display: flex; align-items: center; justify-content: center; border: 1px solid {t["rule"]}; border-radius: 4px; background: {t["surface"]}; color: {t["ink2"]}; transform: scaleX(-1)">{icon("back", 16)}</a>
  </div>"""
    top = mtopbar(t, mbtn(t, "back", label="Back to General"),
        f'<span style="font-size: 13px; color: {t["muted"]}; white-space: nowrap; overflow: hidden; text-overflow: ellipsis">General</span>',
        mbtn(t, "search") + mbtn(t, "more", label="Thread actions"))
    body = top + f"""
<main style="display: flex; flex-direction: column; gap: 16px; padding: 16px 0 24px">
  <div style="padding: 0 16px; display: flex; flex-direction: column; gap: 8px">
    <h1 style="margin: 0; font-family: Newsreader, Georgia, serif; font-weight: 500; font-size: 24px; line-height: 1.2; letter-spacing: -0.01em; color: {t["ink"]}; text-wrap: balance">Naming scheme for the monthly threads</h1>
    <div style="display: flex; flex-wrap: wrap; gap: 8px 12px; align-items: center; font-size: 12.5px; color: {t["muted"]}">
      <a href="#" style="font-family: {MONO}; font-size: 11px; padding: 1px 7px; border-radius: 10px; background: {t["accent_soft"]}; color: {t["accent"]}; text-decoration: none">meta</a>
      <span>mara · 3 days ago</span><span class="tnum">212 posts</span><span style="display: flex; align-items: center; gap: 4px">{icon("pin", 13)}until Oct 1</span>
    </div>
    <div style="display: flex; gap: 8px">
      <a href="#" style="display: flex; align-items: center; gap: 6px; height: 44px; padding: 0 14px; border: 1px solid {t["rule2"]}; border-radius: 4px; background: {t["surface"]}; color: {t["ink2"]}; text-decoration: none; font-size: 13px; font-weight: 500">Only mara</a>
      <a href="#" style="display: flex; align-items: center; gap: 6px; height: 44px; padding: 0 14px; border: 1px solid {t["rule2"]}; border-radius: 4px; background: {t["surface"]}; color: {t["ink2"]}; text-decoration: none; font-size: 13px; font-weight: 500">Lens {icon("chevron", 12)}</a>
      <a href="#" style="display: flex; align-items: center; gap: 6px; height: 44px; padding: 0 14px; border: 1px solid {t["rule2"]}; border-radius: 4px; background: {t["surface"]}; color: {t["accent"]}; text-decoration: none; font-size: 13px; font-weight: 500">{icon("eye", 14)}Watching</a>
    </div>
  </div>
  {pager}
  <section style="background: {t["surface"]}; border-top: 1px solid {t["rule"]}; border-bottom: 1px solid {t["rule"]}; display: flex; flex-direction: column">
    {post(61, "mara", "M", "#5B7FBF" if t is LIGHT else "#6F93D6", "Tue 14:02", '<p style="margin: 0">Could we agree on a naming scheme for the monthly threads before next week? "September 2026 — general" and "Sept chat" are both open right now and people are posting in both.</p>', role="moderator")}
    {post(62, "drew", "D", t["accent"], "Tue 14:10", '<p style="margin: 0">Agreed. Month name, four-digit year, then the topic. I will merge the two open ones tonight and leave a pointer in the old one.</p>', expander="2 replies")}
    {tomb}
    {divider}
    {post(64, "ines", "I", "#8A6BB5" if t is LIGHT else "#A78BD6", "Tue 15:31", quote(37, "mara", "…the old board did this with sticky threads and it got noisy fast.") + '<p style="margin: 0">It got noisy because the stickies never expired. Pin with an end date and the problem goes away.</p>', reply_to=37, unread=True)}
    {post(65, "tomas", "T", "#3E8E7E" if t is LIGHT else "#5FB3A1", "Tue 15:40", '<p style="margin: 0">Mocked up what the forum list would look like with that scheme.</p>' + attachment, unread=True)}
    {post(66, "mara", "M", "#5B7FBF" if t is LIGHT else "#6F93D6", "Tue 15:44", '<p style="margin: 0">Works for me. Pinned until October 1 and I have merged the two September threads.</p>', role="moderator", edited=True, unread=True, last=True)}
  </section>
  {pager}
</main>
<div style="position: sticky; bottom: 0; margin-top: auto; background: {t["surface"]}; border-top: 1px solid {t["rule"]}; padding: 10px 12px; display: flex; align-items: center; gap: 10px">
  <span style="display: flex; align-items: center; gap: 5px; font-family: {MONO}; font-size: 11.5px; padding: 3px 8px; border-radius: 10px; background: {t["accent_soft"]}; color: {t["accent"]}; white-space: nowrap">{icon("reply", 12)}#62</span>
  <a href="#" style="flex-grow: 1; height: 44px; display: flex; align-items: center; padding: 0 14px; border: 1px solid {t["rule2"]}; border-radius: 22px; background: {t["bg"]}; color: {t["muted"]}; text-decoration: none; font-size: 14px; min-width: 0"><span style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis">One thing to add before we lock this in…</span></a>
  <a href="#" aria-label="Post reply" style="width: 44px; height: 44px; border-radius: 50%; background: {t["accent"]}; color: {t["accent_ink"]}; display: flex; align-items: center; justify-content: center; flex-shrink: 0">{icon("pen", 18)}</a>
</div>"""
    return mdoc(t, body, 1640)

here = Path(__file__).parent
(here / "Main.dc.html").write_text(front(LIGHT))
(here / "FrontDark.dc.html").write_text(front(DARK))
(here / "ThreadLight.dc.html").write_text(thread(LIGHT))
(here / "ThreadDark.dc.html").write_text(thread(DARK))
(here / "MobileFrontLight.dc.html").write_text(mobile_front(LIGHT))
(here / "MobileFrontDark.dc.html").write_text(mobile_front(DARK))
(here / "MobileThreadLight.dc.html").write_text(mobile_thread(LIGHT))
(here / "MobileThreadDark.dc.html").write_text(mobile_thread(DARK))
print("wrote 8 artboards")
