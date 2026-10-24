"""Переформулировка вопроса LLM при провале gate (промпт `prompts/query-rewrite.md`).

Разговорный вопрос («проспал», «откат», «кусками по неделе») реранкер скорит как ловушку.
Модель переписывает его в 1–3 формулировки языком регламента, конвейер повторяет по ним
retrieve + rerank и берёт лучший скор. Gate, генерация и проверка цитат — на исходный
вопрос, так что формулировка не может «ответить» ловушку сама по себе.

Промпт — копия источника истины `agency/prompts/query-rewrite.md`; совпадение по блокам A–D
проверяет `tests/test_rewrite.py`.
"""

from __future__ import annotations

import functools
import json
import logging
import re
from dataclasses import dataclass
from pathlib import Path

from llm import LLMClient, LLMConfig, _FENCE, _blocks, _section
from routing import route_question

log = logging.getLogger("rag.rewrite")

PROMPT_PATH = Path(__file__).resolve().parent / "prompts" / "query-rewrite.md"
PROMPT_VERSION = "v1"
MAX_TOKENS = 160
MAX_QUERY_CHARS = 200
_NUM = re.compile(r"\d+")


@dataclass(frozen=True)
class RewritePrompt:
    version: str
    system: str
    user_template: str
    schema: dict
    few_shot: tuple[tuple[str, str], ...]  # (user, assistant)


def parse_prompt_md(md: str) -> RewritePrompt:
    m = re.search(r"^Версия: \*\*(v\d+)\*\*", md, re.MULTILINE)
    if not m:
        raise ValueError("в промпте нет строки «Версия: **vN**»")
    system = _blocks(_section(md, "A)"), "text")[0]
    user_template = _blocks(_section(md, "B)"), "text")[0]
    schema = json.loads(_blocks(_section(md, "C)"), "json")[0])
    shots = []
    for example in re.split(r"^### ", _section(md, "D)"), flags=re.MULTILINE)[1:]:
        users, answers = _blocks(example, "text"), _blocks(example, "json")
        if len(users) != 1 or len(answers) != 1:
            raise ValueError(f"few-shot «{example.splitlines()[0]}»: ожидается один user и один assistant")
        json.loads(answers[0])
        shots.append((users[0], answers[0]))
    if "{question}" not in user_template:
        raise ValueError("в шаблоне user нет плейсхолдера {question}")
    return RewritePrompt(version=m.group(1), system=system, user_template=user_template, schema=schema,
                         few_shot=tuple(shots))


@functools.lru_cache(maxsize=1)
def load_prompt(path: str = str(PROMPT_PATH)) -> RewritePrompt:
    prompt = parse_prompt_md(Path(path).read_text(encoding="utf-8"))
    if prompt.version != PROMPT_VERSION:
        raise ValueError(f"prompts/query-rewrite.md версии {prompt.version}, код ждёт {PROMPT_VERSION}")
    return prompt


def build_payload(cfg: LLMConfig, question: str) -> dict:
    prompt = load_prompt()
    q = re.sub(r"</?question>", " ", question, flags=re.IGNORECASE)
    messages = [{"role": "system", "content": prompt.system}]
    for user, assistant in prompt.few_shot:
        messages += [{"role": "user", "content": user}, {"role": "assistant", "content": assistant}]
    messages.append({"role": "user", "content": prompt.user_template.replace("{question}", q)})
    payload: dict = {
        "model": cfg.model,
        "messages": messages,
        # обязательны в каждом запросе (карточка T-lite-it-2.1)
        "temperature": cfg.temperature,
        "presence_penalty": cfg.presence_penalty,
        "top_p": cfg.top_p,
        "max_tokens": MAX_TOKENS,
    }
    if cfg.structured == "json_schema":
        payload["response_format"] = {"type": "json_schema",
                                      "json_schema": {"name": "query_rewrite", "schema": prompt.schema}}
    elif cfg.structured == "structured_outputs":
        payload["structured_outputs"] = {"json": prompt.schema}
    return payload


def parse_queries(content: str) -> list[str]:
    try:
        data = json.loads(_FENCE.sub("", (content or "").strip()))
    except json.JSONDecodeError:
        return []
    queries = data.get("queries") if isinstance(data, dict) else None
    if not isinstance(queries, list):
        return []
    return [q.strip() for q in queries if isinstance(q, str) and q.strip()]


def safe_rewrite(rewrite: str, question: str) -> bool:
    """Формулировка без новых чисел, не копия вопроса, не маршрут «сразу человеку», не длинная."""
    if not rewrite or len(rewrite) > MAX_QUERY_CHARS:
        return False
    if rewrite.strip().lower() == question.strip().lower():
        return False
    if set(_NUM.findall(rewrite)) - set(_NUM.findall(question)):
        return False
    return route_question(rewrite) is None


def clean_rewrites(rewrites: list[str], question: str, limit: int) -> list[str]:
    out: list[str] = []
    for r in rewrites:
        if safe_rewrite(r, question) and r.lower() not in (x.lower() for x in out):
            out.append(r)
    return out[:limit]


class QueryRewriter:
    """LLM-переформулировка с бюджетом времени. Любая ошибка → пустой список (отказ по gate)."""

    def __init__(self, client: LLMClient, timeout_s: float = 3.0, max_queries: int = 3):
        load_prompt()  # битый промпт — ошибка на старте, а не на запросе
        self.client = client
        self.timeout_s = timeout_s
        self.max_queries = max_queries

    def rewrite(self, question: str) -> list[str]:
        payload = build_payload(self.client.cfg, question)
        try:
            content = self.client.complete(payload, timeout_s=self.timeout_s)
        except Exception as exc:  # noqa: BLE001 — таймаут, сеть, HTTP: просто без переформулировок
            log.warning("rewrite failed: %s", "timeout" if isinstance(exc, TimeoutError) else type(exc).__name__)
            return []
        return clean_rewrites(parse_queries(content), question, self.max_queries)
