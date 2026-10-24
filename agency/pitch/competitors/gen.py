# Таблица «12 аналогов × возможности» для слайда: competitors.html → competitors.png (headless Chrome).
# Данные — ресерч 2026-10-23 (блок D) и facts.md §4; строка «Корсик» — по факту прода 24.10.
COLS = ["Канал", "Сценарий от даты выхода", "Конструктор без кода", "Ответы по регламентам",
        "Сигналы риска и тон", "Передача человеку", "Кабинет по ролям", "В MAX, в РФ"]
Y, P, N, Q = "y", "p", "n", "q"
ROWS = [
  ("Поток Адаптация", "своё приложение", [Y, P, N, P, N, Y, N]),
  ("Пульс (Сбер)", "веб, моб.", [Q, Q, N, P, N, Y, N]),
  ("Websoft HCM", "портал", [P, P, P, P, Q, Y, N]),
  ("Mirapolis HCM", "портал", [P, Q, N, N, N, Y, N]),
  ("VK WorkSpace AI", "свой мессенджер", [N, N, Y, N, N, N, N]),
  ("Yva.ai", "опросы", [Y, N, N, Y, P, Y, N]),
  ("«Жилсервис», заказной бот", "MAX", [P, N, N, N, P, N, Y]),
  ("Конструкторы MAX: Botman, Salebot, BotHelp", "MAX, TG, VK", [N, Y, P, N, P, P, Y]),
  ("Enboarder", "SMS, Slack, Teams", [Y, P, P, N, N, Y, N]),
  ("Moveworks", "Teams, Slack", [P, N, Y, N, P, Y, N]),
  ("Leena AI", "чат, голос", [Y, P, Y, Y, Y, Y, N]),
  ("Paradox", "SMS", [Y, N, P, N, Y, Y, N]),
]
KORSIK = ("Корсик", "MAX + кабинет", [Y, Y, Y, Y, Y, Y, Y])
MARK = {Y: ('<i class="d y"></i>', "y", "есть"), P: ('<i class="d p"></i>', "p", "частично или заявлено"),
        N: ('<i class="d n"></i>', "n", "нет"), Q: ('<i class="d q">?</i>', "q", "нет данных")}

def row(name, ch, marks, cls=""):
    tds = "".join(f'<td class="m {MARK[m][1]}">{MARK[m][0]}</td>' for m in marks)
    return f'<tr class="{cls}"><th>{name}</th><td class="ch">{ch}</td>{tds}</tr>'

head = "".join(f"<th>{c}</th>" for c in COLS)
body = "".join(row(*r) for r in ROWS) + row(*KORSIK, cls="us")
legend = "".join(f'<span><b class="{MARK[k][1]}">{MARK[k][0]}</b> {MARK[k][2]}</span>' for k in (Y, P, N, Q))
html = f"""<!doctype html><meta charset="utf-8"><style>
body{{margin:0;padding:48px 56px;background:#fff;font:15px/1.35 -apple-system,'Inter','Segoe UI',sans-serif;color:#111;width:1808px;display:inline-block}}
h1{{font-size:30px;margin:0 0 6px;font-weight:700}}
p.sub{{margin:0 0 24px;color:#555;font-size:17px}}
table{{border-collapse:collapse;width:100%}}
thead th{{font-size:13.5px;font-weight:600;color:#444;text-align:center;padding:0 8px 12px;vertical-align:bottom;border-bottom:1.5px solid #111}}
thead th:first-child,thead th:nth-child(2){{text-align:left}}
tbody th{{text-align:left;font-weight:500;padding:9px 12px 9px 0;white-space:nowrap}}
td{{padding:9px 8px;border-bottom:1px solid #e6e6e6}}
td.ch{{color:#666;font-size:14px;white-space:nowrap}}
td.m{{text-align:center;font-size:20px;line-height:1}}
.d{{display:inline-block;width:14px;height:14px;border-radius:50%;box-sizing:border-box;vertical-align:middle;font-style:normal}}
.d.y{{background:#111}} .d.p{{border:2px solid #111;background:linear-gradient(90deg,#111 50%,transparent 50%)}}
.d.n{{border:1.5px solid #c4c4c4}} .d.q{{width:auto;height:auto;color:#999;font-size:15px;font-weight:600}}
tr.us .d.y{{background:#1d4ed8}}
tr.us th,tr.us td{{background:#f2f6ff;border-top:1.5px solid #111;border-bottom:none;font-weight:700}}
tr.us td.m{{color:#1d4ed8}}
.legend{{display:flex;gap:28px;margin-top:18px;color:#555;font-size:14px}} .legend b{{margin-right:6px}}
.foot{{margin-top:10px;color:#888;font-size:13px}}
</style>
<h1>12 решений: никто не закрывает всё сразу</h1>
<p class="sub">Российские HCM-системы, конструкторы ботов для MAX и западные платформы онбординга</p>
<table><thead><tr><th>Решение</th>{head}</tr></thead><tbody>{body}</tbody></table>
<div class="legend">{legend}</div>
<div class="foot">По открытым источникам: сайты вендоров, обзоры Soware, Sostav, SMMplanner, G2, Workspace.ru (2023–2026). Западные решения недоступны в России.</div>
"""
open("competitors.html", "w").write(html)
