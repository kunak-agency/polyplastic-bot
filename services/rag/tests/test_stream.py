"""WS /stream (stream.py): порядок событий, пауза конца реплики, авторизация, нет моделей.

Распознаватель и детектор речи — фейки: «речь» — окно с амплитудой выше 0,1. Тест с настоящими
T-one и Silero VAD пропускается, если моделей нет в .cache."""

import threading
import wave
from pathlib import Path

import numpy as np
import pytest
from fastapi.testclient import TestClient
from starlette.websockets import WebSocketDisconnect

import asr
import stream
from app import app

FRAME = 320  # 20 мс PCM16 8 кГц


class FakeVad:
    def __init__(self):
        self.speech = False
        self.resets = 0

    def accept_waveform(self, w):
        self.speech = float(np.max(np.abs(w))) > 0.1

    def is_speech_detected(self):
        return self.speech

    def empty(self):
        return True

    def pop(self):
        pass

    def reset(self):
        self.resets += 1


class FakeStream:
    def __init__(self):
        self.loud = 0
        self.total = 0
        self.finished = False

    def accept_waveform(self, rate, samples):
        assert rate == 8000
        self.total += len(samples)
        self.loud += int(np.sum(np.abs(samples) > 0.1))

    def input_finished(self):
        self.finished = True


class FakeRec:
    def __init__(self, final="когда выплачивают аванс"):
        self.final = final
        self.streams = []

    def create_stream(self):
        s = FakeStream()
        self.streams.append(s)
        return s

    def is_ready(self, s):
        return False

    def decode_stream(self, s):
        pass

    def get_result(self, s):
        if s.finished:
            return self.final
        if not self.final:
            return ""
        return " ".join(["слово"] * (s.loud // 4000))  # новое слово каждые 0,5 с речи


class FakeEngine:
    def __init__(self, final="когда выплачивают аванс"):
        self.rec = FakeRec(final)

    def new_session(self):
        return stream.Session(self.rec, threading.Lock(), FakeVad(), 700)


def pcm(seconds: float, amp: float = 0.0) -> bytes:
    n = int(seconds * 8000)
    x = (amp * np.sin(np.arange(n) * 2 * np.pi * 300 / 8000) * 32767).astype("<i2")
    return x.tobytes()


def send_frames(ws, data: bytes, frame: int = FRAME):
    for i in range(0, len(data), frame):
        ws.send_bytes(data[i:i + frame])


@pytest.fixture
def client():
    with TestClient(app) as c:
        yield c


@pytest.fixture
def fake_engine(monkeypatch):
    eng = FakeEngine()
    monkeypatch.setattr(stream, "_engine", eng)
    return eng


def collect_until_final(ws):
    events = []
    while True:
        ev = ws.receive_json()
        events.append(ev)
        if ev["type"] == "final":
            return events


def test_utterance_event_sequence(client, fake_engine):
    with client.websocket_connect("/stream") as ws:
        send_frames(ws, pcm(0.5))
        send_frames(ws, pcm(1.6, 0.3))
        send_frames(ws, pcm(1.0))
        events = collect_until_final(ws)
    types = [e["type"] for e in events]
    assert types[0] == "speech_start"
    assert types[-2:] == ["speech_end", "final"]
    assert set(types[1:-2]) == {"partial"}
    partials = [e["text"] for e in events if e["type"] == "partial"]
    assert partials == sorted(set(partials), key=partials.index)  # одинаковые подряд не повторяются
    final = events[-1]
    assert final["text"] == "когда выплачивают аванс"
    assert isinstance(final["asr_ms"], int) and final["asr_ms"] >= 0
    assert set(final) == {"type", "text", "asr_ms"}
    assert all(set(e) == {"type", "text"} for e in events if e["type"] == "partial")
    assert all(set(e) == {"type"} for e in events if e["type"] in ("speech_start", "speech_end"))
    # предбуфер 800 мс ушёл в распознаватель вместе с речью
    s = fake_engine.rec.streams[0]
    assert s.total >= int(0.4 * 8000) + int(1.6 * 8000)


def test_two_utterances_any_frame_size(client, fake_engine):
    data = pcm(0.3) + pcm(0.8, 0.3) + pcm(0.9) + pcm(0.8, 0.3) + pcm(0.9)
    with client.websocket_connect("/stream") as ws:
        send_frames(ws, data, frame=1234)  # чётная длина, не кратная окну
        first = collect_until_final(ws)
        second = collect_until_final(ws)
    assert first[0]["type"] == "speech_start" and second[0]["type"] == "speech_start"


def test_empty_final_not_sent(client, monkeypatch):
    monkeypatch.setattr(stream, "_engine", FakeEngine(final=""))
    with client.websocket_connect("/stream") as ws:
        send_frames(ws, pcm(0.5, 0.3) + pcm(1.0))
        assert ws.receive_json() == {"type": "speech_start"}
        assert ws.receive_json() == {"type": "speech_end"}
        ws.send_text('{"type":"config","silence_ms":300}')
        send_frames(ws, pcm(0.5, 0.3) + pcm(0.5))
        assert ws.receive_json() == {"type": "speech_start"}
        assert ws.receive_json() == {"type": "speech_end"}


def test_config_silence_and_flush(client, fake_engine):
    with client.websocket_connect("/stream") as ws:
        ws.send_text('{"type":"config","silence_ms":2500}')
        ws.send_text("не json")  # мусор игнорируется
        ws.send_bytes(b"\x00")  # нечётный кадр игнорируется
        send_frames(ws, pcm(0.6, 0.3) + pcm(1.2))  # 1,2 с тишины < 2,5 с — реплика не закрыта
        ws.send_text('{"type":"flush"}')
        events = collect_until_final(ws)
    types = [e["type"] for e in events]
    assert types[0] == "speech_start" and types[-2:] == ["speech_end", "final"]


def test_session_silence_bounds():
    s = stream.Session(FakeRec(), threading.Lock(), FakeVad(), 700)
    s.set_silence(100)
    s.set_silence(5000)
    assert s.silence_ms == 700
    s.set_silence(1200)
    assert s.silence_ms == 1200
    assert s.feed(pcm(2.0)) == []  # тишина — никаких событий
    assert s.flush() == []


def test_auth_with_key(client, fake_engine, monkeypatch):
    monkeypatch.setenv("RAG_API_KEY", "secret")
    for headers in ({}, {"Authorization": "Bearer wrong"}, {"Authorization": "Basic secret"}):
        with pytest.raises(WebSocketDisconnect) as exc:
            with client.websocket_connect("/stream", headers=headers):
                pass
        assert exc.value.code == 1008
    with client.websocket_connect("/stream", headers={"Authorization": "Bearer secret"}) as ws:
        send_frames(ws, pcm(0.5, 0.3) + pcm(1.0))
        assert collect_until_final(ws)[-1]["type"] == "final"


def test_models_missing_1011(client, monkeypatch, tmp_path):
    eng = stream.Engine(recognizer=asr.Recognizer(model_dir=tmp_path), vad_model=tmp_path / "vad.onnx")
    monkeypatch.setattr(stream, "_engine", eng)
    with client.websocket_connect("/stream") as ws:
        with pytest.raises(WebSocketDisconnect) as exc:
            ws.receive_json()
    assert exc.value.code == 1011
    assert exc.value.reason == "vad model missing"
    # остальной сервис жив
    assert client.post("/ask", json={"question": "Когда выплачивают аванс?"}).status_code == 200


# --- настоящие модели ---

TONE_DIR = asr._DEFAULT_MODEL
VAD = stream._DEFAULT_VAD
needs_models = pytest.mark.skipif(not (TONE_DIR / "model.onnx").is_file() or not VAD.is_file(),
                                  reason="нет T-one или silero_vad.onnx в .cache")


@needs_models
def test_real_models_recognize_utterance():
    eng = stream.Engine(recognizer=asr.Recognizer(model_dir=TONE_DIR, threads=2), vad_model=VAD)
    with wave.open(str(Path(TONE_DIR) / "0.wav")) as w:
        assert w.getframerate() == 8000
        speech = w.readframes(w.getnframes())
    data = bytes(16000) + speech + bytes(32000)
    sess = eng.new_session()
    events = []
    for i in range(0, len(data) - FRAME + 1, FRAME):
        events += sess.feed(data[i:i + FRAME])
    types = [e["type"] for e in events]
    assert types[0] == "speech_start" and types[-1] == "final", types
    assert events[-1]["text"]
    # тишина после реплики событий не даёт
    assert sess.feed(bytes(16000)) == []
