"""Конвейер /ask: route → retrieve → rerank → gate → generate → verify → answer checks.

Каждый шаг — отдельный объект/функция, их можно менять независимо (dense, реранкер, судья).
"""

from __future__ import annotations

import logging
import os
import time
from concurrent.futures import Future, ThreadPoolExecutor
from dataclasses import dataclass, field
from typing import Protocol

from chunker import Chunk
from llm import LLMClient, LLMConfig
from query_expand import QueryExpander, default_expander
from retrieval import (BM25Retriever, CrossEncoderReranker, DenseRetriever, Encoder, Hit, HybridRetriever,
                       NoopReranker, PairScorer, Reranker, Retriever)
from rewrite import QueryRewriter
from routing import classify_topic, route_question
from textnorm import tokenize
from verify import (MAX_ANSWER_CHARS, VerifiedSource, fit_length, foreign_numbers, verify_sources,
                    wrong_address_form)

log = logging.getLogger("rag.pipeline")

# Пороги gate: покрытие вопроса по IDF (BM25-режим) и sigmoid реранкера по лучшему пункту.
# Реранкер: 0.2 — калибровка на настоящих документах ПОЛИПЛАСТИК (eval/golden-real.csv, 10 ловушек):
# худшая ловушка 0.102, ближайшие верные ответы 0.143 и 0.241; 0.15 и 0.2 дают одинаково, у 0.2
# запас ~0.8 логита над ловушкой. На синтетике 0.1 → 0.2 стоит одного ответа (README, «Реальный корпус»).
DEFAULT_GATE = {"lexical": 0.5, "reranker": 0.2}
RRF_K = 60
FUSE_TOP = 20  # кандидатов после RRF
RERANK_TOP = 8  # из них → в реранкер (RAG_RERANK_TOP); на реальном корпусе 8 ≡ 20 по качеству, p50 −40%
WARMUP_QUESTION = "Как устроен первый рабочий день?"
SYNONYM_MODES = ("off", "retrieve", "rerank")
REWRITE_TIMEOUT = 3.0  # с, бюджет LLM-переформулировки (RAG_REWRITE_TIMEOUT)
REWRITE_MAX = 3        # формулировок от LLM (RAG_REWRITE_MAX)
REWRITE_RERANK_TOP = 4  # чанков на формулировку → в реранкер (RAG_REWRITE_RERANK_TOP)
# Бюджет всего /ask, с (RAG_ASK_BUDGET): бот ждёт 8 с. LLM-генерация получает остаток (не больше
# LLM_TIMEOUT), переформулировка запускается, только если прошло меньше половины бюджета.
ASK_BUDGET = 7.5
MIN_LLM_S = 0.8  # меньше — LLM не зовём, отдаём цитату
REWRITE_LLM_TOP = 3  # чанков в LLM, когда пункт найден переформулировкой (короче промпт — быстрее)


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
    # словарь synonyms.yaml: off | retrieve (расширенный вопрос только в BM25 и dense) |
    # rerank (ещё и в реранкер: скор пункта = max по исходному и расширенному вопросу)
    synonyms: str = "off"
    # LLM-переформулировка вопроса при провале gate (нужен LLM_BASE_URL)
    rewrite: bool = False
    rewrite_timeout: float = REWRITE_TIMEOUT
    rewrite_max: int = REWRITE_MAX
    rewrite_rerank_top: int = REWRITE_RERANK_TOP
    ask_budget: float = ASK_BUDGET  # 0 — без общего бюджета (только LLM_TIMEOUT)
    rewrite_llm_top: int = REWRITE_LLM_TOP
    # запрашивать переформулировку параллельно с поиском, не дожидаясь провала gate:
    # −0.7–1 с на разговорных вопросах ценой лишнего короткого запроса к LLM на каждый вопрос
    rewrite_speculative: bool = False

    def __post_init__(self):
        if self.synonyms not in SYNONYM_MODES:
            raise ValueError(f"RAG_SYNONYMS={self.synonyms!r}: ожидается {' | '.join(SYNONYM_MODES)}")

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
            synonyms=os.getenv("RAG_SYNONYMS") or "rerank",
            rewrite=(os.getenv("RAG_REWRITE") or "1") not in ("0", "false", "off", "no"),
            rewrite_timeout=float(os.getenv("RAG_REWRITE_TIMEOUT") or REWRITE_TIMEOUT),
            rewrite_max=int(os.getenv("RAG_REWRITE_MAX") or REWRITE_MAX),
            rewrite_rerank_top=int(os.getenv("RAG_REWRITE_RERANK_TOP") or REWRITE_RERANK_TOP),
            ask_budget=float(os.getenv("RAG_ASK_BUDGET") or ASK_BUDGET),
            rewrite_llm_top=int(os.getenv("RAG_REWRITE_LLM_TOP") or REWRITE_LLM_TOP),
            rewrite_speculative=(os.getenv("RAG_REWRITE_SPECULATIVE") or "0") in ("1", "true", "on", "yes"),
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
    gate: float | None = None   # итоговый скор gate (после переформулировок)
    rewrites: int = 0           # сколько формулировок LLM ушло в повторный поиск
    rewrite_used: bool = False  # gate прошёл только благодаря переформулировке
    hits: list = field(default_factory=list)  # чанки, на которых решал gate (для eval)

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
                 pair_scorer: PairScorer | None = None, rewriter: QueryRewriter | None = None,
                 expander: QueryExpander | None = None):
        """encoder / pair_scorer — готовые модели (или заглушки в тестах) вместо загрузки по имени.
        rewriter — LLM-переформулировка при провале gate (None — шага нет)."""
        self.settings = settings
        self.rewriter = rewriter
        self._rw_pool = ThreadPoolExecutor(max_workers=4, thread_name_prefix="rewrite") if rewriter else None
        self.expander = expander if expander is not None else (
            default_expander() if settings.synonyms != "off" else None)
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
        settings = Settings.from_env()
        client = LLMClient(cfg) if cfg else None
        rewriter = (QueryRewriter(client, timeout_s=settings.rewrite_timeout, max_queries=settings.rewrite_max)
                    if client is not None and settings.rewrite else None)
        return cls(settings, chunks, generator=client, rewriter=rewriter)

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
        if llm and self.rewriter is not None:  # префикс промпта переформулировки — в кэш vLLM
            t2 = time.perf_counter()
            self.rewriter.rewrite(WARMUP_QUESTION)
            times["rewrite_ms"] = (time.perf_counter() - t2) * 1000
        self.clear_caches()
        log.info("warmup done: %s", {k: round(v) for k, v in times.items()})
        return times

    # --- шаги ---

    def expand(self, question: str) -> str | None:
        """Вопрос, расширенный словарём synonyms.yaml; None — словарь выключен или не сработал."""
        return self.expander.expand(question) if self.expander is not None else None

    def retrieve(self, question: str) -> list[Hit]:
        retriever = self.retriever  # снимок: replace_chunks может подменить индекс посреди запроса
        expanded = self.expand(question)
        hits = retriever.search(expanded or question, k=max(self.settings.top_k * 4, FUSE_TOP))
        if expanded and self.settings.synonyms == "rerank" and hasattr(self.reranker, "rerank_many"):
            cands = hits[:self.settings.rerank_top]
            return self.reranker.rerank_many([(question, cands), (expanded, cands)], k=self.settings.top_k)
        return self.reranker.rerank(question, hits, k=self.settings.top_k)

    def retrieve_rewrites(self, question: str, hits: list[Hit], trace_id: str | None = None,
                          pending: "Future | None" = None) -> tuple[list[Hit], int]:
        """LLM переписывает вопрос в 1–3 формулировки, по каждой — поиск и реранк.

        Защита от дрейфа смысла: одна формулировка может увести в чужую тему («подарок» →
        «разглашение тайны») и набрать высокий скор. Поэтому пункт засчитывается, только если он
        первый ещё хотя бы у одного запроса — исходного вопроса или другой формулировки
        (согласие). Скор такого пункта — лучший среди согласных запросов, gate прежний.
        → (hits для gate: согласованный пункт + исходные hits, число формулировок)."""
        t0 = time.perf_counter()
        if pending is not None:  # переформулировка запрошена заранее, параллельно с поиском
            rewrites = pending.result()
        else:
            rewrites = self.rewriter.rewrite(question) if self.rewriter is not None else []
        t_llm = time.perf_counter()
        if not rewrites:
            log.info("trace=%s rewrite: none %.0fms", trace_id, (t_llm - t0) * 1000)
            return hits, 0
        retriever = self.retriever
        jobs = []
        for rw in rewrites:
            q = self.expand(rw) or rw
            jobs.append((rw, retriever.search(q, k=FUSE_TOP)[:self.settings.rewrite_rerank_top]))
        if hasattr(self.reranker, "score_jobs"):
            per_query = self.reranker.score_jobs(jobs)
        else:  # без реранкера gate лексический: у каждой формулировки свой скор покрытия
            per_query = [self.reranker.rerank(rw, cands, k=len(cands)) for rw, cands in jobs]

        def top_key(h: Hit) -> tuple[int, str | None]:
            return id(h.chunk), h.clause_id

        tops = [r[0] for r in per_query if r]
        votes: dict[tuple[int, str | None], int] = {}
        for h in tops + hits[:1]:
            votes[top_key(h)] = votes.get(top_key(h), 0) + 1
        agreed = [h for h in tops if votes[top_key(h)] >= 2]
        log.info("trace=%s rewrite: %d queries, agreed=%d llm=%.0fms rerank=%.0fms", trace_id, len(rewrites),
                 len(agreed), (t_llm - t0) * 1000, (time.perf_counter() - t_llm) * 1000)
        if not agreed:
            return hits, len(rewrites)
        best = max(agreed, key=lambda h: h.gate_score)
        if hits and top_key(hits[0]) == top_key(best) and hits[0].gate_score >= best.gate_score:
            return hits, len(rewrites)
        rest = [h for h in hits if h.chunk is not best.chunk]
        return [best] + rest[:max(self.settings.top_k - 1, 0)], len(rewrites)

    def gate(self, hits: list[Hit]) -> tuple[bool, float]:
        score = max((h.gate_score for h in hits), default=0.0)
        return score >= self.settings.gate_threshold, score

    def generate(self, question: str, hits: list[Hit], topic: str, *, address_form: str | None = None,
                 site: str | None = None, judge: bool = False,
                 timeout_s: float | None = None) -> tuple[dict, str, str | None]:
        """→ (сырой ответ, mode, ошибка LLM для fallback).

        judge — пункт найден только переформулировкой: LLM вызывается и для legal/security, чтобы
        подтвердить, что пункт отвечает на исходный вопрос (сам ответ там всё равно цитата).
        timeout_s — остаток бюджета /ask; меньше MIN_LLM_S — LLM не зовём."""
        # legal/security — режим «только цитата»: LLM не пересказывает взыскания и режим охраны
        if self.generator is None or (topic in ("legal", "security") and not judge):
            return extractive_answer(question, hits), "extractive", None
        if timeout_s is not None and timeout_s < MIN_LLM_S:
            log.warning("LLM skipped: %.1fs left of RAG_ASK_BUDGET", timeout_s)
            return extractive_answer(question, hits), "extractive_fallback", "budget"
        kwargs = {"timeout_s": timeout_s} if timeout_s is not None else {}
        try:
            raw = self.generator.generate(question, [h.chunk for h in hits], address_form=address_form,
                                          site=site, **kwargs)
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

        pending = (self._rw_pool.submit(self.rewriter.rewrite, question)
                   if self._rw_pool is not None and self.settings.rewrite_speculative else None)
        hits = self.retrieve(question)  # TODO: фильтр по site, когда у чанков появится площадка
        if not hits and self.rewriter is None:
            log.info("trace=%s abstain=no_source", trace_id)
            return Answer.abstain("no_source")

        passed, gate_score = self.gate(hits)
        n_rw, rw_used = 0, False
        budget = self.settings.ask_budget
        if not passed and self.rewriter is not None and time.perf_counter() - t0 < budget / 2:
            # Разговорный вопрос: переписать языком регламента и поискать ещё раз. Gate тот же.
            rw_hits, n_rw = self.retrieve_rewrites(question, hits, trace_id, pending)
            rw_passed, rw_score = self.gate(rw_hits)
            log.info("trace=%s rewrite gate %.3f → %.3f", trace_id, gate_score, rw_score)
            if rw_passed:
                hits, passed, gate_score, rw_used = rw_hits, True, rw_score, True
            else:
                gate_score = max(gate_score, rw_score)
        if not hits:  # ни исходный вопрос, ни формулировки ничего не нашли (бывает только в BM25)
            log.info("trace=%s abstain=no_source", trace_id)
            ans = Answer.abstain("no_source")
            ans.rewrites = n_rw
            return ans
        topic = classify_topic(question + "\n" + hits[0].chunk.body)

        def done(ans: Answer) -> Answer:
            ans.gate, ans.rewrites, ans.rewrite_used, ans.hits = gate_score, n_rw, rw_used, hits
            return ans

        if not passed:
            log.info("trace=%s abstain=below_threshold gate=%.3f thr=%.2f", trace_id, gate_score,
                     self.settings.gate_threshold)
            return done(Answer.abstain("below_threshold", topic=topic))

        left = budget - (time.perf_counter() - t0) if budget else None
        # по переформулировке в LLM идут только лучшие чанки: промпт короче, ответ быстрее
        gen_hits = hits[:self.settings.rewrite_llm_top] if rw_used else hits
        raw, mode, llm_error = self.generate(question, gen_hits, topic, address_form=address_form, site=site,
                                             judge=rw_used, timeout_s=left)
        if rw_used and mode != "llm":
            # Пункт нашёлся только по переформулировке, а подтвердить его LLM не смогла (таймаут,
            # ошибка, LLM нет): исходный вопрос gate не прошёл — отказ, а не цитата наугад.
            log.info("trace=%s abstain=below_threshold rewrite unconfirmed (%s)", trace_id, llm_error)
            return done(Answer.abstain("below_threshold", topic=topic, mode=mode,
                                       check=f"rewrite_unconfirmed:{llm_error}"))
        answer_text = raw.get("answer")
        if raw.get("needs_escalation") or not isinstance(answer_text, str) or not answer_text.strip():
            # Отказ самой модели не перебиваем дословной цитатой: так она отбивает инъекции и
            # вопросы, на которые фрагменты отвечают не прямо.
            log.info("trace=%s abstain=no_source mode=%s (generator declined)", trace_id, mode)
            return done(Answer.abstain("no_source", topic=topic, mode=mode, check="declined"))

        ok_check = f"fallback:{llm_error}" if llm_error else "ok"
        answer = self._finish(raw, mode, ok_check, gen_hits, gate_score, topic, trace_id, t0, address_form)
        if rw_used and answer == "foreign_numbers":
            # Число не из пунктов — модель отвечала «из головы»: пункт, найденный переформулировкой,
            # на вопрос не отвечает (так выглядят ловушки «сколько дней на переезд»).
            log.info("trace=%s abstain=below_threshold rewrite unconfirmed (foreign_numbers)", trace_id)
            return done(Answer.abstain("below_threshold", topic=topic, mode=mode,
                                       check="rewrite_unconfirmed:foreign_numbers"))
        if isinstance(answer, Answer) and rw_used and topic in ("legal", "security"):
            # LLM подтвердила пункт; по legal/security отвечаем только его дословной цитатой
            cited = self._cited_hit(answer, hits)
            answer = self._finish(extractive_answer(question, [cited] if cited else hits), "extractive", "ok",
                                  hits, gate_score, topic, trace_id, t0, address_form)
        elif isinstance(answer, str) and mode == "llm":
            # Ответ модели не прошёл проверку кодом (чужое число, цитата не из пункта, форма
            # обращения, длина).
            # Gate уже пройден, значит дословная цитата лучшего пункта верна — отдаём её, а не отказ.
            log.info("trace=%s llm check=%s, fallback to extractive", trace_id, answer)
            answer = self._finish(extractive_answer(question, hits), "extractive_fallback",
                                  f"fallback:{answer}", hits, gate_score, topic, trace_id, t0, address_form)
        if isinstance(answer, str):
            answer = Answer.abstain("no_source", topic=topic, mode=mode, check=answer)
        return done(answer)

    @staticmethod
    def _cited_hit(answer: "Answer", hits: list[Hit]) -> Hit | None:
        """Hit с первым пунктом, на который сослалась LLM (clause_id = этот пункт)."""
        for src in answer.sources:
            for h in hits:
                if h.chunk.doc == src.doc and h.chunk.clause(src.clause_id) is not None:
                    return Hit(chunk=h.chunk, score=h.score, gate_score=h.gate_score, clause_id=src.clause_id)
        return None

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
