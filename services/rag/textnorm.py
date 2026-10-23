"""Нормализация текста: сравнение цитат и токены для BM25."""

from __future__ import annotations

import functools
import logging
import re

log = logging.getLogger("rag.textnorm")

# Латинские омоглифы → кириллица (типичный артефакт OCR). Применяется к обеим сторонам
# сравнения, поэтому проверка «цитата ⊂ чанк» остаётся корректной.
_HOMOGLYPHS = str.maketrans({
    "a": "а", "e": "е", "o": "о", "p": "р", "c": "с", "x": "х", "y": "у",
    "k": "к", "m": "м", "t": "т", "h": "н", "b": "в",
})
_NON_WORD = re.compile(r"[^\w\s]+", re.UNICODE)
_SPACES = re.compile(r"\s+")


def normalize_for_match(text: str) -> str:
    """Нижний регистр, ё→е, омоглифы, без пунктуации, схлопнутые пробелы.

    Пунктуация выкидывается целиком: LLM часто меняет кавычки, тире и точки в конце
    цитаты, а смысловой подмены через пунктуацию в регламентах почти не бывает.
    Точки внутри чисел («9:00», «6.7.8») тоже уходят, но одинаково с обеих сторон.
    """
    t = text.lower().replace("ё", "е").translate(_HOMOGLYPHS)
    t = _NON_WORD.sub(" ", t)
    return _SPACES.sub(" ", t).strip()


def is_quote_in(quote: str, text: str) -> bool:
    q = normalize_for_match(quote)
    return bool(q) and q in normalize_for_match(text)


# --- токенизация для BM25 ---

_CLAUSE_TOKEN = re.compile(r"\d+(?:\.\d+){1,3}")  # номера пунктов — отдельные токены
_WORD = re.compile(r"[a-zа-яё0-9]+", re.IGNORECASE)

# Короткий стоп-лист: служебные слова и вопросительные обороты. Существительные не трогаем.
STOPWORDS = frozenset("""
а и или но да нет не ни ли же бы вот это этот эта эти тот та те то так как какой какая какие
каков что чтобы кто где когда куда откуда почему зачем сколько можно нужно надо ли
в во на по к ко с со у о об от до из за под над при про для без через между
я мы ты вы он она оно они меня мне мной нас нам нами тебя тебе вас вам его ее её их им ими
мой моя мое моё мои наш наша наше наши свой своя свое своё свои
быть есть был была было были будет будут бывает являться
ещё еще уже только тоже также весь вся все всё всех очень сам сама
скажи подскажи подскажите расскажи расскажите пожалуйста здравствуйте привет
после перед около вокруг
пойти идти сходить взять брать получить получать сделать делать мочь хотеть знать узнать
положить положенный полагаться говорить сказать пункт раздел п
""".split())
# Общие глаголы («получить», «положено») убраны, потому что в gate они считаются
# словами вопроса и, не найдясь в пункте, занижают покрытие (и наоборот, совпадают
# с посторонними пунктами). Стоп-лист применяется и к вопросу, и к документам.

# Разговорные синонимы: если в документе есть все леммы правой части, к токенам
# документа добавляется левая. Так «зарплата» находит «заработная плата». Ключи — леммы.
DOC_SYNONYMS: dict[str, tuple[str, ...]] = {
    "зарплата": ("заработный", "плата"),
    "зп": ("заработный", "плата"),
    "спецовка": ("спецодежда",),
    "отпускной": ("отпуск",),
}


def expand_doc_tokens(tokens: list[str]) -> list[str]:
    present = set(tokens)
    extra = [syn for syn, need in DOC_SYNONYMS.items() if syn not in present and all(n in present for n in need)]
    return tokens + extra


@functools.lru_cache(maxsize=1)
def _morph():
    try:
        import pymorphy3  # noqa: PLC0415
        return pymorphy3.MorphAnalyzer()
    except Exception:  # словарь не установился — работаем без лемм, но громко
        log.warning("pymorphy3 недоступен, BM25 работает без лемматизации")
        return None


@functools.lru_cache(maxsize=50_000)
def _lemma(word: str) -> str:
    m = _morph()
    lemma = m.parse(word)[0].normal_form if m else word
    # «выплачивается» → «выплачиваться» → «выплачивать»: вопрос и регламент часто
    # расходятся только залогом глагола
    if lemma.endswith(("ться", "чься", "тись")) and len(lemma) > 6:
        lemma = lemma[:-2]
    return lemma


def tokenize(text: str) -> list[str]:
    """Токены для BM25: номера пунктов как есть + леммы слов без стоп-слов."""
    low = text.lower().replace("ё", "е")
    tokens = _CLAUSE_TOKEN.findall(low)
    rest = _CLAUSE_TOKEN.sub(" ", low)
    for w in _WORD.findall(rest):
        if w in STOPWORDS:
            continue
        lemma = _lemma(w).replace("ё", "е")
        if lemma in STOPWORDS:
            continue
        tokens.append(lemma)
    return tokens
