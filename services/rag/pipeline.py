"""Конвейер /ask: route → retrieve → rerank → gate → generate → verify → answer checks.

Каждый шаг — отдельный объект/функция, их можно менять независимо (dense, реранкер, судья).
"""

from __future__ import annotations

import logging
import os
import time
from dataclasses import dataclass, field
from typing import Protocol

from chunker import Chunk
from llm import LLMClient, LLMConfig
from retrieval import (BM25Retriever, CrossEncoderReranker, DenseRetriever, Encoder, Hit, HybridRetriever,
                       NoopReranker, PairScorer, Reranker, Retriever)
from routing import classify_topic, route_question
from textnorm import tokenize
from verify import (MAX_ANSWER_CHARS, VerifiedSource, fit_length, foreign_numbers, verify_sources,
                    wrong_address_form)

log = logging.getLogger("rag.pipeline")

# Пороги gate: покрытие вопроса по IDF (BM25-режим) и sigmoid реранкера по лучшему пункту.
# Реранкер: стартовый 0.3 из ресерча снижен до 0.1 калибровкой на eval/golden.csv — у всех
# ловушек sigmoid ≤ 0.039, у 0.1 запас ~1 логит над худшей ловушкой (README, «Оценка»).
DEFAULT_GATE = {"lexical": 0.5, "reranker": 0.1}
RRF_K = 60
FUSE_TOP = 20  # кандидатов после RRF
RERANK_TOP = 20  # из них → в реранкер (RAG_RERANK_TOP); README, «Скорость»
WARMUP_QUESTION = "Как устроен первый рабочий день?"


@dataclass
class Settings:
    corpus_dir: str
    retriever: str = "bm25"        # bm25 | hybrid
    reranker: str = "none"         # none | bge
    gate_threshold: float = 0.5
    top_k: int = 5
    dense_model: str = "BAAI/bge-m3"
    reranker_model: str = "BAAI/bge-reranker-v2-m3"
    rerank_top: int = RERANK_TOP   # сколько чанков после RRF скорит реранкер

    @classmethod
    def from_env(cls) -> "Settings":
        reranker = os.getenv("RAG_RERANKER", "none")
        default_gate = DEFAULT_GATE["reranker" if reranker != "none" else "lexical"]
        return cls(
            corpus_dir=os.getenv("RAG_CORPUS_DIR", "testdata/corpus"),
            retriever=os.getenv("RAG_RETRIEVER", "bm25"),
            reranker=reranker,
            gate_threshold=float(os.getenv("RAG_GATE_THRESHOLD") or default_gate),
            top_k=int(os.getenv("RAG_TOP_K", "5")),
            dense_model=os.getenv("RAG_DENSE_MODEL", "BAAI/bge-m3"),
            reranker_model=os.getenv("RAG_RERANKER_MODEL", "BAAI/bge-reranker-v2-m3"),
            rerank_top=int(os.getenv("RAG_RERANK_TOP") or RERANK_TOP),
        )


@dataclass
class Source:
    doc: str
    clause_id: str
    page: int | None
    quote: str


@dataclass
class Answer:
    answer: str | None
    sources: list[Source] = field(default_factory=list)
    confidence: str = "low"
    needs_escalation: bool = True
    topic: str = "other"
    abstain_reason: str | None = None
    # Диагностика для eval и логов, в /ask не уходит.
    # mode: extractive | llm | extractive_fallback | None (до генерации не дошли)
    # check: ok | truncated | declined | verify_failed | foreign_numbers | wrong_address_form | too_long |
    #        fallback:<ошибка>
    mode: str | None = None
    check: str | None = None

    @classmethod
    def abstain(cls, reason: str, topic: str = "other", mode: str | None = None,
                check: str | None = None) -> "Answer":
        return cls(answer=None, sources=[], confidence="low", needs_escalation=True,
                   topic=topic, abstain_reason=reason, mode=mode, check=check)


class Generator(Protocol):
    def generate(self, question: str, chunks: list[Chunk], *, address_form: str | None = None,
                 site: str | None = None) -> dict: ...


def _best_clause(question: str, chunk: Chunk):
    """Пункт склеенного чанка, лучше всего покрывающий слова вопроса."""
    q = set(tokenize(question))
    return max(chunk.clauses, key=lambda cl: len(q & set(tokenize(cl.text))))


def extractive_answer(question: str, hits: list[Hit]) -> dict:
    """Ответ без LLM: дословная цитата лучшего пункта. Формат тот же, что у LLM."""
    best = max(hits, key=lambda h: (h.gate_score, h.score))
    clause = (best.chunk.clause(best.clause_id) if best.clause_id else None) or _best_clause(question, best.chunk)
    return {
        "answer": clause.text,
        "sources": [{"doc": best.chunk.doc, "clause_id": clause.clause_id, "quote": clause.text}],
        "confidence": "medium",
        "needs_escalation": False,
    }


def _confidence(gate_score: float, threshold: float, llm_conf: str | None) -> str:
    by_gate = "high" if gate_score >= min(1.0, threshold + 0.3) else "medium"
    if llm_conf in ("low", "medium") and by_gate == "high":
        return llm_conf
    if llm_conf == "low":
        return "low"
    return by_gate


class Pipeline:
    def __init__(self, settings: Settings, chunks: list[Chunk],
                 generator: Generator | None = None, retriever: Retriever | None = None,
                 reranker: Reranker | None = None, encoder: Encoder | None = None,
                 pair_scorer: PairScorer | None = None):
        """encoder / pair_scorer — готовые модели (или заглушки в тестах) вместо загрузки по имени."""
        self.settings = settings
        self.chunks = chunks
        self._encoder = encoder
        self._pair_scorer = pair_scorer
        self._dense: DenseRetriever | None = None
        self._custom_retriever = retriever is not None
        self.retriever = retriever or self._make_retriever(chunks)
        self.reranker = reranker or self._make_reranker()
        self.generator = generator

    def _make_retriever(self, chunks: list[Chunk]) -> Retriever:
        bm25 = BM25Retriever(chunks)
        if self.settings.retriever == "bm25":
            return bm25
        if self.settings.retriever == "hybrid":
            if self._dense is None:
                self._dense = DenseRetriever(chunks, self.settings.dense_model, encoder=self._encoder)
            else:
                self._dense = self._dense.with_chunks(chunks)
            return HybridRetriever([bm25, self._dense], rrf_k=RRF_K, fuse_top=FUSE_TOP)
        raise ValueError(f"RAG_RETRIEVER={self.settings.retriever!r}: ожидается bm25|hybrid")

    def _make_reranker(self) -> Reranker:
        if self.settings.reranker == "none":
            return NoopReranker()
        if self.settings.reranker == "bge":
            return CrossEncoderReranker(self.settings.reranker_model, model=self._pair_scorer,
                                        max_candidates=self.settings.rerank_top)
        raise ValueError(f"RAG_RERANKER={self.settings.reranker!r}: ожидается none|bge")

    def replace_chunks(self, chunks: list[Chunk]) -> None:
        """Пересобирает индекс (после загрузки документа). Модели не перезагружаются,
        эмбеддинги старых чанков берутся из кэша. Новый ретривер подменяется одной
        операцией присваивания: идущие запросы дорабатывают на старом."""
        if self._custom_retriever:
            raise RuntimeError("ретривер передан снаружи, пересобрать индекс нельзя")
        retriever = self._make_retriever(chunks)
        self.chunks, self.retriever = chunks, retriever

    @classmethod
    def from_env(cls, chunks: list[Chunk]) -> "Pipeline":
        cfg = LLMConfig.from_env()
        return cls(Settings.from_env(), chunks, generator=LLMClient(cfg) if cfg else None)

    def clear_caches(self) -> None:
        """Сбрасывает LRU эмбеддингов вопросов и скоров реранкера (eval меряет холодный вопрос)."""
        if self._dense is not None:
            self._dense.query_cache.clear()
        cache = getattr(self.reranker, "cache", None)
        if cache is not None:
            cache.clear()

    def warmup(self, llm: bool = True) -> dict[str, float]:
        """Прогрев на старте: первый прогон моделей (аллокации, ядра mps) и первый запрос к LLM
        (соединение, префикс промпта в кэше vLLM). Ошибки не роняют старт. → время шагов, мс."""
        times: dict[str, float] = {}
        t0 = time.perf_counter()
        hits: list[Hit] = []
        try:
            hits = self.retrieve(WARMUP_QUESTION)
            self.retrieve(WARMUP_QUESTION + " И когда обед?")  # второй прогон: другая длина батча
        except Exception as exc:  # noqa: BLE001
            log.warning("warmup retrieve failed: %s", type(exc).__name__)
        times["retrieve_ms"] = (time.perf_counter() - t0) * 1000
        if llm and self.generator is not None:
            t1 = time.perf_counter()
            try:
                self.generator.generate(WARMUP_QUESTION, [h.chunk for h in hits] or self.chunks[:1])
            except Exception as exc:  # noqa: BLE001 — LLM недоступен: ответы пойдут fallback'ом
                log.warning("warmup llm failed: %s", type(exc).__name__)
            times["llm_ms"] = (time.perf_counter() - t1) * 1000
        self.clear_caches()
        log.info("warmup done: %s", {k: round(v) for k, v in times.items()})
        return times

    # --- шаги ---

    def retrieve(self, question: str) -> list[Hit]:
        retriever = self.retriever  # снимок: replace_chunks может подменить индекс посреди запроса
        hits = retriever.search(question, k=max(self.settings.top_k * 4, FUSE_TOP))
        return self.reranker.rerank(question, hits, k=self.settings.top_k)

    def gate(self, hits: list[Hit]) -> tuple[bool, float]:
        score = max((h.gate_score for h in hits), default=0.0)
        return score >= self.settings.gate_threshold, score

    def generate(self, question: str, hits: list[Hit], topic: str, *, address_form: str | None = None,
                 site: str | None = None) -> tuple[dict, str, str | None]:
        """→ (сырой ответ, mode, ошибка LLM для fallback)."""
        # legal/security — режим «только цитата»: LLM не пересказывает взыскания и режим охраны
        if self.generator is None or topic in ("legal", "security"):
            return extractive_answer(question, hits), "extractive", None
        try:
            raw = self.generator.generate(question, [h.chunk for h in hits], address_form=address_form, site=site)
            return raw, "llm", None
        except Exception as exc:  # LLM недоступен, не JSON, не по схеме — дословная цитата всё равно верна
            # TimeoutError — бюджет LLM_TIMEOUT (llm.LLMTimeout): /ask не ждёт медленный под
            reason = "timeout" if isinstance(exc, TimeoutError) else type(exc).__name__
            log.warning("LLM failed (%s), fallback to extractive", reason)
            return extractive_answer(question, hits), "extractive_fallback", reason

    def ask(self, question: str, site: str | None = None, trace_id: str | None = None,
            address_form: str | None = None) -> Answer:
        t0 = time.perf_counter()
        # В логи не пишем текст вопроса: в нём могут быть ПДн.
        route = route_question(question)
        if route is not None:
            log.info("trace=%s route=%s", trace_id, route.name)
            return Answer.abstain("routed_topic", topic=route.topic)

        hits = self.retrieve(question)  # TODO: фильтр по site, когда у чанков появится площадка
        if not hits:
            log.info("trace=%s abstain=no_source", trace_id)
            return Answer.abstain("no_source")

        passed, gate_score = self.gate(hits)
        topic = classify_topic(question + "\n" + hits[0].chunk.body)
        if not passed:
            log.info("trace=%s abstain=below_threshold gate=%.3f thr=%.2f", trace_id, gate_score,
                     self.settings.gate_threshold)
            return Answer.abstain("below_threshold", topic=topic)

        raw, mode, llm_error = self.generate(question, hits, topic, address_form=address_form, site=site)
        answer_text = raw.get("answer")
        if raw.get("needs_escalation") or not isinstance(answer_text, str) or not answer_text.strip():
            # Отказ самой модели не перебиваем дословной цитатой: так она отбивает инъекции и
            # вопросы, на которые фрагменты отвечают не прямо.
            log.info("trace=%s abstain=no_source mode=%s (generator declined)", trace_id, mode)
            return Answer.abstain("no_source", topic=topic, mode=mode, check="declined")

        ok_check = f"fallback:{llm_error}" if llm_error else "ok"
        answer = self._finish(raw, mode, ok_check, hits, gate_score, topic, trace_id, t0, address_form)
        if isinstance(answer, str) and mode == "llm":
            # Ответ модели не прошёл проверку кодом (чужое число, цитата не из пункта, форма
            # обращения, длина).
            # Gate уже пройден, значит дословная цитата лучшего пункта верна — отдаём её, а не отказ.
            log.info("trace=%s llm check=%s, fallback to extractive", trace_id, answer)
            answer = self._finish(extractive_answer(question, hits), "extractive_fallback",
                                  f"fallback:{answer}", hits, gate_score, topic, trace_id, t0, address_form)
        if isinstance(answer, str):
            return Answer.abstain("no_source", topic=topic, mode=mode, check=answer)
        return answer

    def _finish(self, raw: dict, mode: str, ok_check: str, hits: list[Hit], gate_score: float,
                topic: str, trace_id: str | None, t0: float,
                address_form: str | None = None) -> "Answer | str":
        """Проверки кодом поверх ответа генератора. → Answer или причина провала проверки."""
        answer_text = raw["answer"]
        retrieved = [h.chunk for h in hits]
        raw_sources = raw.get("sources")
        result = verify_sources(raw_sources if isinstance(raw_sources, list) else [], retrieved)
        if not result.ok:
            log.warning("trace=%s mode=%s verify_failed=%s", trace_id, mode, result.errors)
            return "verify_failed"

        confidence = _confidence(gate_score, self.settings.gate_threshold,
                                 raw.get("confidence") if mode == "llm" else None)
        text = answer_text.strip()
        if mode == "llm":
            # Числа проверяем по полному тексту, до обрезки: выдуманное число в отрезанном
            # хвосте — всё равно признак, что модель отвечала «из головы».
            foreign = foreign_numbers(text, result.sources, retrieved)
            if foreign:
                log.warning("trace=%s mode=llm foreign_numbers=%d", trace_id, len(foreign))
                return "foreign_numbers"
            # «ты» при address_form=vy (и наоборот). Дословная цитата пункта безлична — её не проверяем.
            wrong = wrong_address_form(text, address_form)
            if wrong:
                log.warning("trace=%s mode=llm wrong_address_form=%d", trace_id, len(wrong))
                return "wrong_address_form"
            fitted = fit_length(text, MAX_ANSWER_CHARS)
            if fitted is None:
                log.warning("trace=%s mode=llm too_long=%d", trace_id, len(text))
                return "too_long"
            if fitted != text:
                log.info("trace=%s answer truncated %d→%d chars", trace_id, len(text), len(fitted))
                ok_check = "truncated"
                if confidence == "high":  # отрезали условия — полным ответ уже не назовёшь
                    confidence = "medium"
                text = fitted

        log.info("trace=%s answered mode=%s gate=%.3f sources=%s conf=%s check=%s %.0fms", trace_id, mode,
                 gate_score, [s.clause_id for s in result.sources], confidence, ok_check,
                 (time.perf_counter() - t0) * 1000)
        return Answer(
            answer=text,
            sources=[_to_source(s) for s in result.sources],
            confidence=confidence,
            needs_escalation=False,
            topic=topic,
            abstain_reason=None,
            mode=mode,
            check=ok_check,
        )


def _to_source(s: VerifiedSource) -> Source:
    return Source(doc=s.doc, clause_id=s.clause_id, page=s.page, quote=s.quote)
