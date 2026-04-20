#!/usr/bin/env python3
"""
Generate the OG social sharing image for kunaldawn.com.

Requirements: pip install Pillow
Font: DejaVu Sans Mono (pre-installed on most Linux systems)

Usage: python3 generate_og.py
Output: static/og-image.png (1200x630, ~80KB)
"""

from PIL import Image, ImageDraw, ImageFont
import random
import math
import os

# ─── Config ───
W, H = 1200, 630
OUTPUT = os.path.join(os.path.dirname(__file__), "static", "og-image.png")

# Colors (match site CSS variables)
BG = (7, 16, 19)          # --bg: #071013
NEON = (0, 255, 153)      # --neon: #00ff99
MUTED = (127, 209, 179)   # --muted: #7fd1b3
WHITE = (207, 238, 224)   # body color
SOFT = (168, 216, 196)    # quote/dim text
PANEL = (7, 24, 35)       # --panel: #071823

# Fonts
FONT_PATH = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"
FONT_BOLD_PATH = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"

def mono(size):
    return ImageFont.truetype(FONT_PATH, size)

def bold(size):
    return ImageFont.truetype(FONT_BOLD_PATH, size)


def draw_background(img):
    """Matrix rain + grid + scanlines."""
    random.seed(42)
    chars = "01ABCDEF{}[]<>:;/\\|=+"

    # Matrix rain
    for col in range(0, W, 34):
        for row in range(random.randint(-80, 0), H, 24):
            if random.random() < 0.5:
                c = random.choice(chars)
                a = random.randint(5, 14)
                t = Image.new('RGBA', (18, 22), (0, 0, 0, 0))
                ImageDraw.Draw(t).text((1, 1), c, fill=(0, 255, 153, a), font=mono(12))
                img.paste(t, (col, row), t)

    # Grid
    ov = Image.new('RGBA', (W, H), (0, 0, 0, 0))
    od = ImageDraw.Draw(ov)
    for x in range(0, W, 50):
        od.line([(x, 0), (x, H)], fill=(0, 255, 150, 4))
    for y in range(0, H, 50):
        od.line([(0, y), (W, y)], fill=(0, 255, 150, 4))
    img = Image.alpha_composite(img, ov)

    # Scanlines
    sl = Image.new('RGBA', (W, H), (0, 0, 0, 0))
    for y in range(0, H, 3):
        ImageDraw.Draw(sl).line([(0, y), (W, y)], fill=(0, 0, 0, 10))
    img = Image.alpha_composite(img, sl)

    return img


def draw_panel(img, draw, px, py, pw, ph):
    """Main window panel with title bar."""
    # Panel background
    panel = Image.new('RGBA', (pw, ph), (PANEL[0], PANEL[1], PANEL[2], 225))
    img.paste(panel, (px, py), panel)
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([px, py, px + pw - 1, py + ph - 1], radius=10,
                           outline=(0, 255, 153, 45), width=1)

    # Title bar
    tbh = 44
    tb = Image.new('RGBA', (pw - 2, tbh), (0, 255, 153, 16))
    img.paste(tb, (px + 1, py + 1), tb)
    draw = ImageDraw.Draw(img)
    draw.line([(px, py + tbh), (px + pw - 1, py + tbh)], fill=(0, 255, 153, 40))

    # Traffic lights
    tb_cy = py + tbh // 2
    for i, c in enumerate([(255, 95, 87), (255, 189, 46), (40, 202, 66)]):
        cx = px + 28 + i * 28
        draw.ellipse([cx - 9, tb_cy - 9, cx + 9, tb_cy + 9], fill=c)

    # Title bar text — vertically centered
    tb_font = mono(17)
    tb_text = "vault@kd:~/archive"
    tb_bbox = draw.textbbox((0, 0), tb_text, font=tb_font)
    tb_text_h = tb_bbox[3] - tb_bbox[1]
    tb_text_y = py + (tbh - tb_text_h) // 2
    draw.text((px + 115, tb_text_y), tb_text, fill=NEON, font=tb_font)

    # Neon scan line glow under title bar
    for x in range(px + 1, px + pw - 1):
        p = (x - px) / pw
        a = int(160 * math.sin(p * math.pi))
        for dy in range(3):
            aa = a // (dy + 1)
            if aa > 0:
                draw.point((x, py + tbh + 1 + dy), fill=(0, 255, 153, aa))

    return img, draw, tbh


def draw_corner_glow(img, corners):
    """Subtle neon glow at panel corners."""
    for corner in corners:
        g = Image.new('RGBA', (70, 70), (0, 0, 0, 0))
        gd = ImageDraw.Draw(g)
        for r in range(35, 0, -1):
            a = int(10 * (1 - r / 35))
            gd.ellipse([35 - r, 35 - r, 35 + r, 35 + r], fill=(0, 255, 153, a))
        img.paste(g, (corner[0] - 35, corner[1] - 35), g)
    return img


def generate():
    img = Image.new('RGBA', (W, H), BG)
    img = draw_background(img)
    draw = ImageDraw.Draw(img)

    # ─── Panel ───
    px, py = 44, 34
    pw, ph = W - 88, H - 68
    img, draw, tbh = draw_panel(img, draw, px, py, pw, ph)

    left_x = px + 50
    content_top = py + tbh + 28

    # ─── Badge ───
    badge_text = "\u2588\u2588 KUNAL DAWN \u2588\u2588"
    bf = bold(16)
    badge_bbox = draw.textbbox((0, 0), badge_text, font=bf)
    badge_tw = badge_bbox[2] - badge_bbox[0]
    badge_th = badge_bbox[3] - badge_bbox[1]
    bpad_x, bpad_y = 14, 8
    badge_w = badge_tw + bpad_x * 2
    badge_h = badge_th + bpad_y * 2
    draw.rounded_rectangle(
        [left_x, content_top, left_x + badge_w, content_top + badge_h],
        radius=5, fill=(0, 0, 0, 90), outline=(0, 255, 153, 35)
    )
    draw.text((left_x + bpad_x, content_top + bpad_y), badge_text, fill=NEON, font=bf)

    # ─── Title ───
    ty = content_top + badge_h + 16
    title_font = bold(50)
    draw.text((left_x, ty), "KD's Homebrew", fill=WHITE, font=title_font)
    ty += 58
    draw.text((left_x, ty), "Digital Archive", fill=WHITE, font=title_font)

    # ─── Tagline ───
    ty += 62
    tag_font = mono(20)
    sep_color = (0, 255, 153, 70)
    parts = ["Preserve", "Share", "Rebuild"]
    tx = left_x
    for i, p in enumerate(parts):
        draw.text((tx, ty), p, fill=MUTED, font=tag_font)
        tx += draw.textlength(p, font=tag_font)
        if i < 2:
            tx += draw.textlength("  ", font=tag_font)
            draw.text((tx, ty), "\u2502", fill=sep_color, font=tag_font)
            tx += draw.textlength("\u2502  ", font=tag_font)

    # ─── Archive listing (fills middle space) ───
    ty += 44
    url_font = bold(15)
    desc_font = mono(14)
    archives = [
        ("\u25B8 wiki.kunaldawn.com",
         "36 offline wikis \u2014 Wikipedia, Stack Overflow, iFixit"),
        ("\u25B8 archive.kunaldawn.com",
         "500+ GB \u2014 OS images, vintage software, chiptunes"),
        ("\u25B8 pdfarchive.kunaldawn.com",
         "23K+ PDFs \u2014 Byte Magazine, hardware manuals, journals"),
    ]
    for url_text, desc in archives:
        draw.text((left_x, ty), url_text, fill=NEON, font=url_font)
        ty += 20
        draw.text((left_x + 18, ty), desc, fill=(SOFT[0], SOFT[1], SOFT[2], 190), font=desc_font)
        ty += 26

    # ─── Stats (right column) ───
    stats = [
        ("36", "Offline Wikis"),
        ("23,000+", "Curated PDFs"),
        ("500+ GB", "Data Archives"),
        ("1.3 TB", "Total Preserved"),
    ]
    box_w = 260
    box_h = 60
    box_gap = 8
    stats_top = content_top + 2
    rx = px + pw - box_w - 44

    val_font = bold(24)
    label_font = mono(14)

    for i, (val, label) in enumerate(stats):
        sy = stats_top + i * (box_h + box_gap)
        box = Image.new('RGBA', (box_w, box_h), (0, 0, 0, 50))
        img.paste(box, (rx, sy), box)
        draw = ImageDraw.Draw(img)
        draw.rounded_rectangle([rx, sy, rx + box_w, sy + box_h], radius=6,
                               outline=(0, 255, 153, 25))
        # Accent bar inset within rounded corners
        draw.rounded_rectangle([rx + 3, sy + 7, rx + 6, sy + box_h - 7],
                               radius=1, fill=NEON)
        draw.text((rx + 18, sy + 6), val, fill=NEON, font=val_font)
        draw.text((rx + 18, sy + 35), label, fill=MUTED, font=label_font)

    # ─── Quote ───
    qy = py + ph - 50
    quote_font = bold(15)
    quote = ('\u201CInformation belongs to everyone \u2014 '
             'it deserves to outlive the servers that first hosted it.\u201D')
    qw = draw.textlength(quote, font=quote_font)
    qx = px + (pw - qw) / 2
    # Decorative line above
    line_y = qy - 14
    line_l = px + 80
    line_r = px + pw - 80
    draw.line([(line_l, line_y), (line_r, line_y)], fill=(0, 255, 153, 25))
    # Small diamonds centered on line ends
    dia_font = mono(8)
    for lx in [line_l, line_r]:
        dw = draw.textlength("\u25C6", font=dia_font)
        draw.text((lx - dw / 2, line_y - 5), "\u25C6", fill=(0, 255, 153, 45), font=dia_font)
    draw.text((qx, qy), quote, fill=NEON, font=quote_font)

    # ─── Bottom taskbar ───
    bar_h = 38
    bar_y = H - bar_h
    bar = Image.new('RGBA', (W, bar_h), (0, 12, 8, 240))
    img.paste(bar, (0, bar_y), bar)
    draw = ImageDraw.Draw(img)
    # Top border with glow
    draw.line([(0, bar_y), (W, bar_y)], fill=(0, 255, 153, 50))
    draw.line([(0, bar_y + 1), (W, bar_y + 1)], fill=(0, 255, 153, 15))

    bx = 12
    by_c = bar_y + bar_h // 2

    # Start button — dark fill, center-aligned text
    btn_w = 96
    btn_t, btn_b = bar_y + 5, bar_y + bar_h - 5
    draw.rounded_rectangle([bx, btn_t, bx + btn_w, btn_b],
                           radius=4, outline=(0, 255, 153, 80),
                           fill=(0, 20, 15, 220))
    start_font = bold(15)
    start_text = "vault@kd"
    stw = draw.textlength(start_text, font=start_font)
    st_bbox = draw.textbbox((0, 0), start_text, font=start_font)
    sth = st_bbox[3] - st_bbox[1]
    draw.text((bx + (btn_w - stw) / 2, btn_t + (btn_b - btn_t - sth) / 2),
              start_text, fill=NEON, font=start_font)

    # Separator
    bx += btn_w + 12
    draw.line([(bx, bar_y + 8), (bx, bar_y + bar_h - 8)], fill=(0, 255, 153, 30))

    # Vault taskbar item — dark fill, center-aligned text
    bx += 10
    vault_w = 84
    draw.rounded_rectangle([bx, btn_t, bx + vault_w, btn_b],
                           radius=4, outline=(0, 255, 153, 50),
                           fill=(0, 20, 15, 200))
    vault_font = bold(14)
    vault_text = "Vault"
    vtw = draw.textlength(vault_text, font=vault_font)
    vt_bbox = draw.textbbox((0, 0), vault_text, font=vault_font)
    vth = vt_bbox[3] - vt_bbox[1]
    draw.text((bx + (vault_w - vtw) / 2, btn_t + (btn_b - btn_t - vth) / 2),
              vault_text, fill=WHITE, font=vault_font)
    # Active indicator dot
    draw.rounded_rectangle([bx + 28, bar_y + bar_h - 7, bx + 56, bar_y + bar_h - 5],
                           radius=1, fill=NEON)

    # System tray (right side) — build from right to left
    tray_font = mono(13)
    tray_text_y = bar_y + (bar_h - 13) // 2  # vertically center 13pt text
    tray_sep_top = bar_y + 10
    tray_sep_bot = bar_y + bar_h - 10

    tray_x = W - 18
    tray_items = ["30W", "ARM64", "12TB"]
    for i, item in enumerate(reversed(tray_items)):
        iw = draw.textlength(item, font=tray_font)
        tray_x -= iw
        draw.text((tray_x, tray_text_y), item, fill=(MUTED[0], MUTED[1], MUTED[2], 170), font=tray_font)
        tray_x -= 16
        if i < len(tray_items) - 1:
            draw.line([(tray_x + 6, tray_sep_top), (tray_x + 6, tray_sep_bot)],
                      fill=(0, 255, 153, 22))

    # URL
    url_font = bold(15)
    url = "kunaldawn.com"
    uw = draw.textlength(url, font=url_font)
    url_x = tray_x - uw - 10
    draw.text((url_x, tray_text_y - 1), url, fill=NEON, font=url_font)
    draw.line([(url_x - 10, tray_sep_top), (url_x - 10, tray_sep_bot)],
              fill=(0, 255, 153, 22))

    # ─── Geeky details ───
    detail_font = mono(10)
    dim_green = (0, 255, 153, 28)
    # Hex addresses — bottom-left of panel, top-right well below title bar
    draw.text((px + 12, py + ph - 18), "0x0000FF", fill=dim_green, font=detail_font)
    draw.text((px + pw - 72, py + tbh + 16), "0xC0FFEE", fill=dim_green, font=detail_font)
    # Cursor block in title bar — small and proportionate
    tb_text_end = px + 115 + draw.textlength("vault@kd:~/archive", font=mono(17))
    cursor_h = 14
    cursor_y = py + (tbh - cursor_h) // 2
    draw.rectangle([tb_text_end + 6, cursor_y, tb_text_end + 15, cursor_y + cursor_h],
                   fill=NEON)

    # ─── Corner glow ───
    img = draw_corner_glow(img, [
        (px, py), (px + pw, py), (px, py + ph), (px + pw, py + ph)
    ])

    # ─── Save ───
    os.makedirs(os.path.dirname(OUTPUT), exist_ok=True)
    final = img.convert('RGB')
    final.save(OUTPUT, 'PNG', optimize=True)
    size_kb = os.path.getsize(OUTPUT) // 1024
    print(f"Generated: {OUTPUT}")
    print(f"Size: {final.size[0]}x{final.size[1]}, {size_kb}KB")


if __name__ == "__main__":
    generate()
