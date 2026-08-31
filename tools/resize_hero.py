"""Resize docs/hero.png to a 1200px progressive JPEG for the README."""
from PIL import Image
import os

SRC = os.path.expanduser("~/projects/idea-lab/docs/hero.png")
DST = os.path.expanduser("~/projects/idea-lab/docs/hero.jpg")

img = Image.open(SRC).convert("RGB")
w = 1200
h = round(img.height * w / img.width)
img.resize((w, h), Image.LANCZOS).save(
    DST, quality=82, optimize=True, progressive=True
)
print("optimised:", os.path.getsize(DST), "bytes,", (w, h))