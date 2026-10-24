"""POST /transcribe: голосовые новичков (asr.py). voice_ru.wav — синтетика `say -v Milena` → ffmpeg,
8 кГц моно: «Сколько дней отпуска положено после шести месяцев работы?».

Тесты с настоящей T-one пропускаются, если модели нет в .cache (README.md, «Голос»)."""

import io
import math
import shutil
import subprocess
import time
import wave
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

import app as app_module
import asr
from app import app

VOICE = Path(__file__).parent / "data" / "voice_ru.wav"

needs_ffmpeg = pytest.mark.skipif(asr.ffmpeg_bin() is None, reason="нет ffmpeg")
needs_model = pytest.mark.skipif(not asr.Recognizer().available() or asr.ffmpeg_bin() is None,
                                 reason="нет модели T-one или ffmpeg")


def wav_bytes(seconds: float, freq: float = 0.0, rate: int = 8000) -> bytes:
    n = int(seconds * rate)
    if freq:
        frames = b"".join(int(8000 * math.sin(2 * math.pi * freq * i / rate)).to_bytes(2, "little", signed=True)
                          for i in range(n))
    else:
        frames = b"\x00\x00" * n
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(rate)
        w.writeframes(frames)
    return buf.getvalue()


@pytest.fixture
def client():
    with TestClient(app) as c:
        yield c


def post(client, data: bytes, name="voice.ogg"):
    return client.post("/transcribe", files={"audio": (name, data, "application/octet-stream")})


@needs_model
def test_recognizes_russian_question_wav(client):
    t = time.perf_counter()
    r = post(client, VOICE.read_bytes(), "voice.wav")
    elapsed = time.perf_counter() - t
    assert r.status_code == 200, r.text
    body = r.json()
    assert set(body) == {"text", "lang", "duration_s", "confidence"}
    assert body["lang"] == "ru" and body["confidence"] is None
    assert 3.0 < body["duration_s"] < 4.5
    text = body["text"]
    for word in ("сколько", "дней", "положено", "шести", "месяцев", "работы"):
        assert word in text, text
    assert elapsed < 5, elapsed


@needs_model
@pytest.mark.parametrize("ext,codec", [("ogg", ["-c:a", "libopus", "-b:a", "24k"]), ("m4a", ["-c:a", "aac"]),
                                       ("mp3", ["-c:a", "libmp3lame"])])
def test_formats_ogg_m4a_mp3(client, tmp_path, ext, codec):
    # голосовые MAX — ogg/opus; аудиофайлы с телефона — m4a и mp3. Перекодируем тот же WAV.
    out = tmp_path / f"voice.{ext}"
    subprocess.run([asr.ffmpeg_bin(), "-loglevel", "error", "-i", str(VOICE), *codec, str(out)], check=True)
    r = post(client, out.read_bytes(), out.name)
    assert r.status_code == 200, r.text
    assert "месяцев" in r.json()["text"]


@needs_ffmpeg
def test_silence_gives_empty_text(client):
    r = post(client, wav_bytes(2.0), "silence.wav")
    assert r.status_code == 200, r.text
    assert r.json()["text"] == "" and r.json()["duration_s"] == 2.0


@needs_ffmpeg
def test_longer_than_limit_rejected(client):
    r = post(client, wav_bytes(asr.MAX_DURATION_S + 2), "long.wav")
    assert r.status_code == 413


@needs_ffmpeg
def test_garbage_and_empty_rejected(client):
    assert post(client, b"not an audio file at all" * 10).status_code == 422
    assert post(client, b"").status_code == 422


@needs_ffmpeg
def test_no_model_is_503(client, monkeypatch, tmp_path):
    monkeypatch.setattr(app_module.state, "recognizer", asr.Recognizer(model_dir=tmp_path))
    r = post(client, wav_bytes(1.0, freq=440), "tone.wav")
    assert r.status_code == 503


def test_no_ffmpeg_is_503(client, monkeypatch):
    monkeypatch.setenv("RAG_FFMPEG", "")
    monkeypatch.setattr(shutil, "which", lambda _: None)
    assert post(client, wav_bytes(1.0, freq=440), "tone.wav").status_code == 503
