#!/usr/bin/env bash
# Локальный запуск RAG-сервиса без Docker: ./run.sh
# Оценка на golden set:                    ./run.sh eval [--configs bm25,final] [--sweep] [--rerank-top 8] [-v]
# env: RAG_HOST, RAG_PORT, RAG_CORPUS_DIR, RAG_RETRIEVER, RAG_RERANKER, RAG_GATE_THRESHOLD,
#      RAG_DEVICE (auto: mps на Mac-хосте), RAG_RERANK_TOP, RAG_TORCH_THREADS, RAG_WARMUP,
#      HF_HOME (по умолчанию services/rag/.cache), LLM_BASE_URL, LLM_API_KEY, LLM_MODEL, LLM_TIMEOUT (см. README.md)
# Порт открывается после прогрева моделей и LLM: /health отвечает, когда сервис готов.
set -euo pipefail
cd "$(dirname "$0")"
if [ ! -x .venv/bin/python ]; then
  echo "Нет .venv — см. README.md (python3.13 -m venv .venv && .venv/bin/pip install -r requirements-dev.txt)" >&2
  exit 1
fi
source .venv/bin/activate
export HF_HOME="${HF_HOME:-$PWD/.cache}"
# mps не умеет часть операций torch — пусть они уходят на CPU, а не роняют запрос
export PYTORCH_ENABLE_MPS_FALLBACK="${PYTORCH_ENABLE_MPS_FALLBACK:-1}"
if [ "${1:-}" = "eval" ]; then
  shift
  exec python -m eval "$@"
fi
exec uvicorn app:app --host "${RAG_HOST:-127.0.0.1}" --port "${RAG_PORT:-8093}" --workers 1 --log-level "${RAG_UVICORN_LOG_LEVEL:-info}"
