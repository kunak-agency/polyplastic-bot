"""Тематический маршрут без RAG и тема ответа.

Маршрут (UX-спека, п. 2.5): зарплата конкретного человека, увольнение, конфликт, здоровье,
попытка обойти правила или переписать инструкции → сразу человеку, LLM не вызывается.
Списки намеренно узкие: «Когда зарплата?» — обычный вопрос по регламенту и идёт в RAG.
Regex-триггеры уволь*/взятк*/откат*/ударил* дополнительно живут в Go (backend).
"""

from __future__ import annotations

import re
from dataclasses import dataclass

_I = re.IGNORECASE


@dataclass(frozen=True)
class Route:
    name: str
    topic: str  # hr | legal | security | other


_ROUTES: list[tuple[Route, re.Pattern[str]]] = [
    (Route("prompt_injection", "security"), re.compile(
        r"игнорир\w*\s+(\w+\s+){0,3}(инструкц|правил|регламент|промпт|указани)"
        r"|забудь\w*\s+(\w+\s+){0,3}(инструкц|правил|регламент|промпт|указани)"
        r"|(системн\w*|system)\s*(промпт|prompt)"
        r"|ignore\s+(all\s+|any\s+|the\s+)?(previous|prior|above)"
        r"|теперь\s+ты\s+(не\s+)?\w+|ты\s+больше\s+не\s+(бот|ассистент|корсик)"
        r"|как\s+обойти|обойти\s+(правил|регламент|запрет|пропуск|охран)", _I)),
    (Route("personal_salary", "hr"), re.compile(
        r"сколько\s+(\w+\s+){0,2}(получа|зарабатыва)"
        r"|(зарплат|оклад|преми|доход)\w*\s+(\w+\s+)?(у\s+(?!нас\b)\w+|коллег|начальник|руководител"
        r"|наставник|бригадир|мастер|сосед|друг|других)", _I)),
    (Route("dismissal", "legal"), re.compile(
        r"уволь|увольн|уволи|по\s+собственному|расторж\w*\s+(трудов\w*\s+)?договор", _I)),
    (Route("conflict", "hr"), re.compile(
        r"конфликт|ссор|оскорб|униж|орёт|орет|кричит\s+на|наезжа|травл|буллинг|харассм|домогат|угрожа", _I)),
    (Route("health", "hr"), re.compile(
        r"здоровь|болею|заболел|болит|диагноз|беремен|инвалидн|лечени|депресс|психотерап", _I)),
]


def route_question(question: str) -> Route | None:
    for route, pattern in _ROUTES:
        if pattern.search(question):
            return route
    return None


_LEGAL = re.compile(r"взыскан|дисциплинар|коммерческ\w*\s+тайн|антикорруп|материальн\w*\s+ответствен"
                    r"|штраф|выговор|замечани", _I)
_SECURITY = re.compile(r"пропуск|охран|видеонаблюд|пропускн|режимн|безопасност", _I)


def classify_topic(text: str) -> str:
    """Тема по вопросу и найденному пункту. legal/security отвечаются только цитатой."""
    if _LEGAL.search(text):
        return "legal"
    if _SECURITY.search(text):
        return "security"
    return "hr"
