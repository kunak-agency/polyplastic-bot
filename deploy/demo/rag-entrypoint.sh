#!/bin/sh
# Демо-стек (docker-compose.demo.yml): выбор режима RAG по наличию весов и корпуса.
#
# Веса bge-m3 и bge-reranker-v2-m3 (~6 ГБ) лежат в томе /app/.cache (services/rag/.cache с хоста,
# скачиваются `make rag-models`). Нет весов — сервис стартует на BM25 без реранкера: отвечает
# так же с цитатой, но хуже понимает перефразы.
#
# Корпус: по умолчанию синтетический регламент из образа (testdata/corpus). RAG_CORPUS=real и
# каталог services/rag/corpus-real/ на хосте (в git его нет, документы заказчика) — настоящие
# регламенты, смонтированные томом.
set -u
hub=/app/.cache/hub

if [ "${RAG_RETRIEVER:-hybrid}" = "hybrid" ] || [ "${RAG_RERANKER:-bge}" = "bge" ]; then
  if [ ! -d "$hub/models--BAAI--bge-m3" ] || [ ! -d "$hub/models--BAAI--bge-reranker-v2-m3" ]; then
    echo "demo: WARN нет весов bge-m3/bge-reranker в services/rag/.cache — режим BM25 без реранкера." >&2
    echo "demo: WARN для рабочей конфигурации hybrid+bge: make rag-models (~6 ГБ), затем make demo." >&2
    export RAG_RETRIEVER=bm25 RAG_RERANKER=none
  fi
fi

if [ "${RAG_CORPUS:-synthetic}" = "real" ]; then
  if [ -n "$(ls -A /app/corpus-real 2>/dev/null)" ]; then
    export RAG_CORPUS_DIR=/app/corpus-real
  else
    echo "demo: WARN RAG_CORPUS=real, но services/rag/corpus-real пуст — синтетический корпус." >&2
  fi
fi

if [ -z "${LLM_BASE_URL:-}" ]; then
  echo "demo: LLM_BASE_URL пуст — ответ дословной цитатой пункта, без пересказа моделью"
fi
echo "demo: rag retriever=${RAG_RETRIEVER:-hybrid} reranker=${RAG_RERANKER:-bge} corpus=${RAG_CORPUS_DIR:-/app/testdata/corpus}"

exec uvicorn app:app --host 0.0.0.0 --port 8093 --workers 1
