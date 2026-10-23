"""Клиент OpenAI-совместимого LLM (vLLM с T-lite-it-2.1) для шага generate.

Промпт rag-answer v3 не вшит в код: system prompt, шаблон user-сообщения, JSON-схема и
few-shot читаются из `prompts/rag-answer.md`. Это копия источника истины
`agency/prompts/rag-answer.md` (в Docker-контекст `services/rag` каталог agency не попадает).
Совпадение копии с источником по блокам A–D и версии проверяет `tests/test_llm.py`.

Карточка T-lite: temperature и presence_penalty обязательны в каждом запросе. Поэтому
они зашиты в build_payload() и проверяются тестом — забыть их через вызывающий код нельзя.

Structured outputs (`LLM_STRUCTURED`):
  json_schema         — `response_format: {"type": "json_schema", ...}` (OpenAI-формат). В vLLM
                        переводится в `structured_outputs.json` (`structured_outputs_from_response_format`);
  structured_outputs  — vLLM-специфичное поле `structured_outputs: {"json": schema}` в теле запроса
                        (то, что в openai-SDK передают через `extra_body`). `guided_json` удалён в vLLM 0.12;
  none                — схема только в тексте промпта (Ollama и серверы без structured outputs).
"""

from __future__ import annotations

import functools
import json
import logging
import os
import re
from concurrent.futures import ThreadPoolExecutor
from concurrent.futures import TimeoutError as FutureTimeout
from dataclasses import dataclass
from pathlib import Path

import httpx

from chunker import Chunk

log = logging.getLogger("rag.llm")

PROMPT_PATH = Path(__file__).resolve().parent / "prompts" / "rag-answer.md"
PROMPT_VERSION = "v3"  # должна совпадать с «Версия: **v3**» в prompts/rag-answer.md
STRUCTURED_MODES = ("json_schema", "structured_outputs", "none")
ADDRESS_FORMS = {"ty": "ты", "vy": "вы"}  # значения контракта /ask → слово для промпта
DEFAULT_ADDRESS_FORM = "vy"
# {address_words} в system prompt: какие слова значат выбранную форму
ADDRESS_WORDS = {"ty": "ты, тебе, тебя, твой, можешь, обратись",
                 "vy": "вы, вам, вас, ваш, можете, обратитесь"}
# В few-shot слово с формой обращения записано вариантами {ty:твоего|vy:вашего}: модель видит
# примеры только в той форме, что и в system (v2 с примерами вперемешку путал ты/вы).
_FORM_ALT = re.compile(r"\{ty:([^{}|]*)\|vy:([^{}|]*)\}")


# --- промпт из markdown ---

@dataclass(frozen=True)
class FewShot:
    user: str
    assistant: str


@dataclass(frozen=True)
class RagPrompt:
    version: str
    system: str          # с плейсхолдерами {address_form}, {address_words}, {site}
    user_template: str   # с плейсхолдерами {context}, {question}
    schema: dict
    few_shot: tuple[FewShot, ...]  # с вариантами {ty:…|vy:…}, см. apply_address_form


_SECTION = re.compile(r"^## ", re.MULTILINE)
_FENCE_BLOCK = re.compile(r"```(text|json)\n(.*?)\n```", re.DOTALL)


def _section(md: str, prefix: str) -> str:
    """Текст раздела `## {prefix}…` до следующего заголовка второго уровня."""
    for part in _SECTION.split(md):
        if part.startswith(prefix):
            return part
    raise ValueError(f"в промпте нет раздела «## {prefix}»")


def _blocks(text: str, lang: str) -> list[str]:
    return [body for kind, body in _FENCE_BLOCK.findall(text) if kind == lang]


def parse_prompt_md(md: str) -> RagPrompt:
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
        for form in ADDRESS_FORMS:  # пример ответа обязан быть валидным JSON в каждой форме
            json.loads(apply_address_form(answers[0], form))
        shots.append(FewShot(user=users[0], assistant=answers[0]))
    for ph in ("{address_form}", "{address_words}", "{site}"):
        if ph not in system:
            raise ValueError(f"в system prompt нет плейсхолдера {ph}")
    for ph in ("{context}", "{question}"):
        if ph not in user_template:
            raise ValueError(f"в шаблоне user нет плейсхолдера {ph}")
    return RagPrompt(version=m.group(1), system=system, user_template=user_template, schema=schema,
                     few_shot=tuple(shots))


@functools.lru_cache(maxsize=1)
def load_prompt(path: str = str(PROMPT_PATH)) -> RagPrompt:
    prompt = parse_prompt_md(Path(path).read_text(encoding="utf-8"))
    if prompt.version != PROMPT_VERSION:
        raise ValueError(f"prompts/rag-answer.md версии {prompt.version}, код ждёт {PROMPT_VERSION}")
    return prompt


def answer_schema() -> dict:
    return load_prompt().schema


# --- конфиг и запрос ---

@dataclass
class LLMConfig:
    base_url: str
    api_key: str | None
    model: str
    temperature: float = 0.2
    presence_penalty: float = 1.0
    top_p: float = 0.8
    max_tokens: int = 768
    # Бюджет на весь вызов LLM, с. Бот ждёт /ask 8 с; не уложились — дословная цитата (fallback:timeout).
    timeout_s: float = 4.0
    structured: str = "json_schema"  # json_schema | structured_outputs | none

    def __post_init__(self):
        if self.structured not in STRUCTURED_MODES:
            raise ValueError(f"LLM_STRUCTURED={self.structured!r}: ожидается {' | '.join(STRUCTURED_MODES)}")

    def __repr__(self) -> str:  # ключ не должен попасть в лог или traceback
        return (f"LLMConfig(base_url={self.base_url!r}, model={self.model!r}, api_key={'***' if self.api_key else None}, "
                f"structured={self.structured!r}, presence_penalty={self.presence_penalty})")

    @classmethod
    def from_env(cls) -> "LLMConfig | None":
        base_url = (os.getenv("LLM_BASE_URL") or "").strip()
        if not base_url:
            return None
        return cls(
            base_url=base_url.rstrip("/"),
            api_key=os.getenv("LLM_API_KEY") or None,
            model=os.getenv("LLM_MODEL", "tlite"),
            temperature=float(os.getenv("LLM_TEMPERATURE", "0.2")),
            presence_penalty=float(os.getenv("LLM_PRESENCE_PENALTY", "1.0")),
            top_p=float(os.getenv("LLM_TOP_P", "0.8")),
            max_tokens=int(os.getenv("LLM_MAX_TOKENS", "768")),
            # LLM_TIMEOUT — бюджет вызова; LLM_TIMEOUT_S — прежнее имя, читается для совместимости
            timeout_s=float(os.getenv("LLM_TIMEOUT") or os.getenv("LLM_TIMEOUT_S") or "4"),
            structured=os.getenv("LLM_STRUCTURED", "json_schema"),
        )


def _attr(value: str) -> str:
    return value.replace("&", "&amp;").replace('"', "&quot;").replace("<", "&lt;").replace(">", "&gt;")


def render_context(chunks: list[Chunk]) -> str:
    parts = []
    for ch in chunks:
        for cl in ch.clauses:
            section = f' section="{_attr(ch.section)}"' if ch.section else ""
            # закрывающий тег внутри текста (загруженный документ) не должен ломать границу данных
            text = cl.text.replace("</doc>", "&lt;/doc&gt;")
            parts.append(f'<doc name="{_attr(ch.doc)}" clause_id="{_attr(cl.clause_id)}"{section}>\n{text}\n</doc>')
    return "\n".join(parts)


def address_form_code(address_form: str | None) -> str:
    """ty | vy; None и неизвестное значение — форма по умолчанию («вы»)."""
    return address_form if address_form in ADDRESS_FORMS else DEFAULT_ADDRESS_FORM


def apply_address_form(text: str, address_form: str | None) -> str:
    """{ty:твоего|vy:вашего} → вариант выбранной формы."""
    idx = 1 if address_form_code(address_form) == "ty" else 2
    return _FORM_ALT.sub(lambda m: m.group(idx), text)


def render_system(prompt: RagPrompt, address_form: str | None, site: str | None) -> str:
    code = address_form_code(address_form)
    # replace, а не str.format: в промпте есть фигурные скобки JSON-формата
    return (prompt.system.replace("{address_form}", ADDRESS_FORMS[code])
            .replace("{address_words}", ADDRESS_WORDS[code])
            .replace("{site}", site or "не указана"))


def render_few_shot(prompt: RagPrompt, address_form: str | None) -> list[dict]:
    messages = []
    for shot in prompt.few_shot:
        messages.append({"role": "user", "content": apply_address_form(shot.user, address_form)})
        messages.append({"role": "assistant", "content": apply_address_form(shot.assistant, address_form)})
    return messages


def render_user(prompt: RagPrompt, question: str, chunks: list[Chunk]) -> str:
    q = re.sub(r"</?question>", " ", question, flags=re.IGNORECASE)
    # context подставляется последним, чтобы «{question}» в тексте документа не подменился
    return prompt.user_template.replace("{question}", q).replace("{context}", render_context(chunks))


def build_payload(cfg: LLMConfig, question: str, chunks: list[Chunk], *,
                  address_form: str | None = None, site: str | None = None) -> dict:
    prompt = load_prompt()
    messages = [{"role": "system", "content": render_system(prompt, address_form, site)}]
    messages += render_few_shot(prompt, address_form)
    messages.append({"role": "user", "content": render_user(prompt, question, chunks)})
    payload: dict = {
        "model": cfg.model,
        "messages": messages,
        # обязательны в каждом запросе (карточка T-lite-it-2.1)
        "temperature": cfg.temperature,
        "presence_penalty": cfg.presence_penalty,
        "top_p": cfg.top_p,
        "max_tokens": cfg.max_tokens,
    }
    if cfg.structured == "json_schema":
        payload["response_format"] = {
            "type": "json_schema",
            "json_schema": {"name": "rag_answer", "schema": prompt.schema},
        }
    elif cfg.structured == "structured_outputs":
        payload["structured_outputs"] = {"json": prompt.schema}
    return payload


# --- разбор ответа ---

_FENCE = re.compile(r"^```(?:json)?\s*|\s*```$", re.IGNORECASE)
_CONFIDENCE = ("high", "medium", "low")


class InvalidLLMAnswer(ValueError):
    """Ответ LLM не JSON или не по схеме — вызывающий код уходит в экстрактивный режим."""


def parse_answer(content: str) -> dict:
    try:
        data = json.loads(_FENCE.sub("", (content or "").strip()))
    except json.JSONDecodeError as exc:
        raise InvalidLLMAnswer("LLM вернул не JSON") from exc
    if not isinstance(data, dict):
        raise InvalidLLMAnswer("LLM вернул не объект")
    # Проверка формы по схеме v2/v3 (structured outputs может быть выключен или не сработать)
    if not isinstance(data.get("sources"), list) or not all(isinstance(s, dict) for s in data["sources"]):
        raise InvalidLLMAnswer("sources не список объектов")
    if not (data.get("answer") is None or isinstance(data.get("answer"), str)):
        raise InvalidLLMAnswer("answer не строка и не null")
    if not isinstance(data.get("needs_escalation"), bool):
        raise InvalidLLMAnswer("needs_escalation не boolean")
    if data.get("confidence") not in _CONFIDENCE:
        raise InvalidLLMAnswer("confidence вне high|medium|low")
    return data


class LLMTimeout(TimeoutError):
    """LLM не уложился в бюджет LLM_TIMEOUT: конвейер отдаёт дословную цитату (fallback:timeout)."""


class LLMClient:
    def __init__(self, cfg: LLMConfig, transport: httpx.BaseTransport | None = None):
        self.cfg = cfg
        load_prompt()  # битый или устаревший prompts/rag-answer.md — ошибка на старте, а не на запросе
        headers = {"Authorization": f"Bearer {cfg.api_key}"} if cfg.api_key else {}
        self._http = httpx.Client(base_url=cfg.base_url, headers=headers, timeout=cfg.timeout_s,
                                  transport=transport)
        # Таймауты httpx — на каждую фазу (connect, read…), а не на весь запрос. Общий бюджет
        # держим ожиданием future; опоздавший запрос дорабатывает в фоне и обрывается таймаутом httpx.
        self._pool = ThreadPoolExecutor(max_workers=8, thread_name_prefix="llm")

    def _post(self, payload: dict) -> httpx.Response:
        try:
            return self._http.post("/chat/completions", json=payload)
        except httpx.TimeoutException as exc:
            raise LLMTimeout(f"LLM: {type(exc).__name__}") from exc

    def generate(self, question: str, chunks: list[Chunk], *, address_form: str | None = None,
                 site: str | None = None) -> dict:
        payload = build_payload(self.cfg, question, chunks, address_form=address_form, site=site)
        future = self._pool.submit(self._post, payload)
        try:
            resp = future.result(timeout=self.cfg.timeout_s)
        except FutureTimeout as exc:
            future.cancel()
            raise LLMTimeout(f"LLM не ответил за {self.cfg.timeout_s:g} с") from exc
        resp.raise_for_status()
        choice = resp.json()["choices"][0]
        if choice.get("finish_reason") == "length":
            raise InvalidLLMAnswer("ответ обрезан по max_tokens")
        return parse_answer(choice["message"]["content"])

    def close(self) -> None:
        self._pool.shutdown(wait=False, cancel_futures=True)
        self._http.close()
