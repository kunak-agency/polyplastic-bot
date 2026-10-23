"""Поиск: BM25, dense (`BAAI/bge-m3`), RRF-фьюжн и реранкер (`BAAI/bge-reranker-v2-m3`).

По умолчанию работает только BM25. Dense и реранкер включаются RAG_RETRIEVER=hybrid и
RAG_RERANKER=bge; модели грузятся на старте сервиса из кэша HF_HOME (на сборке образа
не качаются). В тестах вместо моделей подставляются заглушки с тем же интерфейсом.
"""

from __future__ import annotations

import logging
import math
import threading
import time
from dataclasses import dataclass
from typing import Protocol

import numpy as np
from rank_bm25 import BM25Okapi

from chunker import Chunk
from textnorm import expand_doc_tokens, tokenize

log = logging.getLogger("rag.retrieval")

# Чанк — до 400 слов, это ~600–800 токенов XLM-R. 1024 хватает с запасом и не даёт
# 8192-токенному окну bge-m3 раздувать время на CPU.
DENSE_MAX_TOKENS = 1024
RERANK_MAX_TOKENS = 1024


@dataclass
class Hit:
    chunk: Chunk
    score: float          # «сырой» скор ретривера (для логов и сортировки)
    gate_score: float = 0.0  # скор в [0, 1], по которому решает gate
    clause_id: str | None = None  # лучший пункт склеенного чанка, если его выбрал реранкер


class Retriever(Protocol):
    def search(self, query: str, k: int) -> list[Hit]: ...


class Reranker(Protocol):
    def rerank(self, query: str, hits: list[Hit], k: int) -> list[Hit]: ...


class BM25Retriever:
    """BM25Okapi по леммам. gate_score — IDF-взвешенная доля слов вопроса, найденных в чанке.

    Сырой BM25 не нормирован и зависит от длины вопроса, поэтому порог по нему ставить
    нельзя. Покрытие по IDF — грубый, но объяснимый аналог «уверенности»: если в вопросе
    есть редкое слово, которого нет ни в одном документе (ДМС), его вес максимален и
    покрытие падает.
    """

    def __init__(self, chunks: list[Chunk]):
        self.chunks = chunks
        self.docs_tokens = [expand_doc_tokens(tokenize(c.index_text)) for c in chunks]
        self.doc_sets = [set(t) for t in self.docs_tokens]
        self.bm25 = BM25Okapi(self.docs_tokens) if chunks else None
        n = len(chunks)
        df: dict[str, int] = {}
        for s in self.doc_sets:
            for t in s:
                df[t] = df.get(t, 0) + 1
        # сглаженный IDF, всегда > 0; слово вне корпуса получает максимальный вес
        self.idf = {t: math.log(1 + (n - d + 0.5) / (d + 0.5)) for t, d in df.items()}
        self.oov_idf = math.log(1 + (n + 0.5) / 0.5) if n else 1.0

    def coverage(self, q_tokens: list[str], doc_index: int) -> float:
        terms = set(q_tokens)
        if not terms:
            return 0.0
        total = sum(self.idf.get(t, self.oov_idf) for t in terms)
        hit = sum(self.idf.get(t, self.oov_idf) for t in terms if t in self.doc_sets[doc_index])
        return hit / total if total else 0.0

    def search(self, query: str, k: int) -> list[Hit]:
        if self.bm25 is None:
            return []
        q = tokenize(query)
        if not q:
            return []
        scores = self.bm25.get_scores(q)
        order = sorted(range(len(scores)), key=lambda i: scores[i], reverse=True)[:k]
        return [Hit(chunk=self.chunks[i], score=float(scores[i]), gate_score=self.coverage(q, i))
                for i in order if scores[i] > 0]


class Encoder(Protocol):
    """То, что умеет SentenceTransformer: список текстов → матрица (n, dim)."""

    def encode(self, texts: list[str], **kwargs) -> np.ndarray: ...


class PairScorer(Protocol):
    """То, что умеет CrossEncoder: список пар (вопрос, текст) → сырые логиты (n,)."""

    def predict(self, pairs: list[tuple[str, str]], **kwargs) -> np.ndarray: ...


_MODEL_LOCK = threading.Lock()  # torch-модели не гоняем из нескольких потоков пула FastAPI разом


def load_sentence_transformer(model_name: str, device: str = "cpu") -> Encoder:
    """Грузит bge-m3 из кэша HF_HOME (если его нет — качает с Hugging Face). FP32 на CPU."""
    from sentence_transformers import SentenceTransformer  # noqa: PLC0415 — тяжёлый импорт только по флагу

    t0 = time.perf_counter()
    model = SentenceTransformer(model_name, device=device)
    model.max_seq_length = min(model.max_seq_length or 8192, DENSE_MAX_TOKENS)
    log.info("dense model %s loaded on %s in %.1fs", model_name, device, time.perf_counter() - t0)
    return model


def load_cross_encoder(model_name: str, device: str = "cpu") -> PairScorer:
    from sentence_transformers import CrossEncoder  # noqa: PLC0415

    t0 = time.perf_counter()
    model = CrossEncoder(model_name, device=device, max_length=RERANK_MAX_TOKENS)
    log.info("reranker %s loaded on %s in %.1fs", model_name, device, time.perf_counter() - t0)
    return model


class DenseRetriever:
    """bge-m3: cosine по L2-нормированным эмбеддингам `index_text` чанка.

    gate_score у dense-попаданий 0: косинус bi-encoder'а меряет «похоже по теме», а не
    «отвечает на вопрос», и как вероятность не откалиброван, поэтому решение принимает
    реранкер. Без реранкера гибрид гейтится лексическим покрытием BM25.
    Эмбеддинги кэшируются по тексту чанка, чтобы пересборка индекса после загрузки
    документа считала только новые чанки.
    """

    def __init__(self, chunks: list[Chunk], model_name: str = "BAAI/bge-m3",
                 encoder: Encoder | None = None, cache: dict[str, np.ndarray] | None = None):
        self.chunks = chunks
        self.model_name = model_name
        self.encoder = encoder if encoder is not None else load_sentence_transformer(model_name)
        self.cache = cache if cache is not None else {}
        texts = [c.index_text for c in chunks]
        missing = list(dict.fromkeys(t for t in texts if t not in self.cache))
        if missing:
            vecs = self._encode(missing)
            self.cache.update(zip(missing, vecs))
        self.matrix = np.vstack([self.cache[t] for t in texts]) if texts else np.zeros((0, 1), dtype=np.float32)

    def _encode(self, texts: list[str]) -> np.ndarray:
        with _MODEL_LOCK:
            vecs = self.encoder.encode(texts, batch_size=8, normalize_embeddings=True,
                                       convert_to_numpy=True, show_progress_bar=False)
        vecs = np.asarray(vecs, dtype=np.float32)
        norms = np.linalg.norm(vecs, axis=1, keepdims=True)
        return vecs / np.where(norms == 0, 1.0, norms)

    def with_chunks(self, chunks: list[Chunk]) -> "DenseRetriever":
        """Новый индекс на той же модели и том же кэше эмбеддингов."""
        return DenseRetriever(chunks, self.model_name, encoder=self.encoder, cache=self.cache)

    def search(self, query: str, k: int) -> list[Hit]:
        if not self.chunks:
            return []
        q = self._encode([query])[0]
        sims = self.matrix @ q
        order = np.argsort(-sims)[:k]
        return [Hit(chunk=self.chunks[i], score=float(sims[i]), gate_score=0.0) for i in order]


def rrf_fuse(rankings: list[list[Hit]], k: int = 60, top: int = 20) -> list[Hit]:
    """Reciprocal Rank Fusion: score = Σ 1/(k + rank). gate_score берётся максимальный."""
    fused: dict[int, Hit] = {}
    acc: dict[int, float] = {}
    for ranking in rankings:
        for rank, hit in enumerate(ranking, start=1):
            key = id(hit.chunk)
            acc[key] = acc.get(key, 0.0) + 1.0 / (k + rank)
            if key not in fused or hit.gate_score > fused[key].gate_score:
                fused[key] = hit
    order = sorted(acc, key=acc.get, reverse=True)[:top]
    return [Hit(chunk=fused[key].chunk, score=acc[key], gate_score=fused[key].gate_score) for key in order]


class HybridRetriever:
    def __init__(self, retrievers: list[Retriever], rrf_k: int = 60, fuse_top: int = 20):
        self.retrievers = retrievers
        self.rrf_k = rrf_k
        self.fuse_top = fuse_top

    def search(self, query: str, k: int) -> list[Hit]:
        rankings = [r.search(query, self.fuse_top) for r in self.retrievers]
        return rrf_fuse(rankings, k=self.rrf_k, top=self.fuse_top)[:k]


class NoopReranker:
    """Без реранкера: порядок и gate_score ретривера сохраняются."""

    def rerank(self, query: str, hits: list[Hit], k: int) -> list[Hit]:
        return hits[:k]


def _sigmoid(x: np.ndarray) -> np.ndarray:
    return 1.0 / (1.0 + np.exp(-x))


class CrossEncoderReranker:
    """bge-reranker-v2-m3 по пунктам: пары (вопрос, «раздел. текст пункта») → логит → sigmoid.

    Скорим не чанк целиком, а каждый пункт внутри него: склеенный чанк из трёх коротких
    пунктов «размывает» релевантность, и на перефразах скор падал в разы (см. README,
    «Оценка»). Скор чанка = максимум по его пунктам, лучший пункт запоминается в
    Hit.clause_id и идёт в экстрактивный ответ. Заголовок раздела в паре оставлен: он даёт
    реранкеру тему («Оплата труда») без шума из имени документа и номеров пунктов.

    sigmoid считаем сами из сырых логитов (activation_fn=Identity): поведение по умолчанию
    у CrossEncoder менялось между версиями sentence-transformers, а порог gate завязан
    именно на sigmoid.
    """

    def __init__(self, model_name: str = "BAAI/bge-reranker-v2-m3", model: PairScorer | None = None):
        self.model_name = model_name
        self.model = model if model is not None else load_cross_encoder(model_name)

    @staticmethod
    def pair_text(chunk: Chunk, clause) -> str:
        return f"{chunk.section}. {clause.text}" if chunk.section else clause.text

    def _logits(self, pairs: list[tuple[str, str]]) -> np.ndarray:
        kwargs = {"batch_size": 16, "show_progress_bar": False, "convert_to_numpy": True}
        try:
            import torch  # noqa: PLC0415
            kwargs["activation_fn"] = torch.nn.Identity()
        except ImportError:  # заглушка в тестах без torch
            pass
        with _MODEL_LOCK:
            return np.asarray(self.model.predict(pairs, **kwargs), dtype=np.float64).reshape(-1)

    def rerank(self, query: str, hits: list[Hit], k: int) -> list[Hit]:
        if not hits:
            return []
        owners: list[tuple[int, str]] = []
        pairs: list[tuple[str, str]] = []
        for i, h in enumerate(hits):
            for cl in h.chunk.clauses:
                owners.append((i, cl.clause_id))
                pairs.append((query, self.pair_text(h.chunk, cl)))
        probs = _sigmoid(self._logits(pairs))
        best: dict[int, tuple[float, str]] = {}
        for (i, cid), p in zip(owners, probs):
            if i not in best or p > best[i][0]:
                best[i] = (float(p), cid)
        rescored = [Hit(chunk=h.chunk, score=best[i][0], gate_score=best[i][0], clause_id=best[i][1])
                    for i, h in enumerate(hits)]
        rescored.sort(key=lambda h: h.score, reverse=True)
        return rescored[:k]
