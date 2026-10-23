import os
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
CORPUS = ROOT / "testdata" / "corpus"

# Тесты не ходят в настоящий LLM, даже если он задан в окружении разработчика.
for var in ("LLM_BASE_URL", "LLM_API_KEY", "RAG_GATE_THRESHOLD", "RAG_RETRIEVER", "RAG_RERANKER"):
    os.environ.pop(var, None)
os.environ["RAG_CORPUS_DIR"] = str(CORPUS)


@pytest.fixture(scope="session")
def chunks():
    from chunker import load_corpus
    return load_corpus(CORPUS)


@pytest.fixture
def make_pipeline(chunks):
    from pipeline import Pipeline, Settings

    def _make(generator=None, **settings):
        return Pipeline(Settings(corpus_dir=str(CORPUS), **settings), chunks, generator=generator)

    return _make
