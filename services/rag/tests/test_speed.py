"""Ускорение /ask: LRU-кэши вопроса, ограничение кандидатов реранкера, прогрев, выбор устройства."""

import numpy as np
import pytest

from pipeline import RERANK_TOP, Pipeline, Settings
from retrieval import LRU, CrossEncoderReranker, DenseRetriever, Hit, resolve_device
from tests.test_hybrid import FakeEncoder, FakeScorer


class CountingScorer(FakeScorer):
    def __init__(self):
        super().__init__()
        self.pairs: list[list[tuple[str, str]]] = []

    def predict(self, pairs, **kwargs):
        self.pairs.append(list(pairs))
        return super().predict(pairs, **kwargs)


class SpyGen:
    def __init__(self, fail=False):
        self.calls = 0
        self.fail = fail

    def generate(self, question, chunks, **kwargs):
        self.calls += 1
        if self.fail:
            raise TimeoutError
        return {}


def test_lru_evicts_oldest():
    c = LRU(size=2)
    c.put("a", 1)
    c.put("b", 2)
    assert c.get("a") == 1  # «a» теперь свежее «b»
    c.put("c", 3)
    assert c.get("b") is None and c.get("a") == 1 and c.get("c") == 3
    c.clear()
    assert len(c) == 0


def test_query_embedding_cached_and_survives_rebuild(chunks):
    enc = FakeEncoder()
    dense = DenseRetriever(chunks, encoder=enc)
    dense.search("Когда аванс?", k=3)
    dense.search("Когда аванс?", k=3)
    assert len(enc.calls) == 2  # корпус + один вопрос
    dense.with_chunks(chunks[:3]).search("Когда аванс?", k=3)
    assert len(enc.calls) == 2


def test_reranker_caches_pair_logits(chunks):
    scorer = CountingScorer()
    rr = CrossEncoderReranker(model=scorer)
    hits = [Hit(ch, 1.0) for ch in chunks]
    first = rr.rerank("Потерял пропуск", hits, k=3)
    second = rr.rerank("Потерял пропуск", hits, k=3)
    assert len(scorer.pairs) == 1  # второй раз модель не вызывалась
    assert [(h.chunk.clause_id, h.gate_score, h.clause_id) for h in first] == \
        [(h.chunk.clause_id, h.gate_score, h.clause_id) for h in second]
    rr.rerank("Когда аванс?", hits, k=3)
    assert len(scorer.pairs) == 2


def test_reranker_scores_only_top_candidates(chunks):
    scorer = CountingScorer()
    rr = CrossEncoderReranker(model=scorer, max_candidates=2)
    out = rr.rerank("Потерял пропуск", [Hit(ch, 1.0) for ch in chunks], k=5)
    assert len(out) == 2
    assert len(scorer.pairs[0]) == sum(len(ch.clauses) for ch in chunks[:2])


def test_rerank_top_from_env(monkeypatch):
    monkeypatch.delenv("RAG_RERANK_TOP", raising=False)
    assert Settings.from_env().rerank_top == RERANK_TOP
    monkeypatch.setenv("RAG_RERANK_TOP", "8")
    assert Settings.from_env().rerank_top == 8


def test_warmup_runs_models_and_llm_then_clears_caches(chunks):
    enc, scorer, gen = FakeEncoder(), CountingScorer(), SpyGen()
    p = Pipeline(Settings(corpus_dir="x", retriever="hybrid", reranker="bge"), chunks,
                 generator=gen, encoder=enc, pair_scorer=scorer)
    times = p.warmup()
    assert len(enc.calls) == 3 and scorer.pairs and gen.calls == 1
    assert set(times) == {"retrieve_ms", "llm_ms"}
    assert len(p._dense.query_cache) == 0 and len(p.reranker.cache) == 0


def test_warmup_survives_llm_failure(chunks):
    p = Pipeline(Settings(corpus_dir="x", retriever="hybrid", reranker="bge"), chunks,
                 generator=SpyGen(fail=True), encoder=FakeEncoder(), pair_scorer=FakeScorer())
    p.warmup()  # не бросает
    res = p.ask("Когда аванс?")
    assert res.needs_escalation is False and res.mode == "extractive_fallback"


def test_warmup_bm25_without_models(make_pipeline):
    assert "retrieve_ms" in make_pipeline().warmup()


def test_resolve_device(monkeypatch):
    monkeypatch.setenv("RAG_DEVICE", "cpu")
    assert resolve_device() == "cpu"
    assert resolve_device("mps") == "mps"
    monkeypatch.setenv("RAG_DEVICE", "auto")
    assert resolve_device() in ("cpu", "mps", "cuda")


def test_sigmoid_unchanged_with_cache(chunks):
    rr = CrossEncoderReranker(model=FakeScorer())
    out = rr.rerank("Потерял пропуск", [Hit(ch, 1.0) for ch in chunks], k=1)
    assert out[0].gate_score == pytest.approx(1 / (1 + np.exp(-4.0)))


# --- бюджет LLM (LLM_TIMEOUT): медленный под не держит /ask ---

class SlowLLM:
    """Фейковый под: отвечает верным JSON, но через `delay` секунд (как RunPod, ответивший за 13 с)."""

    def __init__(self, delay: float):
        from tests.test_llm import FakeLLM, llm_answer
        self.delay = delay
        self.inner = FakeLLM(llm_answer("Ночная смена начинается в 20:00."))

    def __call__(self, request):
        import time
        time.sleep(self.delay)
        return self.inner(request)


def _client(handler, timeout_s):
    import httpx
    from llm import LLMClient, LLMConfig
    return LLMClient(LLMConfig(base_url="http://llm.test/v1", api_key=None, model="tlite", timeout_s=timeout_s),
                     transport=httpx.MockTransport(handler))


def test_slow_llm_falls_back_to_quote_within_budget(make_pipeline, chunks):
    import time
    from tests.test_llm import NIGHT
    client = _client(SlowLLM(delay=1.5), timeout_s=0.3)
    try:
        t0 = time.perf_counter()
        res = make_pipeline(generator=client).ask(NIGHT)
        elapsed = time.perf_counter() - t0
    finally:
        client.close()
    assert elapsed < 1.0  # не ждали 1.5 с ответа пода
    assert res.needs_escalation is False and res.mode == "extractive_fallback" and res.check == "fallback:timeout"
    clause = next(cl for ch in chunks for cl in ch.clauses if cl.clause_id == "3.2")
    assert res.answer == clause.text == res.sources[0].quote


def test_fast_llm_within_budget_answers(make_pipeline):
    from tests.test_llm import NIGHT
    client = _client(SlowLLM(delay=0.05), timeout_s=2.0)
    try:
        res = make_pipeline(generator=client).ask(NIGHT)
    finally:
        client.close()
    assert res.mode == "llm" and res.check == "ok"


def test_httpx_timeout_is_llm_timeout(make_pipeline):
    import httpx
    from tests.test_llm import NIGHT

    def handler(request):
        raise httpx.ReadTimeout("slow", request=request)

    client = _client(handler, timeout_s=2.0)
    try:
        res = make_pipeline(generator=client).ask(NIGHT)
    finally:
        client.close()
    assert res.check == "fallback:timeout"


def test_llm_timeout_env(monkeypatch):
    from llm import LLMConfig
    monkeypatch.setenv("LLM_BASE_URL", "http://x/v1")
    monkeypatch.delenv("LLM_TIMEOUT", raising=False)
    monkeypatch.delenv("LLM_TIMEOUT_S", raising=False)
    assert LLMConfig.from_env().timeout_s == 4.0
    monkeypatch.setenv("LLM_TIMEOUT_S", "6")
    assert LLMConfig.from_env().timeout_s == 6.0
    monkeypatch.setenv("LLM_TIMEOUT", "2.5")
    assert LLMConfig.from_env().timeout_s == 2.5
