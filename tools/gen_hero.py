#!/usr/bin/env python3
"""Regenerate the README hero with a black cat (same scene, same style)."""
import base64
import json
import os
import subprocess

REPO = os.path.expanduser("~/projects/idea-lab")
KEY_FILE = os.path.join(REPO, ".env")
OUT = os.path.join(REPO, "docs", "hero.png")

PROMPT = (
    "Clean modern editorial illustration, soft pastel palette, gentle texture, "
    "uncluttered composition. Scene: a calm home-office wall in warm afternoon "
    "light, a grid of pinned paper idea cards with tiny sketches on them, one "
    "card showing a wine bottle, a small potted olive tree on the desk below, "
    "a phone leaning against a mug, a large black cat asleep in the corner, "
    "glossy black fur with a hint of brown in the light. Cool, relaxed, "
    "unhurried mood. No text or letters anywhere."
)

key = ""
with open(KEY_FILE) as f:
    for line in f:
        if line.startswith("OPENAI_API_KEY="):
            key = line.split("=", 1)[1].strip()
            break

import urllib.request

body = json.dumps({
    "model": "gpt-image-1",
    "prompt": PROMPT,
    "size": "1536x1024",
    "n": 1,
})

r = urllib.request.Request(
    "https://api.openai.com/v1/images/generations",
    data=body.encode(),
    headers={
        "Authorization": f"Bearer {key}",
        "Content-Type": "application/json",
    },
)
with urllib.request.urlopen(r, timeout=180) as resp:
    d = json.load(resp)

raw = base64.b64decode(d["data"][0]["b64_json"])
tmp = os.path.join(REPO, "docs", "hero-black.png")
with open(tmp, "wb") as f:
    f.write(raw)
print("generated:", len(raw), "bytes")