"""Распознавание голосовых новичков: ffmpeg → PCM 8 кГц → T-one (sherpa-onnx) на CPU.

Всё локально: модель лежит в RAG_ASR_MODEL_DIR (по умолчанию .cache/t-one/…), звук и текст
никуда не уходят (п. 4.3.2 Положения). Выбор движка и замеры — README.md, раздел «Голос».

T-one — потоковая CTC-модель для русского, обучена на телефонии (8 кГц). Выдаёт строчные
буквы без пунктуации и цифр; вероятностей sherpa-onnx для неё не отдаёт, поэтому confidence
в ответе — null.
"""

from __future__ import annotations

import logging
import os
import shutil
import subprocess
import tempfile
import threading
import time
from dataclasses import dataclass
from pathlib import Path

import numpy as np

log = logging.getLogger("rag.asr")

SAMPLE_RATE = 8000  # T-one обучена на 8 кГц
MAX_DURATION_S = float(os.getenv("RAG_ASR_MAX_SECONDS", "120"))
MAX_AUDIO_BYTES = int(float(os.getenv("RAG_ASR_MAX_MB", "20")) * 1024 * 1024)
FFMPEG_TIMEOUT_S = 30
# хвост тишины: CTC дописывает последние слова, только когда за ними есть ещё кадры
TAIL_PAD_S = 1.0
# тише этого (RMS) — тишина: в модель не подаём, сразу пустой текст
SILENCE_RMS = 0.003

_DEFAULT_MODEL = Path(__file__).resolve().parent / ".cache" / "t-one" / "sherpa-onnx-streaming-t-one-russian-2025-09-08"


class AudioError(Exception):
    """Файл не разобрался как звук (ffmpeg не смог) — ответ 422."""


class AudioTooLong(Exception):
    """Длиннее MAX_DURATION_S — ответ 413."""


class ASRUnavailable(Exception):
    """Нет ffmpeg или модели — ответ 503: сервис жив, распознавание выключено."""


@dataclass
class Transcript:
    text: str
    duration_s: float
    lang: str = "ru"
    confidence: float | None = None


def ffmpeg_bin() -> str | None:
    return os.getenv("RAG_FFMPEG") or shutil.which("ffmpeg")


def decode(data: bytes) -> np.ndarray:
    """Любой формат, который знает ffmpeg (ogg/opus, m4a, mp3, wav…), → float32 моно 8 кГц.

    Читаем не больше MAX_DURATION_S + 1 с: длинный файл не декодируется целиком, чтобы
    отказать по длительности."""
    ff = ffmpeg_bin()
    if not ff:
        raise ASRUnavailable("ffmpeg не найден (RAG_FFMPEG или PATH)")
    # Через временный файл, а не stdin: у m4a (mp4) индекс moov часто в конце, из трубы ffmpeg
    # его не находит.
    with tempfile.NamedTemporaryFile(prefix="voice-", suffix=".bin") as tmp:
        tmp.write(data)
        tmp.flush()
        cmd = [ff, "-nostdin", "-loglevel", "error", "-i", tmp.name, "-t", str(MAX_DURATION_S + 1),
               "-ac", "1", "-ar", str(SAMPLE_RATE), "-f", "s16le", "pipe:1"]
        try:
            proc = subprocess.run(cmd, capture_output=True, timeout=FFMPEG_TIMEOUT_S)
        except subprocess.TimeoutExpired as exc:
            raise AudioError("ffmpeg не уложился в срок") from exc
    if proc.returncode != 0 or not proc.stdout:
        raise AudioError("не удалось разобрать звук")
    pcm = np.frombuffer(proc.stdout[: len(proc.stdout) // 2 * 2], dtype=np.int16)
    return pcm.astype(np.float32) / 32768.0


class Recognizer:
    """T-one через sherpa-onnx. Один распознаватель на процесс, разбор под замком: потокобезопасность
    decode_stream на разных потоках не документирована (так же в hack-ru-clinik)."""

    def __init__(self, model_dir: str | Path | None = None, threads: int | None = None):
        self.model_dir = Path(model_dir or os.getenv("RAG_ASR_MODEL_DIR") or _DEFAULT_MODEL)
        self.threads = threads or int(os.getenv("RAG_ASR_THREADS", "4"))
        self._rec = None
        self._lock = threading.Lock()

    def available(self) -> bool:
        return (self.model_dir / "model.onnx").is_file() and (self.model_dir / "tokens.txt").is_file()

    def _load(self):
        if self._rec is not None:
            return self._rec
        if not self.available():
            raise ASRUnavailable(f"нет модели T-one в {self.model_dir} (README.md, «Голос»)")
        import sherpa_onnx

        t = time.perf_counter()
        self._rec = sherpa_onnx.OnlineRecognizer.from_t_one_ctc(
            model=str(self.model_dir / "model.onnx"), tokens=str(self.model_dir / "tokens.txt"),
            num_threads=self.threads, sample_rate=SAMPLE_RATE, decoding_method="greedy_search")
        log.info("asr: T-one загружена за %.2f с (%s, threads=%d)", time.perf_counter() - t, self.model_dir, self.threads)
        return self._rec

    def warmup(self) -> None:
        if self.available():
            self.recognize(np.zeros(SAMPLE_RATE // 2, dtype=np.float32) + 0.01)

    def recognize(self, pcm: np.ndarray) -> str:
        with self._lock:
            rec = self._load()
            st = rec.create_stream()
            st.accept_waveform(SAMPLE_RATE, pcm)
            st.accept_waveform(SAMPLE_RATE, np.zeros(int(SAMPLE_RATE * TAIL_PAD_S), dtype=np.float32))
            st.input_finished()
            while rec.is_ready(st):
                rec.decode_stream(st)
            return " ".join(rec.get_result(st).split())


def transcribe(data: bytes, recognizer: Recognizer) -> Transcript:
    if not data:
        raise AudioError("пустой файл")
    pcm = decode(data)
    duration = len(pcm) / SAMPLE_RATE
    if duration > MAX_DURATION_S:
        raise AudioTooLong(f"голосовое длиннее {int(MAX_DURATION_S)} с")
    if duration == 0 or float(np.sqrt(np.mean(pcm * pcm))) < SILENCE_RMS:
        return Transcript(text="", duration_s=round(duration, 2))
    t = time.perf_counter()
    text = recognizer.recognize(pcm)
    # сам текст в лог не пишем: это переписка новичка
    log.info("asr: %.1f с звука за %.2f с, %d символов", duration, time.perf_counter() - t, len(text))
    return Transcript(text=text, duration_s=round(duration, 2))
