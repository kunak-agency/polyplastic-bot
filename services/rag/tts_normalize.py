# Взято из hack-ru-clinik services/tts/normalize.py без изменений по сути (только этот заголовок).
"""Нормализация русского текста перед Silero TTS.

Silero v5 ru не читает цифры (просто пропускает их), поэтому всё числовое
переводим в слова через num2words(lang='ru'). Плюс убираем разметку
(HTML/Markdown/ссылки) и режем текст на предложения для потоковой отдачи.
"""

from __future__ import annotations

import re

from num2words import num2words

MONTHS_GEN = [
    "января", "февраля", "марта", "апреля", "мая", "июня",
    "июля", "августа", "сентября", "октября", "ноября", "декабря",
]
_MONTHS_RE = "|".join(MONTHS_GEN)

# Максимальная длина одного куска, который отдаём в apply_tts.
MAX_CHUNK_CHARS = 250


def _card(n: int | str, **kw) -> str:
    return num2words(int(n), lang="ru", **kw)


def _ord_gen(n: int | str) -> str:
    """Порядковое в родительном падеже: 16 -> «шестнадцатого»."""
    return num2words(int(n), lang="ru", to="ordinal", case="genitive", gender="n")


def _plural(n: int, one: str, few: str, many: str) -> str:
    n = abs(n) % 100
    if 11 <= n <= 14:
        return many
    n %= 10
    if n == 1:
        return one
    if 2 <= n <= 4:
        return few
    return many


def _digits_or_number(s: str) -> str:
    """Число с ведущими нулями читаем по цифрам («007» -> «ноль ноль семь»)."""
    if len(s) > 1 and s.startswith("0"):
        return " ".join(_card(ch) for ch in s)
    return _card(s)


def _time(m: re.Match) -> str:
    h, mm = m.group(1), m.group(2)
    if mm == "00":
        minutes = "ноль ноль"
    elif mm.startswith("0"):
        minutes = "ноль " + _card(mm[1])
    else:
        minutes = _card(mm)
    return f"{_card(h)} {minutes}"


def _date_numeric(m: re.Match) -> str:
    d, mo, y = int(m.group(1)), int(m.group(2)), m.group(3)
    if not (1 <= d <= 31 and 1 <= mo <= 12):
        return m.group(0)
    out = f"{_ord_gen(d)} {MONTHS_GEN[mo - 1]}"
    if y:
        year = int(y) if len(y) == 4 else 2000 + int(y)
        out += f" {_ord_gen(year)} года"
    return out


def _date_words(m: re.Match) -> str:
    return f"{_ord_gen(m.group(1))} {m.group(2)}"


def _percent(m: re.Match) -> str:
    n = int(m.group(1))
    return f"{_card(n)} {_plural(n, 'процент', 'процента', 'процентов')}"


def _decimal(m: re.Match) -> str:
    return num2words(float(f"{m.group(1)}.{m.group(2)}"), lang="ru")


def strip_markup(text: str) -> str:
    text = re.sub(r"<[^>]+>", " ", text)                      # HTML/SSML-теги
    text = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", text)       # [текст](ссылка)
    text = re.sub(r"https?://\S+|www\.\S+", " ", text)         # голые ссылки
    text = re.sub(r"`{1,3}", " ", text)
    text = re.sub(r"^\s*(#{1,6}|>|[-*•]|\d+[.)])\s+", "", text, flags=re.M)  # заголовки/списки
    text = re.sub(r"[*_~#|]+", " ", text)
    return text


def normalize(text: str) -> str:
    text = strip_markup(text)
    text = text.replace(" ", " ")
    text = re.sub(r"№\s*", "номер ", text)
    text = re.sub(r"\+\s*(?=\d)", "плюс ", text)
    # 10:30 -> десять тридцать
    text = re.sub(r"\b([01]?\d|2[0-3]):([0-5]\d)\b", _time, text)
    # 16.10 / 16.10.2026 -> шестнадцатого октября (две тысячи двадцать шестого года)
    text = re.sub(r"\b(\d{1,2})\.(\d{2})(?:\.(\d{4}|\d{2}))?\b", _date_numeric, text)
    # 16 октября -> шестнадцатого октября
    text = re.sub(rf"\b(\d{{1,2}})\s+({_MONTHS_RE})\b", _date_words, text, flags=re.I)
    # 2026 года -> две тысячи двадцать шестого года
    text = re.sub(r"\b(\d{4})\s+(года|г\.)", lambda m: f"{_ord_gen(m.group(1))} года", text)
    # в 2026 году -> в две тысячи двадцать шестом году
    text = re.sub(
        r"\b(\d{4})\s+году\b",
        lambda m: num2words(int(m.group(1)), lang="ru", to="ordinal", case="prepositional", gender="m") + " году",
        text,
    )
    # 50% -> пятьдесят процентов
    text = re.sub(r"\b(\d+)\s*%", _percent, text)
    # 3,5 / 3.5 -> три целых пять десятых
    text = re.sub(r"\b(\d+)[.,](\d+)\b", _decimal, text)
    # Остальные целые числа (телефоны, кабинеты и т.п. читаются группами)
    text = re.sub(r"\d+", lambda m: f" {_digits_or_number(m.group(0))} ", text)
    # Тире/дефисы между словами-числами и прочая пунктуация
    text = re.sub(r"\s+[-–—]\s+", ", ", text)
    text = re.sub(r"\s+([,.!?…:;])", r"\1", text)
    text = re.sub(r"\s+", " ", text)
    return text.strip()


def _split_long(sentence: str) -> list[str]:
    if len(sentence) <= MAX_CHUNK_CHARS:
        return [sentence]
    parts, cur = [], ""
    for piece in re.split(r"(?<=[,;:])\s+|\s+", sentence):
        if cur and len(cur) + 1 + len(piece) > MAX_CHUNK_CHARS:
            parts.append(cur)
            cur = piece
        else:
            cur = f"{cur} {piece}".strip()
    if cur:
        parts.append(cur)
    return parts


def split_sentences(text: str) -> list[str]:
    """Нормализованный текст -> список кусков для синтеза (по предложениям)."""
    out: list[str] = []
    for s in re.split(r"(?<=[.!?…])\s+", text):
        s = s.strip()
        if not re.search(r"[а-яёa-z]", s, flags=re.I):
            continue
        out.extend(_split_long(s))
    return out
