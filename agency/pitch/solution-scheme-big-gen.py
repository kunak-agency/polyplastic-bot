#!/usr/bin/env python3
"""Крупная версия схемы решения «Корсик»: меньше элементов, шрифты для проектора.
Запуск: python3 solution-scheme-big-gen.py solution-scheme-big.svg"""
import sys
from html import escape

OUT = sys.argv[1]
W, H = 2400, 1350
FONT = "Inter, Arial, sans-serif"
BLUE, BLUE_SOFT, PANEL, PANEL_STROKE = "#1f5fbf", "#dde9f8", "#f2f6fc", "#c7d6ec"
CARD_STROKE, INK, GREY, ARROW, NAVY = "#c9d4e3", "#1b2a3d", "#4f5a69", "#8a96a6", "#15305a"

out, warn = [], []
add = out.append


def est(s, size, bold=False):
    return len(s) * size * (0.60 if bold else 0.55)


def text(x, y, s, size=24, weight="normal", color=GREY, anchor="start", maxw=None):
    if maxw is not None and est(s, size, weight == "bold") > maxw:
        warn.append(f"overflow? {s!r} est={est(s, size, weight == 'bold'):.0f} max={maxw}")
    add(f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" fill="{color}" '
        f'text-anchor="{anchor}">{escape(s)}</text>')


def rect(x, y, w, h, fill="#fff", stroke=CARD_STROKE, rx=14, sw=1.6, dash=None):
    d = f' stroke-dasharray="{dash}"' if dash else ""
    add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill}" '
        f'stroke="{stroke}" stroke-width="{sw}"{d}/>')


def card(x, y, w, h, title, lines=(), ts=34, **kw):
    rect(x, y, w, h, **kw)
    text(x + 22, y + 52, title, ts, "bold", INK, maxw=w - 44)
    for i, ln in enumerate(lines):
        text(x + 22, y + 92 + i * 32, ln, 24, maxw=w - 44)


def panel(x, y, w, h, n, title):
    rect(x, y, w, h, fill=PANEL, stroke=PANEL_STROKE, rx=18)
    add(f'<circle cx="{x+40}" cy="{y+40}" r="24" fill="{BLUE}"/>')
    text(x + 40, y + 50, str(n), 26, "bold", "#fff", "middle")
    text(x + 78, y + 51, title, 30, "bold", INK, maxw=w - 92)


def arrow(pts, dashed=False, color=ARROW, label=None, lx=0, ly=0, anchor="start"):
    d = "M " + " L ".join(f"{a} {b}" for a, b in pts)
    ds = ' stroke-dasharray="10 7"' if dashed else ""
    mk = "ahd" if dashed else "ah"
    add(f'<path d="{d}" fill="none" stroke="{color}" stroke-width="2.6"{ds} marker-end="url(#{mk})"/>')
    if label:
        text(lx, ly, label, 22, color=GREY, anchor=anchor)


def cylinder(x, y, w, h, title, sub):
    ry = 16
    add(f'<path d="M {x} {y+ry} A {w/2} {ry} 0 0 0 {x+w} {y+ry} L {x+w} {y+h-ry} '
        f'A {w/2} {ry} 0 0 1 {x} {y+h-ry} Z" fill="#fff" stroke="{BLUE}" stroke-width="2"/>')
    add(f'<ellipse cx="{x+w/2}" cy="{y+ry}" rx="{w/2}" ry="{ry}" fill="{BLUE_SOFT}" stroke="{BLUE}" stroke-width="2"/>')
    text(x + w / 2, y + ry + 50, title, 32, "bold", INK, "middle", maxw=w - 30)
    text(x + w / 2, y + ry + 86, sub, 24, anchor="middle", maxw=w - 30)


def num(x, y, s, size=88, caps=(), dx=None, capsize=24):
    """Большое число и подпись справа (dx задан) или под ним."""
    text(x, y, s, size, "bold", BLUE)
    for i, c in enumerate(caps):
        if dx is None:
            text(x, y + 40 + i * 30, c, capsize)
        else:
            text(x + dx, y - 30 * (len(caps) - 1 - i) - 4, c, capsize)


add(f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" font-family="{FONT}">')
add('<title>Корсик — схема решения (крупно)</title>')
add('<defs>'
    f'<marker id="ah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto"><path d="M0 0 L10 5 L0 10 z" fill="{ARROW}"/></marker>'
    f'<marker id="ahd" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto"><path d="M0 0 L10 5 L0 10 z" fill="{BLUE}"/></marker>'
    '</defs>')
add(f'<rect width="{W}" height="{H}" fill="#fbfcfe"/>')
add(f'<rect x="10" y="10" width="{W-20}" height="{H-20}" rx="16" fill="none" stroke="{BLUE}" stroke-width="2"/>')

# ======================= СЕКЦИЯ 1
add(f'<rect x="24" y="26" width="{W-48}" height="764" rx="16" fill="#fff" stroke="#e1e8f2"/>')
rect(40, 40, 1420, 84, fill=BLUE_SOFT, stroke="none", rx=12)
text(66, 101, "1. Ведём сотрудника и отвечаем по документам", 56, "bold", BLUE)
text(2356, 96, "поток слева направо →", 26, anchor="end")

# ① Excel-ТЗ и конструктор
panel(36, 144, 500, 630, 1, "Сценарий HR")
cylinder(110, 226, 352, 128, "Excel-ТЗ HR", "загружается как есть")
arrow([(286, 354), (286, 380)])
card(52, 384, 468, 374, "Конструктор", ["ветки, опросы, площадки"])
num(74, 640, "57", 96, ["шагов из ТЗ"])
num(290, 640, "0", 96, ["расхождений", "Excel → Excel"])

# ② планировщик
panel(566, 144, 450, 630, 2, "Планировщик")
card(582, 214, 418, 544, "Ведёт в MAX", ["по площадке и", "рабочим часам"])
text(604, 480, "D−3…D+90", 64, "bold", BLUE, maxw=380)
text(604, 524, "от даты выхода", 24)
text(604, 554, "сотрудника", 24)
arrow([(520, 470), (580, 470)])
arrow([(1000, 470), (1031, 470), (1031, 304), (1060, 304)])

# ③ ответы по документам
panel(1046, 144, 860, 630, 3, "Ответы по документам")
cw, cg, cx0 = 252, 36, 1062
chain = [("Поиск", ["по регламентам"]), ("Модель", ["T-lite пишет", "по пункту"]),
         ("Проверка", ["кодом: цитата", "и числа"])]
for i, (t, ls) in enumerate(chain):
    x = cx0 + i * (cw + cg)
    card(x, 214, cw, 180, t, ls)
    if i:
        arrow([(x - cg, 304), (x - 2, 304)])
# перенос к ромбу
pcx = cx0 + 2 * (cw + cg) + cw / 2
dcx, dcy, dw, dh = 1210, 594, 300, 220
arrow([(pcx, 394), (pcx, 420), (dcx, 420), (dcx, dcy - dh / 2 - 2)])
add(f'<path d="M {dcx-dw/2} {dcy} L {dcx} {dcy-dh/2} L {dcx+dw/2} {dcy} L {dcx} {dcy+dh/2} Z" '
    f'fill="#fff" stroke="{BLUE}" stroke-width="2.4"/>')
text(dcx, dcy - 6, "пункт найден", 26, "bold", INK, "middle")
text(dcx, dcy + 26, "и проверен?", 26, "bold", INK, "middle")
# да
arrow([(dcx + dw / 2, dcy), (1400, dcy), (1400, 510), (1438, 510)], label="да", lx=1366, ly=dcy - 12)
card(1440, 430, 450, 160, "Ответ со ссылкой", ts=32)
num(1462, 566, "85 %", 72, ["разговорных", "с верным пунктом"], dx=184)
# нет
arrow([(dcx, dcy + dh / 2), (dcx, 735), (1438, 735)], dashed=True, color=BLUE,
      label="нет", lx=dcx + 14, ly=736 - 8)
card(1440, 610, 450, 148, "Честный отказ", ts=32, stroke=BLUE, dash="9 6")
num(1462, 736, "100 %", 72, ["отказов", "на ловушках"], dx=222)

# ④ риски
panel(1936, 144, 428, 630, 4, "Риски")
card(1952, 214, 396, 206, "F0–F10", ["ответы опросов", "и тексты сотрудника"])
pills = [("Руководитель", 470), ("СБ", 579), ("HR", 688)]
for name, py in pills:
    rect(1952, py, 348, 70, fill=BLUE_SOFT, stroke=BLUE, rx=35, sw=2)
    text(2126, py + 45, name, 28, "bold", BLUE, "middle")
prev = 420
for name, py in pills:
    arrow([(2330, prev), (2330, py + 35), (2302, py + 35)])
    prev = py + 35
arrow([(1890, 723), (1950, 723)], dashed=True, color=BLUE)

# ======================= СЕКЦИЯ 2
add(f'<rect x="24" y="806" width="{W-48}" height="520" rx="16" fill="#fff" stroke="#e1e8f2"/>')
rect(40, 820, 440, 84, fill=BLUE_SOFT, stroke="none", rx=12)
text(66, 881, "2. Проверено", 56, "bold", BLUE)
text(2356, 876, "на сценарии и регламентах заказчика", 26, anchor="end")
arrow([(1476, 774), (1476, 926)], dashed=True, color=BLUE)

kw, kg = 559, 30
kpis = [("Сверка ТЗ", "70", "строк, 3 площадки, 18 сотрудников"),
        ("Круг через Excel", "0", "расхождений после круга"),
        ("Ответы по документам", "85 %", "разговорных вопросов с верным пунктом"),
        ("Ловушки и взломы", "100 %", "отказов на ловушках и взломах")]
for i, (t, n, c) in enumerate(kpis):
    x = 36 + i * (kw + kg)
    card(x, 930, kw, 245, t)
    text(x + 22, 1090, n, 96, "bold", BLUE)
    text(x + 22, 1140, c, 24, maxw=kw - 44)

rect(36, 1194, 2328, 118, fill=NAVY, stroke=NAVY, rx=14)
prod = [("Кабинет HR", "сценарии, сотрудники, обращения"),
        ("Бот в MAX", "на телефоне сотрудника"),
        ("Открытые модели на GPU", "T-lite, bge-m3, T-one")]
sw_ = 2328 / 3
for i, (t, s) in enumerate(prod):
    x = 36 + i * sw_ + 36
    if i:
        add(f'<line x1="{x-36}" y1="1214" x2="{x-36}" y2="1292" stroke="#3a5687" stroke-width="1.5"/>')
    text(x, 1244, t, 34, "bold", "#fff", maxw=sw_ - 60)
    text(x, 1286, s, 24, color="#b9cdee", maxw=sw_ - 60)

add('</svg>')
open(OUT, "w", encoding="utf-8").write("\n".join(out) + "\n")
for w_ in warn:
    print(w_)
print("ok", OUT)
