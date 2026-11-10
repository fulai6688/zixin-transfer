#!/usr/bin/env python3
"""子欣互传 品牌图标生成器。

设计：深色圆角方块 + 双向流线箭头构成的 "Z"，象征「两台设备互相传送」。
所有尺寸由一张 1024px 母版缩放，保证 32px 下依然清晰。

输出：
  web/public/icon-192.png, icon-512.png, icon-maskable.png, favicon.png, apple-touch-icon.png
  webui/dist/  同名副本（go:embed 用）
  build/icon.ico              Windows 多尺寸图标
  build/icon.png              1024 母版
  build/social-preview.png    GitHub 社交预览 1280x640
"""

from __future__ import annotations

import os
import struct
from PIL import Image, ImageDraw, ImageFilter

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PUBLIC = os.path.join(ROOT, "web", "public")
DIST = os.path.join(ROOT, "webui", "dist")
BUILD = os.path.join(ROOT, "build")

# 品牌色（与 UI 的 CSS 变量一致）
INDIGO = (99, 102, 241)
VIOLET = (139, 92, 246)
CYAN = (34, 211, 238)
BG_TOP = (11, 14, 23)
BG_BOTTOM = (23, 20, 45)


def rounded_mask(size: int, radius_ratio: float = 0.22) -> Image.Image:
    mask = Image.new("L", (size, size), 0)
    d = ImageDraw.Draw(mask)
    r = int(size * radius_ratio)
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=r, fill=255)
    return mask


def diagonal_gradient(size: int, c0, c1) -> Image.Image:
    """便宜好用的对角渐变：先画小图再放大。"""
    small = 64
    g = Image.new("RGB", (small, small))
    px = g.load()
    for y in range(small):
        for x in range(small):
            t = (x + y) / (2 * (small - 1))
            px[x, y] = (
                int(c0[0] + (c1[0] - c0[0]) * t),
                int(c0[1] + (c1[1] - c0[1]) * t),
                int(c0[2] + (c1[2] - c0[2]) * t),
            )
    return g.resize((size, size), Image.BICUBIC)


def draw_z_mark(size: int) -> Image.Image:
    """在透明画布上画「Z + 双向箭头」，用图形叠加而非字体，避免缺字体。"""
    S = size
    layer = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    d = ImageDraw.Draw(layer)

    # 画布内边距：母版 12%，让形状在圆角方块里居中
    pad = S * 0.235
    w = S - 2 * pad                     # 字形宽度
    h = w * 0.86                        # 字形高度
    top = (S - h) / 2
    thick = max(2, int(w * 0.175))

    left = pad
    right = pad + w
    t = top
    b = top + h

    # Z 的上横线（右端带箭头，指向右）
    d.rounded_rectangle([left, t, right - thick * 1.15, t + thick], radius=thick / 2, fill=CYAN)
    arrow = [(right - thick * 1.4, t - thick * 0.42), (right, t + thick / 2), (right - thick * 1.4, t + thick * 1.42)]
    d.polygon(arrow, fill=CYAN)

    # Z 的斜线
    d.line(
        [(right - thick * 0.55, t + thick * 0.62), (left + thick * 0.55, b - thick * 0.62)],
        fill=(255, 255, 255),
        width=thick,
        joint="curve",
    )

    # 下横线（左端带箭头，指向左）
    d.rounded_rectangle([left + thick * 1.15, b - thick, right, b], radius=thick / 2, fill=VIOLET)
    arrow2 = [(left + thick * 1.4, b - thick * 1.42), (left, b - thick / 2), (left + thick * 1.4, b - thick * 0.42)]
    d.polygon(arrow2, fill=VIOLET)

    # 轻微的柔光，让图标在浅色背景上也有层次
    glow = layer.filter(ImageFilter.GaussianBlur(S * 0.012))
    out = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    out = Image.alpha_composite(out, glow)
    out = Image.alpha_composite(out, layer)
    return out


def make_icon(size: int, maskable: bool = False) -> Image.Image:
    # maskable 需要安全区：图形缩到 80%，背景铺满
    scale = 0.80 if maskable else 1.0
    canvas = Image.new("RGBA", (size, size), (0, 0, 0, 0))

    bg = diagonal_gradient(size, BG_TOP, BG_BOTTOM)
    if maskable:
        canvas.paste(bg, (0, 0))
    else:
        canvas.paste(bg, (0, 0), rounded_mask(size))

    mark_size = int(size * scale)
    mark = draw_z_mark(mark_size)
    off = (size - mark_size) // 2
    canvas.alpha_composite(mark, (off, off))
    return canvas


def make_social() -> Image.Image:
    """GitHub 社交预览 1280x640：左侧图标 + 右侧文字。"""
    W, H = 1280, 640
    img = diagonal_gradient(max(W, H), (8, 10, 18), (28, 22, 56)).resize((W, H), Image.BICUBIC).convert("RGBA")

    # 用大图标做背景水印
    big = make_icon(760)
    big = big.filter(ImageFilter.GaussianBlur(1.2))
    img.alpha_composite(big, (W - 700, int((H - 760) / 2)))

    icon = make_icon(200)
    img.alpha_composite(icon, (90, int((H - 200) / 2) - 46))

    d = ImageDraw.Draw(img)
    # 文字用默认位图字体（无外部字体依赖），按尺寸放大排版
    title = "ZIXIN TRANSFER"
    sub = "LAN chat + file transfer, one binary"
    d.text((90, int(H / 2) + 92), title, fill=(255, 255, 255))
    d.text((92, int(H / 2) + 118), sub, fill=(160, 170, 200))
    # 补一条品牌色细线
    d.rounded_rectangle([92, int(H / 2) + 150, 92 + 420, int(H / 2) + 155], radius=3, fill=CYAN)
    return img


def save_ico(master: Image.Image, path: str) -> None:
    sizes = [(256, 256), (128, 128), (64, 64), (48, 48), (32, 32), (16, 16)]
    master.save(path, format="ICO", sizes=sizes)


def main() -> None:
    for p in (PUBLIC, DIST, BUILD):
        os.makedirs(p, exist_ok=True)

    master = make_icon(1024)
    master.save(os.path.join(BUILD, "icon.png"))

    targets = {
        "icon-512.png": make_icon(512),
        "icon-192.png": make_icon(192),
        "icon-maskable.png": make_icon(512, maskable=True),
        "favicon.png": make_icon(64),
        "apple-touch-icon.png": make_icon(180, maskable=True),
    }
    for name, im in targets.items():
        im.save(os.path.join(PUBLIC, name))
        if name.startswith(("icon-", "favicon")):
            im.save(os.path.join(DIST, name))

    save_ico(master, os.path.join(BUILD, "icon.ico"))
    make_social().save(os.path.join(BUILD, "social-preview.png"))

    for name in sorted(os.listdir(PUBLIC)):
        if name.endswith(".png"):
            size = os.path.getsize(os.path.join(PUBLIC, name))
            print(f"  web/public/{name}  {size/1024:.1f} KB")
    for name in ("icon.ico", "social-preview.png"):
        size = os.path.getsize(os.path.join(BUILD, name))
        print(f"  build/{name}  {size/1024:.1f} KB")


if __name__ == "__main__":
    main()
