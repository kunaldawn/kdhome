#!/usr/bin/env python3
"""
Generate the favicon set for KD's Homebrew Digital Archive.

Requirements: pip install Pillow
Font: DejaVu Sans Mono Bold (pre-installed on most Linux systems)

Usage: python3 generate_favicon.py
Output:
  static/favicon.ico                    (multi-res: 16, 32, 48)
  static/favicon-16x16.png
  static/favicon-32x32.png
  static/favicon-48x48.png
  static/apple-touch-icon.png           (180x180)
  static/android-chrome-192x192.png
  static/android-chrome-512x512.png
"""

from PIL import Image, ImageDraw, ImageFont
import os

# ─── Config ───
OUTDIR = os.path.join(os.path.dirname(__file__), "static")

# Colors (match site CSS variables — see generate_og.py)
BG     = (7, 16, 19)        # --bg: #071013
PANEL  = (7, 24, 35)        # --panel: #071823
NEON   = (0, 255, 153)      # --neon: #00ff99
MUTED  = (127, 209, 179)    # --muted: #7fd1b3

# Fonts
FONT_BOLD_PATH = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"


def bold(size):
    return ImageFont.truetype(FONT_BOLD_PATH, size)


def draw_mark(size):
    """Render the "KD" monogram on a rounded neon-bordered panel at `size`x`size`.

    Proportions (border thickness, padding, font scale) are derived from size
    so the mark reads cleanly from 16px up to 512px. Below 24px the border is
    dropped so the letters stay legible.
    """
    img = Image.new('RGBA', (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)

    # Panel geometry
    if size < 24:
        pad = 0
        border = 0
        radius = max(1, size // 5)
    else:
        pad = max(1, round(size * 0.04))
        border = max(1, round(size / 48))
        radius = max(2, round(size * 0.2))

    x0, y0 = pad, pad
    x1, y1 = size - 1 - pad, size - 1 - pad

    # Solid dark panel
    d.rounded_rectangle([x0, y0, x1, y1], radius=radius, fill=PANEL)
    # Neon border (skipped at very small sizes)
    if border:
        d.rounded_rectangle([x0, y0, x1, y1], radius=radius,
                            outline=NEON, width=border)

    # ─── "KD" glyph ───
    # Target text width ~62% of inner width so the two letters sit in the
    # middle without crowding the border.
    inner_w = (x1 - x0) - 2 * border
    inner_h = (y1 - y0) - 2 * border

    text = "KD"
    # Binary-search the largest font size that fits.
    lo, hi = 6, size * 2
    font = bold(lo)
    tw = th = 0
    bx = by = 0
    while lo <= hi:
        mid = (lo + hi) // 2
        f = bold(mid)
        bb = d.textbbox((0, 0), text, font=f)
        w = bb[2] - bb[0]
        h = bb[3] - bb[1]
        target_w = inner_w * (0.72 if size < 24 else 0.56)
        target_h = inner_h * (0.72 if size < 24 else 0.48)
        if w <= target_w and h <= target_h:
            font = f
            tw, th, bx, by = w, h, bb[0], bb[1]
            lo = mid + 1
        else:
            hi = mid - 1

    # Center using actual bbox offsets (textbbox can return non-zero origin).
    cx = (size - tw) // 2 - bx
    cy = (size - th) // 2 - by
    d.text((cx, cy), text, fill=NEON, font=font)

    # ─── Accent: blinking-cursor underscore for larger sizes ───
    # Adds a tiny terminal feel at sizes that can afford the detail.
    # `cy` is the pen origin passed to d.text; the actual glyph bottom
    # lands at `cy + by + th`, so anchor the cursor from there.
    if size >= 96:
        cur_w = max(2, size // 16)
        cur_h = max(1, size // 64)
        cur_x = (size - cur_w) // 2
        cur_y = cy + by + th + max(2, size // 48)
        if cur_y + cur_h < y1 - border:
            d.rectangle([cur_x, cur_y, cur_x + cur_w, cur_y + cur_h], fill=NEON)

    return img


def save_png(img, name):
    path = os.path.join(OUTDIR, name)
    img.save(path, 'PNG', optimize=True)
    kb = os.path.getsize(path) / 1024
    print(f"  {name:<32s}  {img.size[0]}x{img.size[1]}  {kb:.1f} KB")


def generate():
    os.makedirs(OUTDIR, exist_ok=True)
    print(f"Generating favicons in {OUTDIR}:")

    targets = [
        ("favicon-16x16.png",           16),
        ("favicon-32x32.png",           32),
        ("favicon-48x48.png",           48),
        ("apple-touch-icon.png",        180),
        ("android-chrome-192x192.png",  192),
        ("android-chrome-512x512.png",  512),
    ]
    rendered = {}
    for name, s in targets:
        rendered[s] = draw_mark(s)
        save_png(rendered[s], name)

    # Multi-resolution .ico — PIL's ICO writer downscales from the base
    # image, so we feed it the 48-px render and ask for 16/32/48 frames.
    ico_path = os.path.join(OUTDIR, "favicon.ico")
    rendered[48].save(
        ico_path, format='ICO',
        sizes=[(16, 16), (32, 32), (48, 48)],
    )
    kb = os.path.getsize(ico_path) / 1024
    print(f"  {'favicon.ico':<32s}  multi-res   {kb:.1f} KB")


if __name__ == "__main__":
    generate()
