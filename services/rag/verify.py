"""Код-проверка цитат: правила держит код, а не промпт.

Источник принимается, только если:
- (doc, clause_id) есть среди пунктов найденных чанков;
- quote после нормализации входит в текст именно этого пункта;
- quote не короче MIN_QUOTE_WORDS слов (или равна пункту целиком), иначе «цитата» из
  одного слова «отпуск» формально входит в любой пункт про отпуск.
Номер страницы берётся из метаданных чанка, а не из ответа LLM.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

from chunker import Chunk
from textnorm import is_quote_in, normalize_for_match

MIN_QUOTE_WORDS = 3


@dataclass
class VerifiedSource:
    doc: str
    clause_id: str
    page: int | None
    quote: str


@dataclass
class VerifyResult:
    ok: bool
    sources: list[VerifiedSource]
    errors: list[str]


def verify_sources(raw_sources: list[dict], retrieved: list[Chunk]) -> VerifyResult:
    errors: list[str] = []
    verified: list[VerifiedSource] = []
    if not raw_sources:
        return VerifyResult(ok=False, sources=[], errors=["no_sources"])

    by_key = {}
    for ch in retrieved:
        for cl in ch.clauses:
            by_key[(ch.doc, cl.clause_id)] = cl
    by_clause: dict[str, list] = {}
    for (doc, cid), cl in by_key.items():
        by_clause.setdefault(cid, []).append((doc, cl))

    for i, src in enumerate(raw_sources):
        if not isinstance(src, dict):
            errors.append(f"source[{i}]: не объект")
            continue
        cid = str(src.get("clause_id") or "").strip().rstrip(".")
        doc = str(src.get("doc") or "").strip()
        quote = str(src.get("quote") or "").strip()
        candidates = [(d, cl) for d, cl in by_clause.get(cid, []) if not doc or d == doc]
        if not candidates:
            errors.append(f"source[{i}]: пункт {cid!r} документа {doc!r} не среди найденных")
            continue
        q_norm = normalize_for_match(quote)
        match = None
        for d, cl in candidates:
            if is_quote_in(quote, cl.text) or is_quote_in(quote, f"{cl.clause_id}. {cl.text}"):
                whole = q_norm == normalize_for_match(cl.text)
                if len(q_norm.split()) >= MIN_QUOTE_WORDS or whole:
                    match = (d, cl)
                    break
        if match is None:
            errors.append(f"source[{i}]: цитата не найдена в тексте пункта {cid}")
            continue
        d, cl = match
        verified.append(VerifiedSource(doc=d, clause_id=cl.clause_id, page=cl.page, quote=quote))

    return VerifyResult(ok=not errors and bool(verified), sources=verified, errors=errors)


# --- проверки текста ответа LLM (промпт rag-answer v2, раздел E) ---

MAX_ANSWER_CHARS = 700

# Число: цифры, разделители внутри числа «9:00», «1,5», «6.1». Разделители приводятся к точке,
# чтобы «12.00» и «12:00», «1,5» и «1.5» считались одним числом. Числа словами («три дня»)
# не проверяются — это известная дыра, её закрывает только дословная цитата.
_NUMBER = re.compile(r"\d+(?:[.,:]\d+)*")
_SENTENCE_END = re.compile(r"[.!?…](?=\s|$)")


def _norm_number(raw: str) -> str:
    return ".".join(str(int(part)) for part in re.split(r"[.,:]", raw))  # «09:00» → «9.0»


def numbers_in(text: str, parts: bool = False) -> set[str]:
    """Числа текста. parts=True добавляет и части составных: «9:00» → {«9.0», «9», «0»}, чтобы
    «с 9 утра» в ответе находило «9:00» в пункте. Для ответа parts не включаем."""
    out = set()
    for raw in _NUMBER.findall(text or ""):
        n = _norm_number(raw)
        out.add(n)
        if parts:
            out.update(n.split("."))
    return out


def foreign_numbers(answer: str, sources: list[VerifiedSource], retrieved: list[Chunk]) -> set[str]:
    """Числа ответа, которых нет в тексте процитированных пунктов (и в самих цитатах).

    Разрешены только пункты из sources, а не весь чанк: чанк склеивает соседей («3.1+3.2»),
    и «доплата 40%» к ответу про 3.2 прошла бы из-за «40 часов» в 3.1. Если модель взяла
    число из соседнего пункта, она должна была его процитировать (правило 2 промпта v2).
    Номера процитированных пунктов тоже допустимы. Числа из вопроса — нет: иначе инъекция
    «аванс 1-го числа?» превращается в «факт» из ответа.
    """
    allowed: set[str] = set()
    cited = {(s.doc, s.clause_id) for s in sources}
    for s in sources:
        allowed |= numbers_in(s.quote, parts=True) | numbers_in(s.clause_id)
    for ch in retrieved:
        for cl in ch.clauses:
            if (ch.doc, cl.clause_id) in cited:
                allowed |= numbers_in(cl.text, parts=True)
    return numbers_in(answer) - allowed


def fit_length(answer: str, limit: int = MAX_ANSWER_CHARS) -> str | None:
    """Ответ в пределах limit символов: как есть, обрезанный по концу предложения или None.

    None — даже первое предложение (прямой ответ по промпту v2) не влезает: такой ответ
    не обрезаем посреди фразы, а эскалируем.
    """
    text = answer.strip()
    if len(text) <= limit:
        return text
    cut = None
    for m in _SENTENCE_END.finditer(text):
        if m.end() > limit:
            break
        cut = m.end()
    return text[:cut].strip() if cut else None


# --- форма обращения (промпт rag-answer v3, раздел E) ---
#
# Ищем только слова, которые однозначно значат форму: местоимения и глаголы 2-го лица.
# Глаголы 2 л. мн. ч. на -ете/-ите общим правилом не ловим: так же кончаются существительные
# в предложном падеже («в анкете», «в лимите»). Поэтому для «вы» — список частых глаголов.
# Для «ты» окончание -ешь/-ёшь/-ишь(ся) почти всегда глагол; исключения — в стоп-листе.
_TY_PRONOUNS = ("ты тебе тебя тобой тобою твой твоя твоё твое твои твоего твоей твоему твоим "
                "твоими твоих твою").split()
_TY_IMPERATIVES = ("обратись проходи пройди возьми принеси приноси напиши позвони приходи приди зайди "
                   "сходи уточни подай оформи свяжись получи спроси сообщи предъяви договорись").split()
_TY_VERB = re.compile(r"\w{2,}(?:ешь|ёшь|ишь)(?:ся)?")
_TY_VERB_STOP = {"лишь", "мышь", "тишь", "брешь", "плешь"}
_VY_PRONOUNS = "вы вас вам вами ваш ваша ваше ваши вашего вашей вашему вашим вашими ваших вашу".split()
_VY_VERBS = ("можете сможете будете хотите знаете узнаете узнаёте работаете получите получаете "
             "обратитесь обращайтесь подайте возьмите принесите напишите позвоните приходите придёте "
             "придете найдёте найдете оформите пройдёте пройдете проходите уточните свяжитесь "
             "сообщите предъявите договоритесь").split()
_WORD = re.compile(r"\w+")
# Текст в кавычках — цитата документа или вопроса, а не обращение бота
_QUOTED = re.compile(r"«[^«»]*»|“[^“”]*”|„[^„“”]*[“”]|\"[^\"]*\"")


def wrong_address_form(answer: str, address_form: str | None) -> list[str]:
    """Слова ответа в чужой форме обращения: для vy (и None) — формы «ты», для ty — формы «вы».

    Регистр не важен («Вы» и «вы»). Пустой список — ответ в своей форме или безличный.
    """
    text = _QUOTED.sub(" ", answer or "")
    found = []
    for w in _WORD.findall(text):
        lw = w.lower()
        if address_form == "ty":
            bad = lw in _VY_PRONOUNS or lw in _VY_VERBS
        else:
            bad = (lw in _TY_PRONOUNS or lw in _TY_IMPERATIVES
                   or (lw not in _TY_VERB_STOP and _TY_VERB.fullmatch(lw) is not None))
        if bad:
            found.append(w)
    return found
