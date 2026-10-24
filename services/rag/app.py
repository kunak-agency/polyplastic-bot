"""RAG-сервис Корсика: ответы по регламентам с цитатой пункта и отказом без источника.

GET  /health -> {"status":"ok"}
POST /ask    {"question", "site", "trace_id", "address_form"?} -> ответ с источниками или отказ
POST /flags  {"text", "context", "survey_question"?} -> флаги F0–F10, тон, цитата, degraded
POST /documents (multipart: file, doc?) -> {doc_id, chunks, clauses, low_confidence_pages}
GET  /documents -> {"items": [...], "total": N}
POST /transcribe (multipart: audio) -> {text, lang, duration_s, confidence} (asr.py, README «Голос»)

Контракт /ask и /flags: agency/system/api_contract.md. /documents описан в README сервиса.
"""

from __future__ import annotations

import logging
import os
from contextlib import asynccontextmanager
from typing import Literal

import threading

from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from pydantic import BaseModel, Field, model_validator

import asr
from chunker import load_corpus
from documents import DocRecord, DocumentStore, make_doc_id
from parsers import CorruptDocument, UnsupportedDocument, parse_document
from pipeline import Pipeline, Settings

log = logging.getLogger("rag")
logging.basicConfig(level=os.getenv("RAG_LOG_LEVEL", "INFO"),
                    format="%(asctime)s %(levelname)s %(name)s: %(message)s")

MAX_QUESTION_CHARS = int(os.getenv("RAG_MAX_QUESTION_CHARS", "2000"))
MAX_FLAGS_TEXT_CHARS = int(os.getenv("RAG_MAX_FLAGS_TEXT_CHARS", "5000"))
MAX_UPLOAD_BYTES = int(float(os.getenv("RAG_MAX_UPLOAD_MB", "25")) * 1024 * 1024)


class State:
    pipeline: Pipeline | None = None
    store: DocumentStore | None = None
    recognizer = asr.Recognizer()  # T-one грузится при первом /transcribe или на прогреве
    upload_lock = threading.Lock()  # загрузки по одной: реестр и индекс меняются согласованно


state = State()


def build_pipeline() -> tuple[Pipeline, list]:
    settings = Settings.from_env()
    chunks = load_corpus(settings.corpus_dir)
    if not chunks:
        log.warning("корпус %s пуст: все вопросы получат отказ no_source", settings.corpus_dir)
    if any("синтет" in (c.doc + (c.title or "")).lower() for c in chunks):
        log.warning("в индексе СИНТЕТИЧЕСКИЕ документы (%s) — не для реальных ответов", settings.corpus_dir)
    pipeline = Pipeline.from_env(chunks)
    log.info("index ready: %d chunks, retriever=%s, reranker=%s, gate=%.2f, llm=%s",
             len(chunks), settings.retriever, settings.reranker, settings.gate_threshold,
             "on" if pipeline.generator else "off (extractive)")
    return pipeline, chunks


@asynccontextmanager
async def lifespan(app: FastAPI):
    state.pipeline, chunks = build_pipeline()
    state.store = DocumentStore(chunks)
    # Прогрев до yield: uvicorn открывает порт только после старта lifespan, так что /health
    # отвечает лишь когда модели и LLM прогреты и первый /ask не платит за холодный старт.
    if os.getenv("RAG_WARMUP", "1") != "0":
        state.pipeline.warmup()
        if os.getenv("RAG_ASR_WARMUP", "1") != "0":
            try:
                state.recognizer.warmup()
            except Exception as exc:  # без распознавания сервис всё равно нужен
                log.warning("asr: прогрев не удался: %s", exc)
    yield
    gen = state.pipeline.generator if state.pipeline else None
    if gen is not None and hasattr(gen, "close"):
        gen.close()
    state.pipeline = None
    state.store = None


app = FastAPI(title="rag", lifespan=lifespan)


# --- /ask ---

class AskRequest(BaseModel):
    question: str = Field(min_length=1, max_length=MAX_QUESTION_CHARS)
    site: Literal["GTP", "GrPP", "Luzhniki"] | None = None
    trace_id: str | None = Field(default=None, max_length=128)
    # форма обращения для промпта rag-answer v2; null или нет поля — «вы»
    address_form: Literal["ty", "vy"] | None = None


class SourceOut(BaseModel):
    doc: str
    clause_id: str
    page: int | None
    quote: str


class AskResponse(BaseModel):
    answer: str | None
    sources: list[SourceOut]
    confidence: Literal["high", "medium", "low"]
    needs_escalation: bool
    topic: Literal["hr", "legal", "security", "other"]
    abstain_reason: Literal["no_source", "below_threshold", "routed_topic"] | None

    @model_validator(mode="after")
    def _escalation_has_no_answer(self):
        if self.needs_escalation and (self.answer is not None or self.sources):
            raise ValueError("needs_escalation=true требует answer=null и пустых sources")
        if not self.needs_escalation and (self.answer is None or not self.sources):
            raise ValueError("ответ без эскалации требует answer и хотя бы один источник")
        return self


@app.get("/health")
def health():
    return {"status": "ok"}


@app.post("/ask", response_model=AskResponse)
def ask(req: AskRequest) -> AskResponse:
    question = req.question.strip()
    if not question:
        return AskResponse(answer=None, sources=[], confidence="low", needs_escalation=True,
                           topic="other", abstain_reason="no_source")
    res = state.pipeline.ask(question, site=req.site, trace_id=req.trace_id, address_form=req.address_form)
    return AskResponse(
        answer=res.answer,
        sources=[SourceOut(doc=s.doc, clause_id=s.clause_id, page=s.page, quote=s.quote) for s in res.sources],
        confidence=res.confidence,
        needs_escalation=res.needs_escalation,
        topic=res.topic,
        abstain_reason=res.abstain_reason,
    )


# --- /documents (загрузка HR) ---

class DocumentOut(BaseModel):
    doc_id: str
    chunks: int
    clauses: int
    low_confidence_pages: list[int]


class DocumentItem(DocumentOut):
    doc: str
    filename: str | None
    kind: Literal["corpus", "pdf", "pptx", "xlsx", "text"]
    source: Literal["corpus", "upload"]
    uploaded_at: str


class DocumentList(BaseModel):
    items: list[DocumentItem]
    total: int


@app.post("/documents", response_model=DocumentOut)
def upload_document(file: UploadFile = File(...), doc: str | None = Form(default=None, max_length=200)) -> DocumentOut:
    filename = os.path.basename(file.filename or "")[:255]
    data = file.file.read(MAX_UPLOAD_BYTES + 1)
    if len(data) > MAX_UPLOAD_BYTES:
        raise HTTPException(status_code=413, detail=f"файл больше {MAX_UPLOAD_BYTES // (1024 * 1024)} МБ")
    if not data:
        raise HTTPException(status_code=422, detail="пустой файл")
    try:
        parsed = parse_document(filename, data, doc=doc)
    except UnsupportedDocument as exc:
        raise HTTPException(status_code=415, detail=str(exc)) from exc
    except CorruptDocument as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    except Exception as exc:  # битый файл: наружу без деталей и без содержимого
        log.warning("parse failed for %s: %s", filename, type(exc).__name__)
        raise HTTPException(status_code=422, detail="не удалось разобрать документ") from exc
    if not parsed.chunks:
        raise HTTPException(status_code=422, detail="в документе не найден текст")
    rec = DocRecord(doc_id=make_doc_id(parsed.doc, data), doc=parsed.doc, filename=filename, kind=parsed.kind,
                    source="upload", chunks=parsed.chunks, low_confidence_pages=parsed.low_confidence_pages)
    with state.upload_lock:
        state.pipeline.replace_chunks(state.store.put(rec))
    log.info("document %s (%s) indexed: %d chunks, %d clauses, low_conf_pages=%s", rec.doc_id, rec.kind,
             len(rec.chunks), rec.clauses, rec.low_confidence_pages)
    return DocumentOut(doc_id=rec.doc_id, chunks=len(rec.chunks), clauses=rec.clauses,
                       low_confidence_pages=rec.low_confidence_pages)


@app.get("/documents", response_model=DocumentList)
def list_documents() -> DocumentList:
    items = [DocumentItem(**r.summary()) for r in state.store.list()]
    return DocumentList(items=items, total=len(items))


# --- /flags (разметка F0–F10, flags.py) ---

class FlagsRequest(BaseModel):
    text: str = Field(max_length=MAX_FLAGS_TEXT_CHARS)
    # контракт: pulse_d5|free_text|voice; пульсы бывают и на другие дни, поэтому pulse_dN
    context: str = Field(default="free_text", pattern=r"^(pulse_d\d{1,3}|free_text|voice)$")
    # вопрос опроса, к которому относится ответ (промпт flags-classify, {survey_question})
    survey_question: str | None = Field(default=None, max_length=500)


class FlagOut(BaseModel):
    code: Literal["F0", "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"]
    severity: int = Field(ge=1, le=3)
    confidence: float = Field(ge=0.0, le=1.0)


class FlagsResponse(BaseModel):
    flags: list[FlagOut]
    sentiment: Literal["neg", "neu", "pos"]
    quote: str
    # true — LLM не задан, не ответил или дважды ответил не по схеме: «не размечено», а не «флагов нет»
    degraded: bool = False


@app.post("/flags", response_model=FlagsResponse)
def flags(req: FlagsRequest) -> FlagsResponse:
    # Regex-триггеры живут в Go; маршрут и порог решает Go по data/taxonomy.yaml.
    from flags import get_classifier

    res = get_classifier().classify(req.text, req.context, req.survey_question)
    log.info("flags: context=%s → %s%s", req.context, [f["code"] for f in res.flags] or "[]",
             " (degraded)" if res.degraded else "")
    return FlagsResponse(**res.as_dict())


# --- /transcribe (голосовые новичков, asr.py) ---

class TranscribeResponse(BaseModel):
    text: str
    lang: str
    duration_s: float
    confidence: float | None


@app.post("/transcribe", response_model=TranscribeResponse)
def transcribe(audio: UploadFile = File(...)) -> TranscribeResponse:
    data = audio.file.read(asr.MAX_AUDIO_BYTES + 1)
    if len(data) > asr.MAX_AUDIO_BYTES:
        raise HTTPException(status_code=413, detail=f"файл больше {asr.MAX_AUDIO_BYTES // (1024 * 1024)} МБ")
    try:
        res = asr.transcribe(data, state.recognizer)
    except asr.AudioTooLong as exc:
        raise HTTPException(status_code=413, detail=str(exc)) from exc
    except asr.AudioError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    except asr.ASRUnavailable as exc:
        log.warning("asr: распознавание недоступно: %s", exc)
        raise HTTPException(status_code=503, detail="распознавание недоступно") from exc
    return TranscribeResponse(text=res.text, lang=res.lang, duration_s=res.duration_s, confidence=res.confidence)
