"""Golden set согласован с корпусом: ожидаемые пункты существуют, состав как в README."""

from eval.__main__ import load_golden, metrics, run_config


def test_golden_composition_and_clause_ids_exist(chunks):
    items = load_golden()
    ids = {cid for ch in chunks for cid in ch.clause_ids}
    assert 30 <= len(items) <= 40
    assert sum(i.kind == "trap" for i in items) == 8
    assert sum(i.kind == "injection" for i in items) == 3
    answerable = [i for i in items if not i.is_trap]
    assert sum(i.kind == "paraphrase" for i in answerable) * 2 >= len(answerable)
    assert all(i.expected and set(i.expected) <= ids for i in answerable)
    assert all(not i.expected for i in items if i.kind == "trap")
    assert len({i.question for i in items}) == len(items)


def test_bm25_eval_runs_and_literal_questions_hit(chunks):
    m = metrics(run_config("bm25", load_golden(), chunks, None))
    assert m["hit@5_literal"] == 1.0 and m["citation_valid"] == 1.0


def _llm_client(handler):
    import httpx

    from llm import LLMClient, LLMConfig
    return LLMClient(LLMConfig(base_url="http://llm.test/v1", api_key=None, model="m"),
                     transport=httpx.MockTransport(handler))


def _echo_first_doc(request):
    """Фейковый LLM: цитирует первое предложение первого <doc> реального вопроса."""
    import json
    import re

    import httpx
    user = json.loads(request.content)["messages"][-1]["content"]
    m = re.search(r'<doc name="([^"]+)" clause_id="([^"]+)"[^>]*>\n(.+?)\n</doc>', user, re.DOTALL)
    sentence = re.split(r"(?<=[.!?])\s", m.group(3))[0]
    body = {"sources": [{"doc": m.group(1), "clause_id": m.group(2), "quote": sentence}], "answer": sentence,
            "confidence": "high", "needs_escalation": False}
    return httpx.Response(200, json={"choices": [{"message": {"content": json.dumps(body, ensure_ascii=False)},
                                                  "finish_reason": "stop"}]})


def test_eval_llm_metrics_on_fake_llm(chunks):
    import httpx
    items = load_golden()
    ok = metrics(run_config("bm25", items, chunks, None, generator=_llm_client(_echo_first_doc), label="bm25+llm"))
    assert ok["config"] == "bm25+llm" and ok["n_llm_calls"] > 0
    assert ok["llm_fallback"] == 0.0 and ok["llm_verify_pass"] == 1.0
    bad = metrics(run_config("bm25", items, chunks, None,
                             generator=_llm_client(lambda r: httpx.Response(200, json={
                                 "choices": [{"message": {"content": "не json"}, "finish_reason": "stop"}]}))))
    assert bad["llm_fallback"] == 1.0
    assert bad["answer_acc"] == metrics(run_config("bm25", items, chunks, None))["answer_acc"]  # fallback = экстрактив
    plain = metrics(run_config("bm25", items, chunks, None))
    assert plain["n_llm_calls"] == 0 and plain["llm_fallback"] != plain["llm_fallback"]  # nan без LLM
