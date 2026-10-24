#!/usr/bin/env python3
"""Генератор agency/pitch/solution-scheme.svg (схема решения «Корсик»)."""
import sys
from html import escape

OUT = sys.argv[1]
W, H = 2400, 1350
FONT = "Inter, Arial, sans-serif"
BLUE = "#1f5fbf"
BLUE_SOFT = "#dde9f8"
PANEL = "#f2f6fc"
PANEL_STROKE = "#c7d6ec"
CARD_STROKE = "#c9d4e3"
INK = "#1b2a3d"
GREY = "#5b6675"
ARROW = "#8a96a6"
NAVY = "#15305a"
GREEN_BG = "#e4f4e9"
GREEN = "#2f8a52"
GREEN_INK = "#1d5e36"

out = []
warn = []


def add(s):
    out.append(s)


def est(s, size, bold=False):
    return len(s) * size * (0.60 if bold else 0.55)


def text(x, y, s, size=13, weight="normal", color=GREY, anchor="start", maxw=None):
    if maxw is not None and est(s, size, weight == "bold") > maxw:
        warn.append(f"overflow? {s!r} est={est(s, size, weight == 'bold'):.0f} max={maxw}")
    add(f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" fill="{color}" '
        f'text-anchor="{anchor}">{escape(s)}</text>')


def rect(x, y, w, h, fill="#fff", stroke=CARD_STROKE, rx=10, sw=1.2, dash=None):
    d = f' stroke-dasharray="{dash}"' if dash else ""
    add(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill}" '
        f'stroke="{stroke}" stroke-width="{sw}"{d}/>')


def card(x, y, w, h, title, lines=(), num=None, cap=(), layout="below", ns=40,
         fill="#fff", stroke=CARD_STROKE, tcolor=INK, lcolor=GREY):
    rect(x, y, w, h, fill=fill, stroke=stroke)
    inner = w - 32
    text(x + 16, y + 28, title, 16, "bold", tcolor, maxw=inner)
    for i, ln in enumerate(lines):
        text(x + 16, y + 50 + i * 18, ln, 13, color=lcolor, maxw=inner)
    if num is None:
        return
    if layout == "below":
        nb = y + h - 18 - 17 * len(cap)
        text(x + 16, nb, num, ns, "bold", BLUE, maxw=inner)
        for i, c in enumerate(cap):
            text(x + 16, nb + 20 + i * 17, c, 12.5, color=GREY, maxw=inner)
    else:  # right: число слева, подпись справа от него
        nb = y + h - 18
        text(x + 16, nb, num, ns, "bold", BLUE)
        cx = x + 16 + est(num, ns, True) + 12
        for i, c in enumerate(cap):
            text(cx, nb - 17 * (len(cap) - 1 - i) - 4, c, 12.5, color=GREY,
                 maxw=x + w - 12 - cx)


def badge(x, y, n):
    add(f'<circle cx="{x}" cy="{y}" r="17" fill="{BLUE}"/>')
    text(x, y + 6, str(n), 17, "bold", "#fff", "middle")


def panel(x, y, w, h, n, title):
    rect(x, y, w, h, fill=PANEL, stroke=PANEL_STROKE, rx=16, sw=1.2)
    badge(x + 30, y + 32, n)
    text(x + 58, y + 39, title, 20, "bold", INK, maxw=w - 70)


def arrow(pts, dashed=False, label=None, lx=None, ly=None, anchor="middle", color=ARROW):
    d = "M " + " L ".join(f"{px} {py}" for px, py in pts)
    dash = ' stroke-dasharray="7 5"' if dashed else ""
    mk = "ahd" if dashed else "ah"
    add(f'<path d="{d}" fill="none" stroke="{color}" stroke-width="1.6"{dash} '
        f'marker-end="url(#{mk})"/>')
    if label:
        text(lx, ly, label, 12, color=GREY, anchor=anchor)


def cylinder(x, y, w, h, title, sub, fill="#fff", ts=15, ss=12.5):
    ry = 11
    add(f'<path d="M {x} {y+ry} A {w/2} {ry} 0 0 0 {x+w} {y+ry} L {x+w} {y+h-ry} '
        f'A {w/2} {ry} 0 0 1 {x} {y+h-ry} Z" fill="{fill}" stroke="{BLUE}" stroke-width="1.4"/>')
    add(f'<ellipse cx="{x+w/2}" cy="{y+ry}" rx="{w/2}" ry="{ry}" fill="{BLUE_SOFT}" '
        f'stroke="{BLUE}" stroke-width="1.4"/>')
    cx, mid = x + w / 2, y + ry + (h - ry) / 2
    text(cx, mid - 2, title, ts, "bold", INK, "middle", maxw=w - 16)
    text(cx, mid + ss + 6, sub, ss, color=GREY, anchor="middle", maxw=w - 16)


# ---------------------------------------------------------------- canvas
add(f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" '
    f'font-family="{FONT}">')
add('<title>Корсик: схема решения</title>')
add('<defs>'
    f'<marker id="ah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" '
    f'orient="auto-start-reverse"><path d="M0 0 L10 5 L0 10 z" fill="{ARROW}"/></marker>'
    f'<marker id="ahd" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" '
    f'orient="auto-start-reverse"><path d="M0 0 L10 5 L0 10 z" fill="{BLUE}"/></marker>'
    '</defs>')
add(f'<rect x="0" y="0" width="{W}" height="{H}" fill="#fbfcfe"/>')
add(f'<rect x="10" y="10" width="{W-20}" height="{H-20}" rx="14" fill="none" stroke="{BLUE}" stroke-width="1.5"/>')

# ================================================================ SECTION 1
add(f'<rect x="24" y="26" width="{W-48}" height="776" rx="14" fill="#fff" stroke="#e1e8f2" stroke-width="1"/>')
rect(40, 40, 960, 60, fill=BLUE_SOFT, stroke="none", rx=10)
text(62, 81, "1. Бот сопровождает сотрудника и отвечает по документам", 30, "bold", BLUE)
text(2360, 76, "Корсик · ПОЛИПЛАСТИК · «Открой#Моспром» 2026", 15, color="#8a96a6", anchor="end")

# ---- panel 1: вход
panel(36, 118, 290, 666, 1, "Вход")
cylinder(64, 200, 234, 190, "Excel-ТЗ HR", "сценарий, строки r1–r70", ts=19, ss=14)
arrow([(298, 295), (370, 295)])

rect(52, 430, 258, 330)
text(70, 466, "Кто участвует", 19, "bold", INK, maxw=222)
for i, (who, where) in enumerate([("Сотрудник", "чат в MAX"), ("HR, рекрутер", "кабинет"),
                                    ("Руководители", "MAX"), ("HR-партнёр, СБ", "через обращения")]):
    text(70, 506 + i * 58, who, 15, "bold", INK, maxw=222)
    text(70, 526 + i * 58, where, 14, color=GREY, maxw=222)
text(70, 742, "данные в репозитории синтетические", 11.5, color="#8a96a6", maxw=222)

# ---- panel 2: конструктор и планировщик
panel(356, 118, 500, 666, 2, "Конструктор и планировщик")
card(372, 185, 220, 220, "Импорт Excel-ТЗ", [
    "ищет колонки по шапке,",
    "на каждую проблему ТЗ",
    "выдаёт предупреждение",
    "со ссылкой на строку",
], num="57", cap=["шагов в 21 блоке;", "в них 29 веток и 5 опросов"])
card(620, 185, 220, 220, "Конструктор HR", [
    "дни, ветки, опросы,",
    "площадки, «ты» / «вы»,",
    "выгрузка обратно в Excel",
], num="0", cap=["расхождений после", "выгрузки и сравнения"])
arrow([(592, 250), (618, 250)])

cylinder(388, 420, 168, 82, "Postgres 16", "сценарии, сотрудники")
rect(620, 432, 220, 46, fill=BLUE_SOFT, stroke=BLUE, rx=23, sw=1.2)
text(730, 461, "Версия сценария", 15, "bold", BLUE, "middle")
arrow([(730, 405), (730, 430)])
arrow([(620, 455), (558, 455)], label="хранит", lx=589, ly=448)
arrow([(730, 478), (730, 518)], label="публикация", lx=740, ly=503, anchor="start")

card(372, 520, 468, 240, "Планировщик", [
    "отсчитывает шаги от даты выхода сотрудника",
    "учитывает площадку и рабочие часы (пн–чт 9–18, пт 9–17)",
    "пока сотрудник не ответил на вопрос, следующий шаг ждёт",
    "если ответа нет, напоминает и не задерживает остальных",
], num="33–35", cap=["сообщений за 90 дней получит сотрудник, который не отвечает",
                     "(раньше доходило 1)"])
# планировщик -> MAX (панель 3)
arrow([(840, 600), (871, 600), (871, 237), (900, 237)])
text(866, 625, "MAX", 11.5, "bold", GREY, "middle")

# ---- panel 3: ответы по документам
panel(886, 118, 1000, 666, 3, "Ответы по документам")
card(902, 185, 180, 105, "Сотрудник в MAX", [
    "получает шаги,",
    "пишет вопрос текстом",
    "или голосом (T-one)",
])
card(1124, 185, 180, 105, "Маршрут тем", [
    "увольнение, конфликт",
    "и здоровье передаёт",
    "человеку без модели",
])
arrow([(1082, 237), (1122, 237)])
cylinder(912, 312, 160, 93, "Регламенты", "ПВТР + 3 презентации")
card(1346, 185, 240, 220, "Гибридный поиск", [
    "BM25 по словоформам",
    "+ bge-m3 по смыслу,",
    "слияние RRF;",
    "словарь сленга: 19 групп",
], num="20", cap=["лучших пунктов на реранкер"], ns=38)
arrow([(1304, 237), (1344, 237)])
arrow([(1072, 362), (1344, 362)], label="пункты и слайды", lx=1208, ly=355)
card(1628, 185, 242, 220, "Реранкер + порог", [
    "bge-reranker-v2-m3",
    "оценивает 8 лучших;",
    "ниже порога модель",
    "переформулирует вопрос",
], num="0,2", cap=["порог оценки пункта"], ns=38)
arrow([(1586, 290), (1626, 290)])
# перенос строки: реранкер -> T-lite
arrow([(1749, 405), (1749, 437), (1007, 437), (1007, 463)],
      label="найденные пункты", lx=1378, ly=430)

card(902, 465, 210, 235, "T-lite формулирует", [
    "T-lite-it-2.1 на vLLM",
    "(на базе Qwen3)",
    "только по найденным",
    "пунктам, на «ты» / «вы»;",
    "нет ответа модели:",
    "отдаёт цитату пункта",
])
card(1142, 465, 220, 235, "Проверка кодом", [
    "цитата есть в пункте,",
    "все числа есть в пункте,",
    "ответ ≤ 700 символов",
], num="100 %", cap=["цитата дословно", "совпадает с пунктом"], ns=38)
arrow([(1112, 545), (1140, 545)])
# ромб
cx, cy, dw, dh = 1477, 545, 170, 124
add(f'<path d="M {cx-dw/2} {cy} L {cx} {cy-dh/2} L {cx+dw/2} {cy} L {cx} {cy+dh/2} Z" '
    f'fill="#fff" stroke="{BLUE}" stroke-width="1.6"/>')
text(cx, cy - 12, "Ответ", 14, "bold", INK, "middle")
text(cx, cy + 5, "подтверждён", 14, "bold", INK, "middle")
text(cx, cy + 22, "документом?", 14, "bold", INK, "middle")
arrow([(1362, 545), (cx - dw / 2 - 2, 545)])
arrow([(cx + dw / 2, 545), (1598, 545)], label="да", lx=1580, ly=536)
# да
rect(1600, 465, 270, 160)
text(1616, 492, "Ответ со ссылкой", 16, "bold", INK)
text(1616, 512, "доля ответов с верным пунктом:", 13, color=GREY, maxw=238)
text(1616, 562, "85 %", 34, "bold", BLUE)
text(1616, 582, "26 разговорных", 12.5, color=GREY)
text(1616, 599, "вопросов", 12.5, color=GREY)
text(1748, 562, "≈60 %", 34, "bold", BLUE)
text(1748, 582, "13–14 из 22", 12.5, color=GREY)
text(1748, 599, "новых вопросов", 12.5, color=GREY)
# нет
arrow([(cx, cy + dh / 2), (cx, 705), (1598, 705)], dashed=True, label="нет", lx=cx + 10, ly=660,
      anchor="start", color=BLUE)
rect(1600, 650, 270, 110, stroke=BLUE, dash="6 4")
text(1616, 676, "Не нашёл в документах", 16, "bold", INK)
text(1616, 696, "предлагает «Передать HR»", 13, color=GREY, maxw=238)
text(1616, 742, "100 %", 30, "bold", BLUE)
text(1712, 728, "отказов: 20 ловушек", 12.5, color=GREY, maxw=150)
text(1712, 745, "и 5 попыток взлома", 12.5, color=GREY, maxw=150)
# нет -> обращение
arrow([(1870, 705), (1930, 705)], dashed=True, color=BLUE)

# ---- panel 4: риски и эскалация
panel(1916, 118, 448, 666, 4, "Риски и эскалация")
card(1932, 185, 416, 130, "Откуда сигнал", [
    "вариант ответа опроса с флагом от HR",
    "слова в тексте: увольнение, удар, взятка",
    "если таких слов нет, текст размечает T-lite",
])
arrow([(2140, 315), (2140, 338)])
card(1932, 340, 416, 150, "Категории риска F0–F10", [
    "адресата и что скрыть",
    "определяет таблица в коде, не модель",
], num="12 из 12", cap=["граничных случаев", "размечены верно,", "выборка малая"], layout="right", ns=34)
arrow([(2031, 490), (2031, 513)])
arrow([(2249, 490), (2249, 513)])
card(1932, 515, 198, 245, "Обращение", [
    "HR, рекрутер,",
    "HR-партнёр, СБ",
    "F4, F5, F9 не видит",
    "руководитель",
    "HR отвечает из",
    "кабинета через бота",
])
card(2150, 515, 198, 245, "Руководителям", [
    "подразделения:",
    "поводы для встречи 1:1",
    "подбора: дайджест",
    "по понедельникам, 9:00",
    "без имён и групп",
    "меньше 5 человек",
])

# ================================================================ SECTION 2
add(f'<rect x="24" y="822" width="{W-48}" height="504" rx="14" fill="#fff" stroke="#e1e8f2" stroke-width="1"/>')
rect(40, 836, 470, 60, fill=BLUE_SOFT, stroke="none", rx=10)
text(62, 877, "2. Проверяем и показываем", 30, "bold", BLUE)

# межсекционная пунктирная стрелка
arrow([(1000, 784), (1000, 910)], dashed=True, label="что проверили", lx=1012, ly=866,
      anchor="start", color=BLUE)

# ---- panel 5: проверки
panel(36, 912, 1144, 400, 5, "Проверки")
cw, gap, x0, cy0, ch = 204, 23, 52, 975, 321
checks = [
    ("Сверка с ТЗ", ["18 тестовых сотрудников,", "3 площадки, D−3 … D+92,", "автотест сценария"],
     "70", ["строк ТЗ проверено"]),
    ("Выгрузка в Excel", ["Excel загружен,", "выгружен обратно,", "сравнен с исходным"],
     "0", ["расхождений"]),
    ("Наборы вопросов", ["основной набор и", "отложенный, собранный", "до настройки словаря"],
     "60+34", ["вопроса на ПВТР", "и 3 презентациях"]),
    ("Тесты", ["подсчёт по коду:", "сервер и сервис", "поиска"],
     "> 200", ["тестов сервера", "и > 200 тестов поиска"]),
    ("Голос", ["T-one + sherpa-onnx", "на нашем GPU-сервере,", "без внешних сервисов"],
     "< 1 с", ["фраза 3,6 с", "распознана за 0,96 с"]),
]
for i, (t, ls, n, c) in enumerate(checks):
    x = x0 + i * (cw + gap)
    card(x, cy0, cw, ch, t, ls, num=n, cap=c, ns=50)

arrow([(1180, 1060), (1208, 1060)])

# ---- panel 6: продукт и инфраструктура
panel(1210, 912, 1154, 252, 6, "Продукт и инфраструктура")
pw, pg, px0, py0, ph = 268, 22, 1226, 975, 175
prod = [
    ("Кабинет HR · Vue 3", ["обзор, сотрудники, обращения,", "конструктор, база знаний,",
                            "медиатека, аналитика"], None, []),
    ("Бот в MAX · Go", ["официальный Go SDK MAX,", "long polling,", "до 2 сообщений в секунду"],
     "2–5 с", ["ответ в живом MAX", "по регламентам"]),
    ("ВМ · Terraform", ["бэкенд, Postgres 16, кабинет", "за Caddy с HTTPS",
                        "выкатка через terraform apply"], None, []),
    ("GPU-поды RunPod", ["поиск и голос: RTX 2000 Ada,", "закрыт ключом;",
                         "T-lite на vLLM: второй под"],
     "5,6 с", ["95 % ответов", "с моделью быстрее"]),
]
for i, (t, ls, n, c) in enumerate(prod):
    x = px0 + i * (pw + pg)
    card(x, py0, pw, ph, t, ls, num=n, cap=c, layout="right", ns=30)

# ---- итоги
rect(1210, 1180, 430, 132, fill=GREEN_BG, stroke=GREEN, rx=12, sw=1.4)
add(f'<circle cx="1243" cy="1209" r="12" fill="{GREEN}"/>')
add('<path d="M 1237 1209 L 1241.5 1213.5 L 1249 1204.5" fill="none" stroke="#fff" stroke-width="2.4" '
    'stroke-linecap="round" stroke-linejoin="round"/>')
text(1264, 1215, "Подтверждено", 18, "bold", GREEN_INK)
for yy, head, rest in [(1244, "Работает в живом MAX: ", "приглашение, ответ"),
                       (1266, "", "со ссылкой на пункт, передача HR, ответ HR в чат."),
                       (1290, "Автотест сценария: ", "70 строк ТЗ, 18 сотрудников, 3 площадки.")]:
    add(f'<text x="1232" y="{yy}" font-size="13.5" fill="{GREEN_INK}">'
        f'<tspan font-weight="bold">{escape(head)}</tspan>{escape(rest)}</text>')

rect(1660, 1180, 704, 132, fill=NAVY, stroke=NAVY, rx=12)
totals = [("70", ["строк ТЗ проверено", "на 3 площадках"]),
          ("85 %", ["разговорных вопросов", "с верным пунктом"]),
          ("100 %", ["отказов: 20 ловушек", "и 5 попыток взлома"]),
          ("0", ["расхождений после", "выгрузки в Excel"])]
for i, (n, c) in enumerate(totals):
    x = 1660 + 24 + i * 172
    if i:
        add(f'<line x1="{x-14}" y1="1200" x2="{x-14}" y2="1292" stroke="#3a5687" stroke-width="1"/>')
    text(x, 1240, n, 40, "bold", "#ffffff")
    for j, cc in enumerate(c):
        text(x, 1268 + j * 18, cc, 13, color="#b9cdee", maxw=150)

add('</svg>')
with open(OUT, "w", encoding="utf-8") as f:
    f.write("\n".join(out) + "\n")
for w_ in warn:
    print(w_)
print("ok", OUT)
