"""Расширение вопроса словарём разговорной лексики (`synonyms.yaml`), без LLM.

«Проспал — кому сообщить?» и пункт про невыход на работу не делят ни одного слова: BM25 их
не сводит, а реранкер скорит такую пару как ловушку. Словарь дописывает к вопросу
формулировки «языком регламента». Расширение не добавляет фактов: только термины, которые
уже есть в вопросе другими словами.

Совпадение триггера — по леммам подряд, без стоп-листа: «за свой счёт», «проспала»,
«покурить» в любой форме.
"""

from __future__ import annotations

import functools
import logging
import re
from dataclasses import dataclass
from pathlib import Path

import yaml

from textnorm import _lemma

log = logging.getLogger("rag.expand")

SYNONYMS_PATH = Path(__file__).resolve().parent / "synonyms.yaml"
_WORD = re.compile(r"[a-zа-яё0-9]+", re.IGNORECASE)


def lemmas(text: str) -> tuple[str, ...]:
    """Все слова текста леммами, без стоп-листа (в отличие от textnorm.tokenize)."""
    return tuple(_lemma(w).replace("ё", "е") for w in _WORD.findall(text.lower().replace("ё", "е")))


@dataclass(frozen=True)
class Group:
    triggers: tuple[tuple[str, ...], ...]  # каждая фраза — леммы подряд
    expand: tuple[str, ...]


def _contains(seq: tuple[str, ...], sub: tuple[str, ...]) -> bool:
    n = len(sub)
    return any(seq[i:i + n] == sub for i in range(len(seq) - n + 1))


class QueryExpander:
    def __init__(self, groups: list[Group]):
        self.groups = groups

    @classmethod
    def from_yaml(cls, path: str | Path = SYNONYMS_PATH) -> "QueryExpander":
        data = yaml.safe_load(Path(path).read_text(encoding="utf-8")) or {}
        groups = []
        for i, g in enumerate(data.get("groups") or []):
            trig = tuple(t for t in (lemmas(str(x)) for x in g.get("triggers") or []) if t)
            exp = tuple(str(x).strip() for x in g.get("expand") or [] if str(x).strip())
            if not trig or not exp:
                raise ValueError(f"synonyms.yaml: группа {i} без triggers или expand")
            groups.append(Group(triggers=trig, expand=exp))
        log.info("synonyms: %d групп", len(groups))
        return cls(groups)

    def expansions(self, question: str) -> list[str]:
        """Формулировки для сработавших групп, без повторов и без того, что уже есть в вопросе."""
        q = lemmas(question)
        out: list[str] = []
        for g in self.groups:
            if any(_contains(q, t) for t in g.triggers):
                out += [e for e in g.expand if e not in out and not _contains(q, lemmas(e))]
        return out

    def expand(self, question: str) -> str | None:
        """Вопрос + формулировки регламента; None, если ни один триггер не сработал."""
        extra = self.expansions(question)
        return f"{question.rstrip()} ({'; '.join(extra)})" if extra else None


@functools.lru_cache(maxsize=1)
def default_expander() -> QueryExpander:
    return QueryExpander.from_yaml()
