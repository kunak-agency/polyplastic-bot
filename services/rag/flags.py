"""Разметка текста новичка по таксономии F0–F10 (POST /flags) — промпт flags-classify v2.

Промпт не вшит в код: system, шаблон user, JSON-схема и few-shot читаются из
`prompts/flags-classify.md` — копии источника истины `agency/prompts/flags-classify.md`
(в Docker-контекст `services/rag` каталог agency не попадает). Совпадение копии с источником
проверяет `tests/test_flags.py`.

LLM только размечает. Маршрут, порог, эскалацию и видимость решает Go по `data/taxonomy.yaml`.
Здесь — проверки кодом (п. E промпта): код из таксономии, severity 1–3, confidence 0–1 и
не ниже 0.3, quote — дословная подстрока текста, у F10 и при ПДн quote пустой.
v2: tone_confidence (0–1 или None), summary (без телефона, почты, паспорта; ≤ 120 символов; при
F10 пусто), clarify_hint (F1/F2/F3/F4/F6/F8 или None). Нет новых полей в ответе модели — они
пустые, ответ валиден (совместимость с v1).

Карточка T-lite: temperature и presence_penalty в каждом запросе — их ставит build_payload().
LLM не задан, не ответил или дважды ответил не по схеме → пустая разметка с degraded=true:
«не размечено» не должно выглядеть как «флагов нет».
"""

from __future__ import annotations

import functools
import json
import logging
import os
import re
import threading
from dataclasses import dataclass, field
from pathlib import Path

import httpx

from llm import STRUCTURED_MODES, InvalidLLMAnswer
from textnorm import is_quote_in

log = logging.getLogger("rag.flags")

PROMPT_PATH = Path(__file__).resolve().parent / "prompts" / "flags-classify.md"
PROMPT_VERSION = "v2"  # должна совпадать с «Версия: **v2**» в prompts/flags-classify.md

# Таксономия: TAXONOMY_PATH или data/taxonomy.yaml из корня репозитория. В Docker-образе файла
# нет — тогда коды берутся из enum схемы промпта (тест сверяет их с таксономией).
TAXONOMY_CANDIDATES = (
    Path(__file__).resolve().parent.parent.parent / "data" / "taxonomy.yaml",
    Path(__file__).resolve().parent / "taxonomy.yaml",
)

MIN_CONFIDENCE = 0.3   # п. 3 промпта: флаг с уверенностью ниже 0.3 не ставится
MAX_FLAGS = 3
MAX_QUOTE_CHARS = 150
SENTIMENTS = ("neg", "neu", "pos")
CONTEXTS_DEFAULT = "free_text"
NO_QUOTE_CODES = {"F10"}  # личное и здоровье: категория есть, цитаты нет
MAX_SUMMARY_CHARS = 120
# уточняющий вопрос — только для категорий с шаблоном уточнения (ресерч 2026-10-24, #6)
CLARIFY_CODES = {"F1", "F2", "F3", "F4", "F6", "F8"}
# при этих кодах уточнение не задаётся: свои сценарии (помощь, «Передать», без вопросов)
NO_CLARIFY_CODES = {"F0", "F5", "F7", "F9", "F10"}

# ПДн в цитате (п. E): телефон, e-mail, паспорт — цитата обнуляется.
_PII = (
    re.compile(r"(?:\+7|\b8)[\s\-(]*\d{3}[\s\-)]*\d{3}[\s\-]*\d{2}[\s\-]*\d{2}\b"),
    re.compile(r"\b\d{3}[\s\-]\d{2}[\s\-]\d{2}\b"),
    re.compile(r"[\w.+-]+@[\w-]+\.[\w.-]+"),
    re.compile(r"\b\d{2}\s?\d{2}\s?\d{6}\b"),
)


# --- промпт из markdown ---

@dataclass(frozen=True)
class FewShot:
    user: str
    assistant: str


@dataclass(frozen=True)
class FlagsPrompt:
    version: str
    system: str          # с плейсхолдером {context}
    user_template: str   # с плейсхолдерами {context}, {survey_question}, {text}
    schema: dict
    few_shot: tuple[FewShot, ...]

    @property
    def codes(self) -> tuple[str, ...]:
        return tuple(self.schema["properties"]["flags"]["items"]["properties"]["code"]["enum"])


_SECTION = re.compile(r"^## ", re.MULTILINE)
_FENCE_BLOCK = re.compile(r"```(text|json)\n(.*?)\n```", re.DOTALL)
_INLINE = re.compile(r"`([^`]+)`")


def _section(md: str, prefix: str) -> str:
    for part in _SECTION.split(md):
        if part.startswith(prefix):
            return part
    raise ValueError(f"в промпте нет раздела «## {prefix}»")


def _blocks(text: str, lang: str) -> list[str]:
    return [body for kind, body in _FENCE_BLOCK.findall(text) if kind == lang]


def _shot_user(example: str) -> str:
    """Строка `User: `Контекст: …` / `<text>…</text>`` → сообщение user, части через перевод строки."""
    for line in example.splitlines():
        if line.startswith("User:"):
            parts = _INLINE.findall(line)
            if parts:
                return "\n".join(parts)
    raise ValueError(f"few-shot «{example.splitlines()[0]}»: нет строки User")


def parse_prompt_md(md: str) -> FlagsPrompt:
    m = re.search(r"^Версия: \*\*(v\d+)\*\*", md, re.MULTILINE)
    if not m:
        raise ValueError("в промпте нет строки «Версия: **vN**»")
    system = _blocks(_section(md, "A)"), "text")[0]
    user_template = _blocks(_section(md, "B)"), "text")[0]
    schema = json.loads(_blocks(_section(md, "C)"), "json")[0])
    shots = []
    for example in re.split(r"^### ", _section(md, "D)"), flags=re.MULTILINE)[1:]:
        answers = _blocks(example, "json")
        if len(answers) != 1:
            raise ValueError(f"few-shot «{example.splitlines()[0]}»: ожидается один ответ assistant")
        json.loads(answers[0])
        shots.append(FewShot(user=_shot_user(example), assistant=answers[0]))
    for ph in ("{context}", "{survey_question}", "{text}"):
        if ph not in user_template:
            raise ValueError(f"в шаблоне user нет плейсхолдера {ph}")
    return FlagsPrompt(version=m.group(1), system=system, user_template=user_template, schema=schema,
                       few_shot=tuple(shots))


@functools.lru_cache(maxsize=1)
def load_prompt(path: str = str(PROMPT_PATH)) -> FlagsPrompt:
    prompt = parse_prompt_md(Path(path).read_text(encoding="utf-8"))
    if prompt.version != PROMPT_VERSION:
        raise ValueError(f"prompts/flags-classify.md версии {prompt.version}, код ждёт {PROMPT_VERSION}")
    return prompt


def load_taxonomy_codes(path: str | None = None) -> tuple[str, ...] | None:
    """Коды категорий из taxonomy.yaml; None — файла нет (Docker-образ)."""
    candidates = [Path(path)] if path else [Path(p) for p in ([os.getenv("TAXONOMY_PATH")] if os.getenv("TAXONOMY_PATH") else [])]
    candidates += list(TAXONOMY_CANDIDATES)
    for p in candidates:
        if p.is_file():
            import yaml  # в образе идёт зависимостью transformers; без файла не нужен

            data = yaml.safe_load(p.read_text(encoding="utf-8"))
            return tuple(sorted(data["categories"], key=lambda c: int(c[1:])))
    return None


# --- конфиг и запрос ---

@dataclass
class FlagsConfig:
    base_url: str
    api_key: str | None
    model: str
    # п. G промпта: классификация — низкая температура; передаются явно в каждом запросе
    temperature: float = 0.1
    presence_penalty: float = 1.0
    top_p: float = 0.8
    max_tokens: int = 320
    timeout_s: float = 10.0
    structured: str = "json_schema"

    def __post_init__(self):
        if self.structured not in STRUCTURED_MODES:
            raise ValueError(f"LLM_STRUCTURED={self.structured!r}: ожидается {' | '.join(STRUCTURED_MODES)}")

    def __repr__(self) -> str:  # ключ не должен попасть в лог или traceback
        return (f"FlagsConfig(base_url={self.base_url!r}, model={self.model!r}, "
                f"api_key={'***' if self.api_key else None}, structured={self.structured!r})")

    @classmethod
    def from_env(cls) -> "FlagsConfig | None":
        base_url = (os.getenv("LLM_BASE_URL") or "").strip()
        if not base_url:
            return None
        return cls(
            base_url=base_url.rstrip("/"),
            api_key=os.getenv("LLM_API_KEY") or None,
            model=os.getenv("LLM_MODEL", "tlite"),
            temperature=float(os.getenv("FLAGS_TEMPERATURE", "0.1")),
            presence_penalty=float(os.getenv("FLAGS_PRESENCE_PENALTY", os.getenv("LLM_PRESENCE_PENALTY", "1.0"))),
            top_p=float(os.getenv("FLAGS_TOP_P", "0.8")),
            max_tokens=int(os.getenv("FLAGS_MAX_TOKENS", "320")),
            timeout_s=float(os.getenv("FLAGS_TIMEOUT_S", "10")),
            structured=os.getenv("LLM_STRUCTURED", "json_schema"),
        )


def render_user(prompt: FlagsPrompt, text: str, context: str, survey_question: str | None) -> str:
    # закрывающий тег в тексте новичка не должен ломать границу данных
    safe = re.sub(r"</?text>", " ", text, flags=re.IGNORECASE)
    sq = re.sub(r"\s+", " ", survey_question or "").strip()
    lines = []
    for line in prompt.user_template.split("\n"):
        if "{survey_question}" in line and not sq:
            continue  # промпт, п. B: пустой вопрос — строку убрать целиком
        lines.append(line)
    # text подставляется последним, чтобы «{context}» в тексте новичка не подменился
    return ("\n".join(lines).replace("{context}", context).replace("{survey_question}", sq)
            .replace("{text}", safe))


def build_payload(cfg: FlagsConfig, text: str, context: str = CONTEXTS_DEFAULT,
                  survey_question: str | None = None) -> dict:
    prompt = load_prompt()
    messages = [{"role": "system", "content": prompt.system.replace("{context}", context)}]
    for shot in prompt.few_shot:
        messages.append({"role": "user", "content": shot.user})
        messages.append({"role": "assistant", "content": shot.assistant})
    messages.append({"role": "user", "content": render_user(prompt, text, context, survey_question)})
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
        payload["response_format"] = {"type": "json_schema",
                                      "json_schema": {"name": "flags_classify", "schema": prompt.schema}}
    elif cfg.structured == "structured_outputs":
        payload["structured_outputs"] = {"json": prompt.schema}
    return payload


# --- проверки кодом (п. E) ---

@dataclass
class FlagsResult:
    flags: list[dict] = field(default_factory=list)
    sentiment: str = "neu"
    quote: str = ""
    degraded: bool = False
    tone_confidence: float | None = None
    summary: str = ""
    clarify_hint: str | None = None

    def as_dict(self) -> dict:
        return {"flags": self.flags, "sentiment": self.sentiment, "quote": self.quote, "degraded": self.degraded,
                "tone_confidence": self.tone_confidence, "summary": self.summary, "clarify_hint": self.clarify_hint}


def degraded() -> FlagsResult:
    return FlagsResult(degraded=True)


_FENCE = re.compile(r"^```(?:json)?\s*|\s*```$", re.IGNORECASE)


def _has_pii(s: str) -> bool:
    return any(p.search(s) for p in _PII)


def clean_summary(summary, flags: list[dict]) -> str:
    """summary модели → строка для HR: одна строка, без телефона, почты, паспорта, ≤ 120
    символов (по слову, с «…»); при F10 — пусто."""
    if not isinstance(summary, str) or any(f["code"] in NO_QUOTE_CODES for f in flags):
        return ""
    s = summary
    for p in _PII:
        s = p.sub(" ", s)
    s = re.sub(r"\s+", " ", s).strip(" ,;:—-")
    s = re.sub(r"\s+([,.;:!?])", r"\1", s).strip()
    if len(s) > MAX_SUMMARY_CHARS:
        cut = s[: MAX_SUMMARY_CHARS - 1]
        if " " in cut[MAX_SUMMARY_CHARS // 2:]:
            cut = cut[: cut.rfind(" ")]
        s = cut.rstrip(" ,;:—-") + "…"
    return s


def clean_tone_confidence(v) -> float | None:
    if isinstance(v, bool) or not isinstance(v, (int, float)):
        return None
    return round(min(1.0, max(0.0, float(v))), 3)


def clean_clarify_hint(v, flags: list[dict]) -> str | None:
    code = str(v or "").strip().upper()
    if code not in CLARIFY_CODES or any(f["code"] in NO_CLARIFY_CODES for f in flags):
        return None
    return code


def postprocess(content: str, text: str, codes: tuple[str, ...]) -> FlagsResult:
    """Ответ LLM → проверенная разметка. Форма не та → InvalidLLMAnswer (ретрай / degraded)."""
    try:
        data = json.loads(_FENCE.sub("", (content or "").strip()))
    except json.JSONDecodeError as exc:
        raise InvalidLLMAnswer("LLM вернул не JSON") from exc
    if not isinstance(data, dict) or not isinstance(data.get("flags"), list):
        raise InvalidLLMAnswer("нет списка flags")

    by_code: dict[str, dict] = {}
    for f in data["flags"]:
        if not isinstance(f, dict):
            continue
        code = str(f.get("code", "")).strip().upper()
        if code not in codes:
            log.info("flags: код %r не из таксономии — отброшен", code)
            continue
        sev, conf = f.get("severity"), f.get("confidence")
        if isinstance(sev, bool) or not isinstance(sev, (int, float)) or isinstance(conf, bool) \
                or not isinstance(conf, (int, float)):
            continue
        sev = min(3, max(1, int(round(sev))))
        if code == "F0":
            sev = 3  # промпт, п. 2: F0 всегда 3
        conf = round(min(1.0, max(0.0, float(conf))), 3)
        if conf < MIN_CONFIDENCE:
            continue
        prev = by_code.get(code)
        if prev is None or (sev, conf) > (prev["severity"], prev["confidence"]):
            by_code[code] = {"code": code, "severity": sev, "confidence": conf}
    flags = sorted(by_code.values(), key=lambda f: (-f["severity"], -f["confidence"]))[:MAX_FLAGS]

    sentiment = data.get("sentiment")
    if sentiment not in SENTIMENTS:
        sentiment = "neu"

    quote = data.get("quote")
    quote = quote.strip() if isinstance(quote, str) else ""
    if (not flags or any(f["code"] in NO_QUOTE_CODES for f in flags) or len(quote) > MAX_QUOTE_CHARS
            or not is_quote_in(quote, text) or _has_pii(quote)):
        quote = ""
    return FlagsResult(flags=flags, sentiment=sentiment, quote=quote,
                       tone_confidence=clean_tone_confidence(data.get("tone_confidence")),
                       summary=clean_summary(data.get("summary"), flags),
                       clarify_hint=clean_clarify_hint(data.get("clarify_hint"), flags))


# --- клиент ---

class FlagsClassifier:
    def __init__(self, cfg: FlagsConfig | None, transport: httpx.BaseTransport | None = None,
                 codes: tuple[str, ...] | None = None):
        self.cfg = cfg
        prompt = load_prompt()  # битый промпт — ошибка сразу, а не на запросе
        tax = codes or load_taxonomy_codes()
        if tax is None:
            log.warning("flags: data/taxonomy.yaml не найден — коды берутся из схемы промпта")
            tax = prompt.codes
        # код валиден, только если он есть и в таксономии, и в схеме
        self.codes = tuple(c for c in tax if c in prompt.codes)
        self._http = None
        if cfg is not None:
            headers = {"Authorization": f"Bearer {cfg.api_key}"} if cfg.api_key else {}
            self._http = httpx.Client(base_url=cfg.base_url, headers=headers, timeout=cfg.timeout_s,
                                      transport=transport)

    @classmethod
    def from_env(cls) -> "FlagsClassifier":
        return cls(FlagsConfig.from_env())

    def classify(self, text: str, context: str = CONTEXTS_DEFAULT, survey_question: str | None = None) -> FlagsResult:
        text = (text or "").strip()
        if not text:
            return FlagsResult()
        if self._http is None:
            return degraded()
        payload = build_payload(self.cfg, text, context, survey_question)
        for attempt in (1, 2):  # п. E: при ответе не по схеме — один ретрай
            try:
                resp = self._http.post("/chat/completions", json=payload)
                resp.raise_for_status()
                choice = resp.json()["choices"][0]
                if choice.get("finish_reason") == "length":
                    raise InvalidLLMAnswer("ответ обрезан по max_tokens")
                return postprocess(choice["message"]["content"], text, self.codes)
            except InvalidLLMAnswer as exc:
                log.warning("flags: попытка %d — %s", attempt, exc)
            except (httpx.HTTPError, KeyError, IndexError, TypeError, ValueError) as exc:
                # сеть, срок, не 2xx — ретрай не поможет за разумное время
                log.warning("flags: LLM недоступен (%s) — degraded", type(exc).__name__)
                return degraded()
        return degraded()

    def close(self) -> None:
        if self._http is not None:
            self._http.close()


_lock = threading.Lock()
_classifier: FlagsClassifier | None = None


def get_classifier() -> FlagsClassifier:
    """Ленивый синглтон: конфиг LLM читается из env при первом /flags."""
    global _classifier
    with _lock:
        if _classifier is None:
            _classifier = FlagsClassifier.from_env()
        return _classifier


def reset_classifier(c: FlagsClassifier | None = None) -> None:
    """Тесты: подменить классификатор (None — перечитать env при следующем вызове)."""
    global _classifier
    with _lock:
        if _classifier is not None and _classifier is not c:
            _classifier.close()
        _classifier = c
