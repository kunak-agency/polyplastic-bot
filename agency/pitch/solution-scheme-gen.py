#!/usr/bin/env python3
"""Схема решения «Корсик»: что делает система, как ограничивает ошибки, чем подтверждено.
Запуск из agency/pitch: python3 solution-scheme-gen.py solution-scheme.svg
Числа: agency/system/Human/13-criteria-coverage.md §7 и 05-knowledge-base-rag.md."""
import sys
from html import escape

OUT = sys.argv[1]
W, H = 2400, 1350
FONT = "Inter, Arial, sans-serif"
BLUE, BLUE_SOFT, PANEL, PANEL_STROKE = "#1f5fbf", "#dde9f8", "#f2f6fc", "#c7d6ec"
CARD_STROKE, INK, GREY, ARROW = "#c9d4e3", "#1b2a3d", "#4f5a69", "#8a96a6"

out, warn = [], []
add = out.append


def est(s, size, bold=False):
    return len(s) * size * (0.60 if bold else 0.55)


def text(x, y, s, size=19, weight="normal", color=GREY, anchor="start", maxw=None):
    if maxw is not None and est(s, size, weight == "bold") > maxw:
        warn.append(f"overflow? {s!r} est={est(s, size, weight == 'bold'):.0f} max={maxw}")
    add(f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" fill="{color}" '
        f'text-anchor="{anchor}">{escape(s)}</text>')


def rect(x, y, w, h, fill="#fff", stroke=CARD_STROKE, rx=12, sw=1.4, dash=None):
    d = f' stroke-dasharray="{dash}"' if dash else ""
    add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill}" '
        f'stroke="{stroke}" stroke-width="{sw}"{d}/>')


def badge(x, y, n, r=19):
    add(f'<circle cx="{x}" cy="{y}" r="{r}" fill="{BLUE}"/>')
    text(x, y + 7, str(n), 20, "bold", "#fff", "middle")


def block(x, y, w, h, n, title, lines, fill=PANEL, stroke=PANEL_STROKE, ts=24, ls=19, step=29):
    rect(x, y, w, h, fill=fill, stroke=stroke, rx=16)
    badge(x + 34, y + 38, n)
    text(x + 64, y + 46, title, ts, "bold", INK, maxw=w - 80)
    for i, ln in enumerate(lines):
        text(x + 24, y + 90 + i * step, ln, ls, maxw=w - 48)


def box(x, y, w, h, lines, size=19, dash=None, stroke=CARD_STROKE, bold_first=False):
    rect(x, y, w, h, stroke=stroke, dash=dash)
    lh = size + 7
    top = y + h / 2 - lh * (len(lines) - 1) / 2 + size * 0.35
    for i, ln in enumerate(lines):
        text(x + w / 2, top + i * lh, ln, size, "bold" if (bold_first and i == 0) else "normal",
             INK if (bold_first and i == 0) else GREY, "middle", maxw=w - 20)


def arrow(pts, dashed=False, color=ARROW, label=None, lx=0, ly=0, anchor="start"):
    d = "M " + " L ".join(f"{a} {b}" for a, b in pts)
    ds = ' stroke-dasharray="8 6"' if dashed else ""
    add(f'<path d="{d}" fill="none" stroke="{color}" stroke-width="2"{ds} marker-end="url(#ah)"/>')
    if label:
        text(lx, ly, label, 17, "bold", GREY, anchor)


def plaque(y, s, w):
    rect(40, y, w, 64, fill=BLUE_SOFT, stroke="none", rx=10)
    text(64, y + 46, s, 38, "bold", BLUE)


add(f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" font-family="{FONT}">')
add('<title>Корсик: схема решения</title>')
add(f'<defs><marker id="ah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" '
    f'orient="auto"><path d="M0 0 L10 5 L0 10 z" fill="{ARROW}"/></marker></defs>')
add(f'<rect width="{W}" height="{H}" fill="#fbfcfe"/>')
add(f'<rect x="10" y="10" width="{W-20}" height="{H-20}" rx="14" fill="none" stroke="{BLUE}" stroke-width="1.6"/>')

# ===================== 1. Что делает система
add(f'<rect x="24" y="26" width="{W-48}" height="326" rx="14" fill="#fff" stroke="#e1e8f2"/>')
plaque(40, "1. Что делает система", 500)
text(2356, 80, "Корсик · ПОЛИПЛАСТИК · «Открой#Моспром» 2026", 17, color=ARROW, anchor="end")
bw, bg = 537, 60
does = [
    ("HR задаёт сценарий", ["Загружает свой Excel или правит шаги",
                            "в конструкторе: дни, кнопки, опросы,", "площадки."]),
    ("Бот сопровождает новичка в MAX", ["Присылает шаги по дате выхода",
                                        "и площадке, в рабочее время.",
                                        "Пока новичок не ответил на вопрос,",
                                        "следующий шаг ждёт."]),
    ("Бот отвечает на вопросы", ["Текстом или голосом, по регламентам.",
                                 "К ответу прикладывает ссылку на пункт",
                                 "ПВТР или слайд презентации."]),
    ("Бот передаёт проблему человеку", ["Тревожный ответ или жалоба становится",
                                        "обращением к HR. Жалобы на руководителя",
                                        "получают HR-партнёр и СБ, руководитель",
                                        "их не видит."]),
]
for i, (t, ls) in enumerate(does):
    x = 36 + i * (bw + bg)
    block(x, 120, bw, 218, i + 1, t, ls)
    if i:
        arrow([(x - bg + 8, 229), (x - 8, 229)])

# ===================== 2. Как система ограничивает ошибки
add(f'<rect x="24" y="366" width="{W-48}" height="486" rx="14" fill="#fff" stroke="#e1e8f2"/>')
plaque(380, "2. Как система ограничивает ошибки", 760)

# 5: ответ только из документа
rect(36, 460, 1364, 378, fill=PANEL, stroke=PANEL_STROKE, rx=16)
badge(70, 498, 5)
text(100, 506, "Модель может придумать ответ", 24, "bold", INK)
text(100, 538, "Ответ строится только из документа:", 19, color=GREY)
cyb = 675
bx = [52, 305, 558]
steps = [["Поиск находит", "подходящие пункты", "регламентов"],
         ["Модель получает", "только эти пункты", "и пишет ответ"],
         ["Код сверяет", "цитату и числа", "с текстом пункта"]]
for i, ls in enumerate(steps):
    box(bx[i], cyb - 60, 215, 120, ls)
    if i:
        arrow([(bx[i] - 38, cyb), (bx[i] - 4, cyb)])
dcx, dw, dh = 930, 230, 176
arrow([(773, cyb), (dcx - dw / 2 - 3, cyb)])
add(f'<path d="M {dcx-dw/2} {cyb} L {dcx} {cyb-dh/2} L {dcx+dw/2} {cyb} L {dcx} {cyb+dh/2} Z" '
    f'fill="#fff" stroke="{BLUE}" stroke-width="2"/>')
text(dcx, cyb - 18, "Ответ", 19, "bold", INK, "middle")
text(dcx, cyb + 6, "подтверждён", 19, "bold", INK, "middle")
text(dcx, cyb + 30, "документом?", 19, "bold", INK, "middle")
box(1122, 560, 262, 90, ["Ответ со ссылкой", "на пункт"], size=19)
arrow([(dcx + dw / 2, cyb), (1102, cyb), (1102, 605), (1120, 605)], label="да", lx=1074, ly=cyb - 10, anchor="middle")
box(1122, 700, 262, 120, ["«Не нашёл", "в документах»", "и кнопка «Передать HR»"], size=18,
    stroke=BLUE, dash="7 5")
arrow([(dcx, cyb + dh / 2), (dcx, 790), (1120, 790)], dashed=True, label="нет", lx=dcx + 10, ly=782)

# 6–9: остальные контрольные точки
cw, cgx = 452, 30
controls = [
    ("Вопрос на чувствительную тему", ["Вопросы об увольнении, конфликте",
                                       "и здоровье бот сразу передаёт человеку,",
                                       "модель на них не отвечает."]),
    ("Ошибка в сценарии", ["Импорт помечает проблемы ТЗ",
                           "предупреждением со ссылкой на строку.",
                           "Перед публикацией HR видит, что изменится",
                           "и скольких новичков это затронет."]),
    ("Сервер с моделью недоступен", ["Бот отвечает цитатой найденного пункта.",
                                     "Сценарии, опросы и обращения",
                                     "работают без модели."]),
    ("Новичок не ответил", ["Когда подходит следующий блок,",
                            "обязательные шаги приходят одной",
                            "сводкой «Что вы пропустили»,",
                            "необязательные бот пропускает."]),
]
for i, (t, ls) in enumerate(controls):
    x = 1430 + (i % 2) * (cw + cgx)
    y = 460 + (i // 2) * 197
    rect(x, y, cw, 181, rx=14)
    badge(x + 32, y + 34, 6 + i, r=17)
    text(x + 60, y + 41, t, 21, "bold", INK, maxw=cw - 76)
    for j, ln in enumerate(ls):
        text(x + 22, y + 80 + j * 25, ln, 18, maxw=cw - 40)

# ===================== 3. Чем подтверждено
add(f'<rect x="24" y="866" width="{W-48}" height="438" rx="14" fill="#fff" stroke="#e1e8f2"/>')
plaque(880, "3. Чем подтверждено", 470)
kw, kg = 446, 24.5
proofs = [
    ("70", "строк Excel-ТЗ заказчика проверены",
     ["3 площадки, 18 тестовых новичков,", "период с D−3 по D+92,", "имитация MAX."]),
    ("0", "расхождений после выгрузки",
     ["Excel заказчика (57 шагов в 21 блоке)", "загружен в конструктор, выгружен",
      "обратно и сравнен с исходным."]),
    ("85 %", "разговорных вопросов: верный пункт",
     ["60 вопросов по ПВТР и 3 презентациям,", "из них 26 разговорных.",
      "На 22 новых вопросах, которых не было", "при настройке: ≈60 % (13–14 из 22)."]),
    ("25 из 25", "отказов на вопросах вне документов",
     ["20 вопросов, ответа на которые", "в документах нет, и 5 попыток взлома."]),
]
for i, (n, lab, meth) in enumerate(proofs):
    x = 36 + i * (kw + kg)
    rect(x, 960, kw, 328, rx=14)
    text(x + 24, 1052, n, 76, "bold", BLUE)
    text(x + 24, 1096, lab, 21, "bold", INK, maxw=kw - 48)
    for j, ln in enumerate(meth):
        text(x + 24, 1134 + j * 26, ln, 18, maxw=kw - 48)
x = 36 + 4 * (kw + kg)
rect(x, 960, kw, 328, rx=14)
text(x + 24, 1040, "Живой MAX", 50, "bold", BLUE)
text(x + 24, 1096, "на телефоне", 21, "bold", INK)
for j, ln in enumerate(["Приглашение → ответ со ссылкой", "на пункт → передача HR →",
                        "ответ HR в чате."]):
    text(x + 24, 1134 + j * 26, ln, 18, maxw=kw - 48)
text(x + 24, 1262, "Новички и данные тестовые.", 17, color=ARROW)

text(40, 1328, "Go · PostgreSQL · Vue · MAX Bot API · T-lite-it-2.1 (Qwen3) · bge-m3 · T-one · ВМ + GPU-серверы",
     16, color=ARROW)

add('</svg>')
open(OUT, "w", encoding="utf-8").write("\n".join(out) + "\n")
for w_ in warn:
    print(w_)
print("ok", OUT)
