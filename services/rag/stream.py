"""WS /stream: потоковое распознавание одного звонка — детектор речи Silero режет реплики, T-one
распознаёт их на лету.

Порт hack-ru-clinik backend/speech/speech.go (Session) и backend/cmd/speech/main.go (serve) без
изменений по сути; протокол совпадает с clinik docs/call-protocol.md, «Внутренние сервисы речи»:

  вход:  бинарные кадры PCM16LE моно 8 кГц любой длины (обычно 320 байт = 20 мс);
         текст {"type":"config","silence_ms":N} — пауза конца реплики (200–3000 мс);
         текст {"type":"flush"} — закрыть начатую реплику немедленно.
  выход: {"type":"speech_start"}, {"type":"partial","text":"…"}, {"type":"speech_end"},
         {"type":"final","text":"…","asr_ms":N}. Пустые финалы (шум, кашель) не отправляются.

Авторизация — заголовок «Authorization: Bearer <RAG_API_KEY>», проверяется здесь: HTTP-middleware
app.require_api_key на WebSocket не действует. Неверный ключ — закрытие 1008 до принятия соединения
(клиент получает отказ рукопожатия, HTTP 403). Нет модели VAD или T-one — принять и закрыть 1011
с причиной; остальной сервис работает.

Встроенный endpointing sherpa для T-one сломан (clinik docs/voice.md): конец реплики решает VAD.
"""

from __future__ import annotations

import asyncio
import hmac
import json
import logging
import os
import threading
import time
from pathlib import Path

import numpy as np
from fastapi import APIRouter, WebSocket, WebSocketDisconnect

import asr

log = logging.getLogger("rag.stream")

SAMPLE_RATE = 8000  # телефонный звук и вход T-one
VAD_RATE = 16000  # Silero VAD в sherpa-onnx работает только на 16 кГц
VAD_WINDOW = 512  # 32 мс на 16 кГц
PRE_ROLL_MS = 800  # звук до срабатывания детектора: начало фразы не теряется
WINDOW_MS = VAD_WINDOW * 1000 / VAD_RATE

_DEFAULT_VAD = Path(__file__).resolve().parent / ".cache" / "vad" / "silero_vad.onnx"


class StreamUnavailable(Exception):
    """Нет модели VAD или T-one — WS закрывается 1011."""


class Engine:
    """Общая на все звонки модель T-one. Детектор речи у каждого звонка свой: у него состояние.

    Отдельный экземпляр asr.Recognizer (тот же код загрузки, своя копия модели и свой замок):
    /transcribe держит замок на всё голосовое (до секунд), и звонок не должен ждать его."""

    def __init__(self, recognizer: asr.Recognizer | None = None, vad_model: str | Path | None = None):
        self.recognizer = recognizer or asr.Recognizer(threads=int(os.getenv("RAG_STREAM_ASR_THREADS", "2")))
        self.vad_model = Path(vad_model or os.getenv("RAG_VAD_MODEL") or _DEFAULT_VAD)
        self.threshold = float(os.getenv("RAG_VAD_THRESHOLD", "0.5"))
        self.min_speech_s = float(os.getenv("RAG_VAD_MIN_SPEECH_MS", "250")) / 1000
        self.silence_ms = int(os.getenv("RAG_STREAM_SILENCE_MS", "700"))
        self._load_lock = threading.Lock()
        self._rec = None

    @property
    def lock(self) -> threading.Lock:
        # потокобезопасность decode_stream на разных потоках не документирована — сериализуем
        return self.recognizer._lock

    def check(self) -> None:
        if not self.vad_model.is_file():
            raise StreamUnavailable("vad model missing")
        if not self.recognizer.available():
            raise StreamUnavailable("asr model missing")

    def load(self):
        """Грузит T-one и проверяет, что детектор создаётся (как NewEngine в clinik)."""
        self.check()
        with self._load_lock:
            if self._rec is None:
                with self.lock:
                    rec = self.recognizer._load()
                self.new_vad()
                self._rec = rec
                log.info("stream: VAD %s, T-one готовы", self.vad_model)
        return self._rec

    def new_vad(self):
        import sherpa_onnx

        cfg = sherpa_onnx.VadModelConfig()
        cfg.silero_vad.model = str(self.vad_model)
        cfg.silero_vad.threshold = self.threshold
        cfg.silero_vad.min_silence_duration = 0.1  # паузу конца реплики считаем сами — она меняется на ходу
        cfg.silero_vad.min_speech_duration = self.min_speech_s
        cfg.silero_vad.window_size = VAD_WINDOW
        cfg.silero_vad.max_speech_duration = 30
        cfg.sample_rate = VAD_RATE
        cfg.num_threads = 1
        cfg.provider = "cpu"
        return sherpa_onnx.VoiceActivityDetector(cfg, buffer_size_in_seconds=30)

    def new_session(self) -> "Session":
        rec = self.load()
        return Session(rec, self.lock, self.new_vad(), self.silence_ms)


class Session:
    """Распознавание одного звонка. Не потокобезопасна: звук подаёт один поток за раз."""

    def __init__(self, rec, lock: threading.Lock, vad, silence_ms: int = 700):
        self.rec = rec
        self.lock = lock
        self.vad = vad
        self.silence_ms = silence_ms
        self.vad_buf = np.zeros(0, dtype=np.float32)  # 16 кГц, ждёт полного окна
        self.last = np.float32(0)  # последний отсчёт 8 кГц — для интерполяции на стыке кадров
        self.pre_roll = np.zeros(0, dtype=np.float32)  # 8 кГц, хвост звука до начала речи
        self.stream = None
        self.in_speech = False
        self.quiet_ms = 0.0  # тишина с последнего окна речи
        self.partial = ""

    def set_silence(self, ms: int) -> None:
        if 200 <= ms <= 3000:
            self.silence_ms = ms

    def feed(self, pcm: bytes) -> list[dict]:
        samples = np.frombuffer(pcm[: len(pcm) // 2 * 2], dtype="<i2").astype(np.float32) / 32768
        out: list[dict] = []
        if self.in_speech:
            out += self._decode(samples)
        else:
            self.pre_roll = np.concatenate([self.pre_roll, samples])[-PRE_ROLL_MS * SAMPLE_RATE // 1000:]
        if len(samples):
            # детектор — на 16 кГц: линейная интерполяция ×2; распознаватель получает исходные 8 кГц
            prev = np.concatenate([[self.last], samples[:-1]]).astype(np.float32)
            up = np.empty(2 * len(samples), dtype=np.float32)
            up[0::2] = (prev + samples) / 2
            up[1::2] = samples
            self.last = samples[-1]
            self.vad_buf = np.concatenate([self.vad_buf, up])
        while len(self.vad_buf) >= VAD_WINDOW:
            window, self.vad_buf = self.vad_buf[:VAD_WINDOW], self.vad_buf[VAD_WINDOW:]
            self.vad.accept_waveform(window)
            speech = self.vad.is_speech_detected()
            while not self.vad.empty():  # готовые сегменты не нужны — звук уже у распознавателя
                self.vad.pop()
            if speech and not self.in_speech:
                self.in_speech, self.quiet_ms = True, 0.0
                self.stream = self.rec.create_stream()
                out.append({"type": "speech_start"})
                out += self._decode(self.pre_roll)
                self.pre_roll = np.zeros(0, dtype=np.float32)
            elif speech:
                self.quiet_ms = 0.0
            elif self.in_speech:
                self.quiet_ms += WINDOW_MS
                if self.quiet_ms >= self.silence_ms:
                    out += self._finish()
        return out

    def flush(self) -> list[dict]:
        """Звонок кончается посреди реплики: дораспознать то, что есть."""
        return self._finish() if self.in_speech else []

    def _decode(self, samples: np.ndarray) -> list[dict]:
        if len(samples) == 0:
            return []
        with self.lock:
            self.stream.accept_waveform(SAMPLE_RATE, samples)
            while self.rec.is_ready(self.stream):
                self.rec.decode_stream(self.stream)
            text = self.rec.get_result(self.stream)
        if not text or text == self.partial:
            return []
        self.partial = text
        return [{"type": "partial", "text": text}]

    def _finish(self) -> list[dict]:
        started = time.perf_counter()
        out: list[dict] = [{"type": "speech_end"}]
        with self.lock:
            # хвост тишины, чтобы последние кадры прошли через модель, затем конец ввода
            self.stream.accept_waveform(SAMPLE_RATE, np.zeros(SAMPLE_RATE // 5, dtype=np.float32))
            self.stream.input_finished()
            while self.rec.is_ready(self.stream):
                self.rec.decode_stream(self.stream)
            text = self.rec.get_result(self.stream)
        self.stream, self.in_speech, self.partial, self.quiet_ms = None, False, "", 0.0
        self.vad.reset()
        text = " ".join((text or "").split())
        if text:  # пустой финал (шум, кашель) не отдаём
            out.append({"type": "final", "text": text,
                        "asr_ms": int((time.perf_counter() - started) * 1000)})
        return out


# --- WebSocket ---

router = APIRouter()
_engine: Engine | None = None
_engine_lock = threading.Lock()
_active = 0
MAX_SESSIONS = int(os.getenv("RAG_STREAM_MAX_SESSIONS", "4"))


def get_engine() -> Engine:
    global _engine
    with _engine_lock:
        if _engine is None:
            _engine = Engine()
        return _engine


def warmup() -> None:
    """Фоновый прогрев на старте: модели есть — грузим, нет — молча (WS ответит 1011)."""
    try:
        eng = get_engine()
        eng.check()
        sess = eng.new_session()
        sess.feed(bytes(SAMPLE_RATE))  # 0,5 с тишины
    except StreamUnavailable as exc:
        log.warning("stream: распознавание звонков выключено: %s", exc)
    except Exception as exc:  # без звонков сервис всё равно нужен
        log.warning("stream: прогрев не удался: %s", exc)


def _authorized(ws: WebSocket) -> bool:
    key = os.getenv("RAG_API_KEY", "").strip()
    if not key:
        return True
    scheme, _, token = ws.headers.get("authorization", "").partition(" ")
    return scheme.lower() == "bearer" and hmac.compare_digest(token.strip().encode(), key.encode())


@router.websocket("/stream")
async def stream_ws(ws: WebSocket) -> None:
    global _active
    if not _authorized(ws):
        await ws.close(code=1008)
        return
    try:
        sess = await asyncio.to_thread(lambda: get_engine().new_session())
    except StreamUnavailable as exc:
        await ws.accept()
        await ws.close(code=1011, reason=str(exc))
        return
    except Exception as exc:
        log.warning("stream: сессия не создана: %s", exc)
        await ws.accept()
        await ws.close(code=1011, reason="asr init failed")
        return
    if _active >= MAX_SESSIONS:
        await ws.accept()
        await ws.close(code=1013, reason="too many sessions")
        return
    _active += 1
    await ws.accept()
    started, finals = time.perf_counter(), 0
    try:
        while True:
            msg = await ws.receive()
            if msg["type"] == "websocket.disconnect":
                break
            events: list[dict] = []
            data, text = msg.get("bytes"), msg.get("text")
            if data is not None:
                if len(data) % 2:
                    continue
                events = await asyncio.to_thread(sess.feed, data)
            elif text is not None:
                try:
                    cmd = json.loads(text)
                except ValueError:
                    continue
                if not isinstance(cmd, dict):
                    continue
                if cmd.get("type") == "config":
                    try:
                        sess.set_silence(int(cmd.get("silence_ms") or 0))
                    except (TypeError, ValueError):
                        pass
                elif cmd.get("type") == "flush":
                    events = await asyncio.to_thread(sess.flush)
            for ev in events:
                if ev["type"] == "final":
                    finals += 1
                    # сам текст в лог не пишем: это речь сотрудника
                    log.info("stream: final %d символов, asr_ms=%d", len(ev["text"]), ev["asr_ms"])
                await ws.send_json(ev)
    except WebSocketDisconnect:
        pass
    finally:
        _active -= 1
        log.info("stream: сессия закрыта, %.1f с, реплик %d", time.perf_counter() - started, finals)
