"""Dense + RRF + реранкер + gate на заглушках моделей (без torch и без скачивания весов)."""

import numpy as np
import pytest

from pipeline import DEFAULT_GATE, Settings
from retrieval import (BM25Retriever, CrossEncoderReranker, DenseRetriever, Hit, HybridRetriever,
                       rrf_fuse)
from textnorm import tokenize

# «Концепты» заглушки-эмбеддера: перефраз и текст пункта попадают в одно измерение.
CONCEPTS = [
    {"пропуск", "утеря", "потерять", "посеять", "бейдж"},
    {"отпуск", "отдыхать"},
    {"аванс", "заработный", "деньги"},
    {"смена", "ночной"},
    {"дмс", "страховка"},
]


class FakeEncoder:
    def __init__(self):
        self.calls: list[list[str]] = []

    def encode(self, texts, **kwargs):
        self.calls.append(list(texts))
        out = np.zeros((len(texts), len(CONCEPTS) + 1), dtype=np.float32)
        for i, t in enumerate(texts):
            toks = set(tokenize(t))
            for d, words in enumerate(CONCEPTS):
                out[i, d] = len(toks & words)
            out[i, -1] = 0.1  # чтобы вектор не был нулевым
        return out


class FakeScorer:
    """Логит = +4, если в тексте чанка есть ключевое слово вопроса из словаря, иначе −4."""

    KEYS = {"потерял": "утере", "аванс": "аванс", "ночная": "ночная"}

    def __init__(self):
        self.kwargs = None

    def predict(self, pairs, **kwargs):
        self.kwargs = kwargs
        out = []
        for q, text in pairs:
            hit = any(k in q.lower() and v in text.lower() for k, v in self.KEYS.items())
            out.append(4.0 if hit else -4.0)
        return np.array(out)


class Spy:
    calls = 0

    def generate(self, question, chunks, **kwargs):
        self.calls += 1
        return {}


def test_dense_finds_paraphrase_by_concept(chunks):
    dense = DenseRetriever(chunks, encoder=FakeEncoder())
    hits = dense.search("посеял бейдж, что делать", k=3)
    assert hits[0].chunk.clause_id == "5.1"
    assert all(h.gate_score == 0.0 for h in hits)  # dense сам не гейтит


def test_dense_cache_reuses_embeddings_on_rebuild(chunks):
    enc = FakeEncoder()
    dense = DenseRetriever(chunks, encoder=enc)
    assert len(enc.calls) == 1 and len(enc.calls[0]) == len(chunks)
    rebuilt = dense.with_chunks(chunks[:3])
    assert len(enc.calls) == 1  # всё из кэша, модель не вызывалась
    assert rebuilt.matrix.shape[0] == 3


def test_rrf_k60_rewards_agreement(chunks):
    a, b, c = chunks[0], chunks[1], chunks[2]
    r1 = [Hit(a, 9.0, 0.2), Hit(b, 5.0, 0.9)]
    r2 = [Hit(b, 0.8), Hit(c, 0.7)]
    fused = rrf_fuse([r1, r2], k=60)
    assert fused[0].chunk is b  # 1/62 + 1/61 > 1/61
    assert fused[0].score == pytest.approx(1 / 62 + 1 / 61)
    assert fused[0].gate_score == 0.9  # максимум по ретриверам


def test_reranker_sigmoid_from_raw_logits_and_order(chunks):
    scorer = FakeScorer()
    rr = CrossEncoderReranker(model=scorer)
    hits = [Hit(ch, 1.0) for ch in chunks]
    out = rr.rerank("Потерял пропуск", hits, k=3)
    assert out[0].chunk.clause_id == "5.1"
    assert out[0].gate_score == pytest.approx(1 / (1 + np.exp(-4.0)))
    assert out[1].gate_score == pytest.approx(1 / (1 + np.exp(4.0)))
    # sigmoid считаем сами: модель должна вернуть сырые логиты
    assert type(scorer.kwargs["activation_fn"]).__name__ == "Identity"


def test_reranker_scores_clauses_inside_merged_chunk(chunks):
    merged = next(ch for ch in chunks if ch.clause_ids == ["3.1", "3.2"])
    out = CrossEncoderReranker(model=FakeScorer()).rerank("Когда ночная смена?", [Hit(merged, 1.0)], k=1)
    assert out[0].clause_id == "3.2"  # лучший пункт, а не первый в чанке


def test_extractive_answer_quotes_reranker_clause(chunks):
    from pipeline import Pipeline
    p = Pipeline(Settings(corpus_dir="x", retriever="hybrid", reranker="bge", gate_threshold=0.3), chunks,
                 encoder=FakeEncoder(), pair_scorer=FakeScorer())
    res = p.ask("Во сколько ночная?")  # BM25-покрытие пунктов 3.1 и 3.2 одинаковое, решает реранкер
    assert res.sources[0].clause_id == "3.2"


def test_hybrid_rerank_pipeline_answers_paraphrase(chunks):
    from pipeline import Pipeline
    p = Pipeline(Settings(corpus_dir="x", retriever="hybrid", reranker="bge", gate_threshold=0.3), chunks,
                 encoder=FakeEncoder(), pair_scorer=FakeScorer())
    res = p.ask("Потерял пропуск, что делать?")
    assert res.needs_escalation is False
    assert res.sources[0].clause_id == "5.1"
    assert res.topic == "security" and res.answer == res.sources[0].quote


def test_reranker_gate_blocks_trap_before_llm(chunks):
    from pipeline import Pipeline
    spy = Spy()
    p = Pipeline(Settings(corpus_dir="x", retriever="hybrid", reranker="bge", gate_threshold=0.3), chunks,
                 generator=spy, encoder=FakeEncoder(), pair_scorer=FakeScorer())
    res = p.ask("Положен ли ДМС после испытательного срока?")
    assert res.needs_escalation and res.abstain_reason == "below_threshold"
    assert spy.calls == 0


def test_replace_chunks_rebuilds_index_without_reloading_models(chunks):
    from pipeline import Pipeline
    enc = FakeEncoder()
    p = Pipeline(Settings(corpus_dir="x", retriever="hybrid", reranker="bge"), chunks,
                 encoder=enc, pair_scorer=FakeScorer())
    p.replace_chunks([ch for ch in chunks if ch.clause_id != "6.1"])
    assert len(enc.calls) == 1
    assert p.ask("Когда аванс?").needs_escalation  # пункт 6.1 из индекса убран


def test_hybrid_without_reranker_uses_lexical_gate(chunks):
    bm25 = BM25Retriever(chunks)
    dense = DenseRetriever(chunks, encoder=FakeEncoder())
    hits = HybridRetriever([bm25, dense]).search("Когда выплачивают аванс?", k=5)
    assert hits[0].chunk.clause_id == "6.1"
    assert hits[0].gate_score > 0.5  # покрытие BM25, а не 0 от dense


def test_default_gate_switches_with_reranker(monkeypatch):
    monkeypatch.delenv("RAG_GATE_THRESHOLD", raising=False)
    monkeypatch.setenv("RAG_RERANKER", "bge")
    assert Settings.from_env().gate_threshold == DEFAULT_GATE["reranker"]
    monkeypatch.setenv("RAG_RERANKER", "none")
    assert Settings.from_env().gate_threshold == DEFAULT_GATE["lexical"]


def test_unknown_retriever_rejected(make_pipeline):
    with pytest.raises(ValueError):
        make_pipeline(retriever="faiss")
    with pytest.raises(ValueError):
        make_pipeline(reranker="cohere")
