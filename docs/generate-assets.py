#!/usr/bin/env python3
"""Generate hive-go's repository artwork.

Outputs (all committed under docs/):
  social-preview.png  1280x640 image for GitHub Settings -> Social preview
  demo.png            terminal screenshot used at the top of README.md
  logo.png            512x512 logo mark

Requires Pillow and the Noto fonts that ship with most Linux distributions:

    python3 docs/generate-assets.py

The layout is deliberately data-driven so the artwork can be regenerated when
the CLI output or product copy changes.
"""

from __future__ import annotations

import math
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont

HERE = Path(__file__).resolve().parent

SANS = "/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf"
SANS_BOLD = "/usr/share/fonts/truetype/noto/NotoSans-Bold.ttf"
MONO = "/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf"
MONO_BOLD = "/usr/share/fonts/truetype/noto/NotoSansMono-Bold.ttf"

# Palette: dark terminal surface with Hive amber and a cool teal accent.
BG_TOP = (16, 21, 29)
BG_BOTTOM = (7, 9, 13)
CARD = (13, 17, 23)
CARD_BAR = (22, 27, 34)
BORDER = (48, 54, 61)
TEXT = (230, 237, 243)
MUTED = (139, 148, 158)
DIM = (110, 118, 129)
AMBER = (240, 160, 43)
AMBER_LIGHT = (245, 200, 120)
BLUE = (121, 192, 255)
CYAN = (86, 212, 221)
GREEN = (63, 185, 80)
WHITE = (240, 246, 252)


def load(path: str, size: int) -> ImageFont.FreeTypeFont:
    return ImageFont.truetype(path, size)


def blend(fg: tuple[int, int, int], alpha: int, bg: tuple[int, int, int]) -> tuple[int, int, int, int]:
    """Flatten a translucent colour onto an opaque background.

    ImageDraw writes RGBA values directly instead of compositing, so translucent
    draws would otherwise turn solid when the image is converted to RGB.
    """
    a = alpha / 255
    return tuple(round(fg[i] * a + bg[i] * (1 - a)) for i in range(3)) + (255,)


def hex_points(cx: float, cy: float, r: float) -> list[tuple[float, float]]:
    """Pointy-top hexagon vertices."""
    return [
        (cx + r * math.cos(math.radians(60 * i - 30)),
         cy + r * math.sin(math.radians(60 * i - 30)))
        for i in range(6)
    ]


def draw_hex(draw: ImageDraw.ImageDraw, cx, cy, r, **kwargs) -> None:
    draw.polygon(hex_points(cx, cy, r), **kwargs)


def draw_background(width: int, height: int) -> Image.Image:
    img = Image.new("RGBA", (width, height), BG_BOTTOM + (255,))
    draw = ImageDraw.Draw(img)
    for y in range(height):
        t = y / max(1, height - 1)
        color = tuple(round(BG_TOP[i] * (1 - t) + BG_BOTTOM[i] * t) for i in range(3))
        draw.line([(0, y), (width, y)], fill=color + (255,))

    # Soft colour washes: amber behind the wordmark, teal behind the terminal.
    glow = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    gd = ImageDraw.Draw(glow)
    gd.ellipse([-260, -300, 620, 460], fill=(240, 160, 43, 46))
    gd.ellipse([760, 300, 1420, 880], fill=(42, 180, 190, 34))
    glow = glow.filter(ImageFilter.GaussianBlur(110))
    img.alpha_composite(glow)

    # Honeycomb watermark.
    honeycomb = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    hd = ImageDraw.Draw(honeycomb)
    r = 38
    dx = math.sqrt(3) * r
    dy = 1.5 * r
    row = -1
    cy = -r
    while cy < height + r:
        row += 1
        cx = -dx + (dx / 2 if row % 2 else 0)
        while cx < width + dx:
            draw_hex(hd, cx, cy, r, outline=(240, 160, 43, 16), width=1)
            cx += dx
        cy += dy
    img.alpha_composite(honeycomb)

    # A few filled cells as an accent, kept very quiet.
    accent = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    ad = ImageDraw.Draw(accent)
    for cx, cy, alpha in [(1090, 120, 26), (1150, 230, 18), (1040, 430, 16)]:
        draw_hex(ad, cx, cy, 34, fill=(240, 160, 43, alpha),
                 outline=(240, 160, 43, alpha + 20), width=2)
    img.alpha_composite(accent)
    return img


def draw_logo_mark(img: Image.Image, cx: float, cy: float, r: float, scale: float = 1.0) -> None:
    draw = ImageDraw.Draw(img)
    draw_hex(draw, cx, cy, r, fill=blend(AMBER, 26, CARD),
             outline=blend(AMBER, 225, CARD), width=max(2, round(4 * scale)))
    glyph = load(MONO_BOLD, round(24 * scale))
    text = ">_"
    w = draw.textlength(text, font=glyph)
    bbox = glyph.getbbox(text)
    h = bbox[3] - bbox[1]
    draw.text((cx - w / 2, cy - h / 2 - bbox[1]), text, font=glyph, fill=AMBER)


def draw_terminal(img: Image.Image, x: int, y: int, w: int, h: int, s: float = 1.0) -> None:
    draw = ImageDraw.Draw(img)

    shadow = Image.new("RGBA", img.size, (0, 0, 0, 0))
    sd = ImageDraw.Draw(shadow)
    sd.rounded_rectangle(
        [x + 4 * s, y + 14 * s, x + w + 4 * s, y + h + 18 * s],
        radius=round(20 * s),
        fill=(0, 0, 0, 150),
    )
    shadow = shadow.filter(ImageFilter.GaussianBlur(round(16 * s)))
    img.alpha_composite(shadow)

    radius = round(18 * s)
    draw.rounded_rectangle([x, y, x + w, y + h], radius=radius, fill=CARD + (255,))
    bar = round(44 * s)
    draw.rounded_rectangle([x, y, x + w, y + bar], radius=radius, fill=CARD_BAR + (255,))
    draw.rectangle([x, y + bar - radius, x + w, y + bar], fill=CARD_BAR + (255,))
    draw.line([x, y + bar, x + w, y + bar], fill=BORDER + (255,), width=max(1, round(s)))
    draw.rounded_rectangle(
        [x, y, x + w, y + h], radius=radius,
        outline=BORDER + (255,), width=max(1, round(s)),
    )

    for i, color in enumerate([(255, 95, 87), (254, 188, 46), (40, 200, 64)]):
        cx = x + (24 + i * 18) * s
        cy = y + bar / 2
        rr = 6 * s
        draw.ellipse([cx - rr, cy - rr, cx + rr, cy + rr], fill=color + (255,))

    title = load(SANS, round(13 * s))
    title_text = "hive — bash"
    tw = draw.textlength(title_text, font=title)
    draw.text((x + w / 2 - tw / 2, y + bar / 2), title_text, font=title,
              fill=MUTED, anchor="lm")

    mono = load(MONO, round(16 * s))
    mono_bold = load(MONO_BOLD, round(16 * s))
    advance = draw.textlength("0", font=mono)
    left = x + round(26 * s)
    top = y + bar + round(22 * s)
    line_h = round(26 * s)

    empty = (0, 0, 0, 0)

    def line(parts, row: int, *, bold_prompt: bool = False) -> None:
        px = left
        py = top + row * line_h
        for text, color in parts:
            f = mono_bold if (bold_prompt and text == "$ ") else mono
            draw.text((px, py), text, font=f, fill=color + (255,))
            px += draw.textlength(text, font=f)

    line([("$ ", AMBER), ("hive -u ", TEXT), ("hive2://hive:10000/default", BLUE), (" \\", TEXT)], 0,
         bold_prompt=True)
    line([("    ", TEXT), ("--timing", GREEN), (" -e ", TEXT), ('"SELECT id, s FROM demo"', CYAN)], 1)

    table = [
        ("+----+------+", BORDER),
        ("| id | s    |", AMBER_LIGHT),
        ("+----+------+", BORDER),
        ("|  0 | NULL |", TEXT),
        ("|  1 | v1   |", TEXT),
        ("|  2 | v2   |", TEXT),
        ("+----+------+", BORDER),
    ]
    for i, (text, color) in enumerate(table):
        row = 3 + i
        if "NULL" in text:
            before, after = text.split("NULL", 1)
            line([(before, TEXT), ("NULL", DIM), (after, TEXT)], row)
        else:
            line([(text, color)], row)

    line([("Time taken: 0.124 seconds, Fetched: 3 row(s)", GREEN)], 11)
    line([("$ ", AMBER)], 13, bold_prompt=True)
    cursor_x = left + advance * 2
    cursor_y = top + 13 * line_h + 2 * s
    draw.rounded_rectangle(
        [cursor_x, cursor_y, cursor_x + 9 * s, cursor_y + 17 * s],
        radius=round(2 * s), fill=AMBER + (255,),
    )


def render_social_preview() -> Image.Image:
    img = draw_background(1280, 640)
    draw = ImageDraw.Draw(img)

    # Logo + wordmark.
    draw_logo_mark(img, 112, 128, 36, scale=1.0)
    word = load(SANS_BOLD, 56)
    x = 164
    draw.text((x, 96), "hive", font=word, fill=WHITE + (255,))
    x += draw.textlength("hive", font=word)
    draw.text((x, 96), "-go", font=word, fill=AMBER + (255,))

    tagline = load(SANS_BOLD, 29)
    draw.text((82, 196), "Pure-Go HiveServer2 client & CLI",
              font=tagline, fill=TEXT + (255,))

    body = load(SANS, 19)
    draw.text((82, 248), "Query Apache Hive without a JVM, Hadoop client",
              font=body, fill=MUTED + (255,))
    draw.text((82, 277), "libraries, or CGO — one static binary, zero setup.",
              font=body, fill=MUTED + (255,))

    # Capability pills.
    pill_font = load(SANS_BOLD, 15)
    pills = ["No JVM", "No Hadoop", "No CGO", "Single binary"]
    py = 344
    ph = 36
    px = 82
    for label in pills:
        tw = draw.textlength(label, font=pill_font)
        pw = tw + 30
        draw.rounded_rectangle(
            [px, py, px + pw, py + ph], radius=ph // 2,
            fill=blend(AMBER, 22, (14, 18, 25)),
            outline=blend(AMBER, 120, (14, 18, 25)), width=1,
        )
        draw.text((px + pw / 2, py + ph / 2), label, font=pill_font,
                  fill=AMBER_LIGHT + (255,), anchor="mm")
        px += pw + 10

    # Trust signal.
    verified = load(SANS, 15)
    dot_r = 4
    dot_y = 425
    draw.ellipse([82, dot_y - dot_r, 82 + dot_r * 2, dot_y + dot_r],
                 fill=GREEN + (255,))
    draw.text((100, dot_y), "Verified end-to-end against Apache Hive 3.1 and 4.0",
              font=verified, fill=(139, 148, 158, 255), anchor="lm")
    platforms = load(SANS, 14)
    draw.text((82, 452), "NONE / NOSASL / LDAP auth  ·  V10 / V6  ·  Linux / macOS / Windows",
              font=platforms, fill=DIM + (255,))

    # Footer.
    footer = load(MONO, 16)
    draw.text((82, 558), "github.com/wgzhao/hive-go", font=footer, fill=(139, 148, 158, 255))
    detail = load(SANS, 13)
    draw.text((82, 588), "MIT licensed  ·  Hive 3.x / 4.x  ·  protocol V10",
              font=detail, fill=DIM + (255,))

    draw_terminal(img, 650, 80, 560, 480, s=1.0)
    return img.convert("RGB")


def render_demo() -> Image.Image:
    s = 2
    margin = 24 * s
    w, h = 560, 480
    img = Image.new("RGBA", (w * s + margin * 2, h * s + margin * 2), (0, 0, 0, 0))
    draw_terminal(img, margin, margin, w * s, h * s, s=s)
    return img


def render_logo() -> Image.Image:
    size = 512
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([0, 0, size - 1, size - 1], radius=96, fill=CARD + (255,))

    glow = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    gd = ImageDraw.Draw(glow)
    gd.ellipse([40, 40, 472, 472], fill=(240, 160, 43, 34))
    glow = glow.filter(ImageFilter.GaussianBlur(48))
    img.alpha_composite(glow)

    draw.rounded_rectangle([1, 1, size - 1, size - 1], radius=96,
                           outline=(48, 54, 61, 255), width=2)
    draw_logo_mark(img, size / 2, size / 2, 154, scale=4.2)

    # Subtle honeycomb detail in the corners.
    for cx, cy in [(92, 92), (420, 404), (420, 92), (92, 404)]:
        draw_hex(draw, cx, cy, 30, outline=blend(AMBER, 45, CARD), width=2)
    return img


def main() -> None:
    render_social_preview().save(HERE / "social-preview.png", optimize=True)
    render_demo().save(HERE / "demo.png", optimize=True)
    render_logo().save(HERE / "logo.png", optimize=True)
    for name in ("social-preview.png", "demo.png", "logo.png"):
        path = HERE / name
        print(f"wrote {path.relative_to(HERE.parent)} ({path.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
