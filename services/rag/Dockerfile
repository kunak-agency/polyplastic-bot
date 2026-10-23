# RAG-сервис Корсика. Веса bge-m3 и bge-reranker-v2-m3 на сборке НЕ скачиваются: они
# лежат в кэше Hugging Face (HF_HOME=/app/.cache), который монтируется томом
# (services/rag/.cache с хоста). Без тома и без RAG_RETRIEVER/RAG_RERANKER сервис
# работает на BM25 + экстрактивном ответе, как раньше.
FROM python:3.13-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

# OCR сканов: tesseract + русский (английский идёт в пакете tesseract-ocr)
RUN apt-get update \
 && apt-get install -y --no-install-recommends tesseract-ocr tesseract-ocr-rus \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
# torch из CPU-индекса: иначе на x86_64 pip притащит CUDA-колёса на несколько ГБ
RUN pip install torch==2.14.1 --index-url https://download.pytorch.org/whl/cpu
COPY requirements.txt .
RUN pip install -r requirements.txt

COPY *.py ./
COPY prompts ./prompts
COPY eval ./eval
COPY testdata ./testdata

RUN useradd --uid 10001 --no-create-home rag && mkdir -p /app/.cache && chown rag /app/.cache
USER rag

# Корпус по умолчанию — СИНТЕТИЧЕСКИЙ тестовый регламент. Настоящие документы монтируются
# томом и задаются через RAG_CORPUS_DIR.
ENV RAG_CORPUS_DIR=/app/testdata/corpus \
    HF_HOME=/app/.cache
EXPOSE 8093

# start-period большой: с RAG_RETRIEVER=hybrid модели грузятся на старте (~10 с из кэша)
HEALTHCHECK --interval=10s --timeout=3s --start-period=120s --retries=3 \
  CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8093/health', timeout=2).status == 200 else 1)"

# 0.0.0.0 внутри контейнера; наружу compose публикует только 127.0.0.1:8093
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8093", "--workers", "1"]
