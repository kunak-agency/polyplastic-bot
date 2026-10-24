"""POST /tts: синтез речи для звонка Корсику (Google Cloud TTS, запасной — Silero v5_5_ru).

Google включается ключом RAG_GOOGLE_TTS_KEY (голос RAG_GOOGLE_TTS_VOICE, по умолчанию
ru-RU-Wavenet-D): все предложения запрашиваются параллельно, отдаются по порядку. Если Google не
ответил на предложение — оно синтезируется Silero, звонок не замолкает.

{"text": str ≤ 1000, "speaker"?: str} -> audio/L16;rate=8000;channels=1 — сырой PCM16LE моно 8 кГц
потоком: один HTTP-чанк на предложение, первое уходит до синтеза следующих.

Порт hack-ru-clinik services/tts/app.py без изменений по сути. Отличия: живёт внутри RAG под общим
Bearer-middleware; модель грузится в фоне (пока не готова — 503), веса не качаются из кода —
их кладёт /workspace/restore.sh на поде (RAG_TTS_MODEL, по умолчанию .cache/silero-tts/v5_5_ru.pt);
torch.set_num_threads не трогаем — он общий с реранкером /ask.

ЛИЦЕНЗИЯ: Silero TTS v5_5_ru — CC BY-NC-SA 4.0, только для демо. Для пилота заменить
(MIT-вариант или коммерческая лицензия Silero).
"""

from __future__ import annotations

import logging
import os
import re
import base64
import json
import threading
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Iterator

from fastapi import APIRouter, HTTPException
from fastapi.responses import StreamingResponse
from num2words import num2words
from pydantic import BaseModel, Field

from tts_normalize import normalize, split_sentences

log = logging.getLogger("rag.tts")

SAMPLE_RATE = 8000  # по умолчанию; звонок просит 24000 — полное качество голоса
RATES = (8000, 24000, 48000)  # что умеют и Silero, и Google LINEAR16


def media_type(rate: int) -> str:
    return f"audio/L16;rate={rate};channels=1"
MAX_TEXT_CHARS = 1000
FALLBACK_SPEAKER = "eugene"
WARMUP_TEXT = "Здравствуйте! Это проверка синтеза речи."

_DEFAULT_MODEL = Path(__file__).resolve().parent / ".cache" / "silero-tts" / "v5_5_ru.pt"


class Engine:
    def __init__(self):
        self.model_path = Path(os.getenv("RAG_TTS_MODEL") or _DEFAULT_MODEL)
        self.device = os.getenv("RAG_TTS_DEVICE", "cpu").strip() or "cpu"
        self.speaker = os.getenv("RAG_TTS_SPEAKER", "aidar").strip() or "aidar"
        self.model = None
        self.speakers: list[str] = []
        self.ready = False
        self.error: str | None = None
        self.lock = threading.Lock()  # потокобезопасность apply_tts не документирована

    def load(self) -> None:
        if not self.model_path.is_file():
            raise FileNotFoundError(f"нет весов Silero TTS: {self.model_path}")
        import torch

        t0 = time.perf_counter()
        model = torch.package.PackageImporter(str(self.model_path)).load_pickle("tts_models", "model")
        model.to(torch.device(self.device))
        self.model = model
        self.speakers = list(getattr(model, "speakers", []) or [])
        if self.speakers and self.speaker not in self.speakers:
            log.warning("tts: голоса %s нет в модели, беру %s", self.speaker, FALLBACK_SPEAKER)
            self.speaker = FALLBACK_SPEAKER
        t1 = time.perf_counter()
        self.synth(WARMUP_TEXT, self.speaker)
        self.ready = True
        log.info("tts: %s загружена за %.2f с, прогрев %.2f с, голос=%s, device=%s",
                 self.model_path.name, t1 - t0, time.perf_counter() - t1, self.speaker, self.device)

    def synth(self, text: str, speaker: str, rate: int = SAMPLE_RATE) -> bytes:
        import torch

        with self.lock, torch.inference_mode():
            audio = self.model.apply_tts(text=text, speaker=speaker, sample_rate=rate,
                                         put_accent=True, put_yo=True)
        pcm = (audio.detach().cpu().clamp(-1.0, 1.0) * 32767.0).to(torch.int16)
        return pcm.numpy().astype("<i2", copy=False).tobytes()  # PCM16LE


engine = Engine()


class GoogleTTS:
    """Cloud Text-to-Speech REST v1: LINEAR16 8 кГц, WAV-заголовок срезаем."""

    URL = "https://texttospeech.googleapis.com/v1/text:synthesize"

    def __init__(self):
        self.key = os.getenv("RAG_GOOGLE_TTS_KEY", "").strip()
        self.voice = os.getenv("RAG_GOOGLE_TTS_VOICE", "ru-RU-Wavenet-D").strip() or "ru-RU-Wavenet-D"
        self.timeout = float(os.getenv("RAG_GOOGLE_TTS_TIMEOUT", "4"))
        self.pool = ThreadPoolExecutor(max_workers=8, thread_name_prefix="gtts")

    @property
    def enabled(self) -> bool:
        return bool(self.key)

    def synth(self, text: str, rate: int = SAMPLE_RATE) -> bytes:
        body = json.dumps({
            "input": {"text": text},
            "voice": {"languageCode": self.voice[:5], "name": self.voice},
            "audioConfig": {"audioEncoding": "LINEAR16", "sampleRateHertz": rate,
                            "effectsProfileId": ["headphone-class-device"]},
        }).encode()
        req = urllib.request.Request(self.URL, data=body, headers={
            "Content-Type": "application/json", "X-Goog-Api-Key": self.key})
        with urllib.request.urlopen(req, timeout=self.timeout) as resp:
            audio = base64.b64decode(json.load(resp)["audioContent"])
        return strip_wav(audio)


def strip_wav(audio: bytes) -> bytes:
    """LINEAR16 от Google приходит WAV-файлом: отдаём только данные чанка data."""
    if audio[:4] != b"RIFF":
        return audio
    i = audio.find(b"data", 12)
    return audio[i + 8:] if i >= 0 else audio[44:]


google = GoogleTTS()


def warmup() -> None:
    """Фоновая загрузка на старте; без весов сервис работает, /tts отвечает 503."""
    try:
        engine.load()
    except Exception as exc:
        engine.error = str(exc)
        log.warning("tts: синтез выключен: %s", exc)


_CLAUSE_MULTI = re.compile(r"\b\d+(?:\.\d+){2,}\b")  # 6.7.4
_CLAUSE_AFTER_WORD = re.compile(r"\b(пункт\w*|п\.|раздел\w*|глав\w*)\s*(\d+(?:\.\d+)+)\b", re.I)  # пункт 6.7


def speakable_clauses(text: str) -> str:
    """Номера пунктов регламента — «шесть точка семь точка четыре». Без этого общая нормализация
    clinik читает «6.7.4» как дробь «шесть целых семь десятых» и режет фразу по точке."""
    def dots(num: str) -> str:
        return " точка ".join(num2words(int(p), lang="ru") for p in num.split("."))

    text = _CLAUSE_AFTER_WORD.sub(lambda m: f"{m.group(1)} {dots(m.group(2))}", text)
    return _CLAUSE_MULTI.sub(lambda m: dots(m.group(0)), text)


router = APIRouter()


class TTSRequest(BaseModel):
    text: str = Field(max_length=MAX_TEXT_CHARS)
    speaker: str | None = Field(default=None, max_length=32)
    sample_rate: int = SAMPLE_RATE
    engine: str | None = Field(default=None, pattern="^(google|silero)$")  # None — Google, если есть ключ


@router.post("/tts")
def tts(req: TTSRequest):
    if not engine.ready and not google.enabled:
        raise HTTPException(status_code=503, detail="tts model is not ready")
    if not req.text.strip():
        raise HTTPException(status_code=400, detail="text is empty")
    rate = req.sample_rate
    if rate not in RATES:
        raise HTTPException(status_code=400, detail=f"sample_rate: one of {RATES}")
    speaker = req.speaker or engine.speaker
    if engine.speakers and speaker not in engine.speakers:
        if google.enabled:
            speaker = engine.speaker  # голос Silero нужен только запасному пути
        else:
            raise HTTPException(status_code=400, detail=f"unknown speaker; available: {engine.speakers}")
    chunks = split_sentences(normalize(speakable_clauses(req.text)))
    if not chunks:
        raise HTTPException(status_code=400, detail="text has nothing to pronounce")
    # Google: все предложения сразу, параллельно; ждём по порядку.
    use_google = google.enabled and req.engine != "silero"
    if not use_google and not engine.ready:
        raise HTTPException(status_code=503, detail="silero model is not ready")
    futures = [google.pool.submit(google.synth, c, rate) for c in chunks] if use_google else None

    def one(i: int, chunk: str) -> bytes:
        if futures is not None:
            try:
                return futures[i].result(timeout=google.timeout + 1)
            except Exception as exc:
                log.warning("tts: google не ответил на предложение %d/%d: %s — Silero", i + 1, len(chunks), exc)
                if not engine.ready:
                    raise
        return engine.synth(chunk, speaker, rate)

    def stream() -> Iterator[bytes]:
        t0 = time.perf_counter()
        total = 0
        for i, chunk in enumerate(chunks):
            try:
                pcm = one(i, chunk)
            except Exception:  # не валим весь поток из-за одного предложения
                log.exception("tts: предложение %d/%d (%d символов) не синтезировалось", i + 1, len(chunks), len(chunk))
                continue
            total += len(pcm)
            if i == 0:
                log.info("tts: первый чанк за %.0f мс", (time.perf_counter() - t0) * 1000)
            yield pcm
        log.info("tts: %d чанков, %.2f с звука за %.0f мс",
                 len(chunks), total / 2 / rate, (time.perf_counter() - t0) * 1000)

    # синхронный генератор Starlette гоняет в threadpool, каждый yield — отдельный HTTP-чанк
    return StreamingResponse(stream(), media_type=media_type(rate), headers={"Cache-Control": "no-store"})
