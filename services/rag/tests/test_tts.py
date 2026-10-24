"""POST /tts (tts.py) и нормализация текста (tts_normalize.py, из hack-ru-clinik).

Настоящая модель Silero не нужна: подставляется фейк с apply_tts."""

import pytest
import torch
from fastapi.testclient import TestClient

import tts
from app import app
from tts_normalize import normalize, split_sentences


# --- нормализация ---

@pytest.mark.parametrize("text, expected", [
    ("Аванс выплачивается 25 числа.", "Аванс выплачивается двадцать пять числа."),
    ("Приходите в 10:30.", "Приходите в десять тридцать."),
    ("Срок до 16.10.2026.", "Срок до шестнадцатого октября две тысячи двадцать шестого года."),
    ("с 1 января", "с первого января"),
    ("Доплата 50%", "Доплата пятьдесят процентов"),
    ("Пункт № 7", "Пункт номер семь"),
])
def test_normalize_numbers_dates_time(text, expected):
    assert normalize(text) == expected


def test_normalize_strips_markup():
    out = normalize("**Важно**: см. [ПВТР](https://example.com/x) <b>п.</b>")
    assert "*" not in out and "http" not in out and "<" not in out
    assert "ПВТР" in out


def test_no_digits_left():
    out = normalize("Отпуск 28 дней, из них 14 подряд; 3,5 часа; в 2026 году.")
    assert not any(ch.isdigit() for ch in out)


def test_split_sentences():
    assert split_sentences("Первое. Второе! Третье? 123.") == ["Первое.", "Второе!", "Третье?"]
    long = " ".join(["слово"] * 100) + "."
    parts = split_sentences(long)
    assert len(parts) > 1 and all(len(p) <= 250 for p in parts)


@pytest.mark.parametrize("text, expected", [
    ("Это пункт 6.7.4 правил.", "Это пункт шесть точка семь точка четыре правил."),
    ("См. п. 3.2 ПВТР", "См. п. три точка два ПВТР"),
    ("раздел 10.1, пункт 6.7.11", "раздел десять точка один, пункт шесть точка семь точка одиннадцать"),
    ("Температура 36.6", "Температура 36.6"),  # не номер пункта — дробь остаётся общей нормализации
])
def test_speakable_clauses(text, expected):
    assert tts.speakable_clauses(text) == expected


def test_clause_is_one_sentence():
    assert split_sentences(normalize(tts.speakable_clauses("Это пункт 6.7.4 правил распорядка."))) == [
        "Это пункт шесть точка семь точка четыре правил распорядка."]


# --- /tts с фейковой моделью ---

class FakeModel:
    speakers = ["aidar", "baya", "kseniya", "xenia", "eugene"]

    def __init__(self):
        self.calls = []

    def apply_tts(self, text, speaker, sample_rate, put_accent, put_yo):
        self.calls.append((text, speaker, sample_rate))
        return torch.full((100 * len(text),), 0.5)  # длина звука ∝ длине текста


@pytest.fixture
def client():
    with TestClient(app) as c:
        yield c


@pytest.fixture
def fake(monkeypatch):
    model = FakeModel()
    monkeypatch.setattr(tts.engine, "model", model)
    monkeypatch.setattr(tts.engine, "speakers", FakeModel.speakers)
    monkeypatch.setattr(tts.engine, "speaker", "aidar")
    monkeypatch.setattr(tts.engine, "ready", True)
    return model


def test_tts_streams_pcm_per_sentence(client, fake):
    with client.stream("POST", "/tts", json={"text": "Аванс 25 числа. Зарплата 10 числа!"}) as r:
        assert r.status_code == 200
        assert r.headers["content-type"] == "audio/L16;rate=8000;channels=1"
        body = b"".join(r.iter_bytes())
    texts = [c[0] for c in fake.calls]
    assert texts == ["Аванс двадцать пять числа.", "Зарплата десять числа!"]
    assert all(c[1] == "aidar" and c[2] == 8000 for c in fake.calls)
    assert len(body) == sum(2 * 100 * len(t) for t in texts)  # PCM16: 2 байта на отсчёт
    assert body[:2] == int(0.5 * 32767).to_bytes(2, "little", signed=True)


def test_tts_speaker_override_and_unknown(client, fake):
    assert client.post("/tts", json={"text": "Привет.", "speaker": "eugene"}).status_code == 200
    assert fake.calls[-1][1] == "eugene"
    r = client.post("/tts", json={"text": "Привет.", "speaker": "nobody"})
    assert r.status_code == 400


def test_tts_bad_input(client, fake):
    assert client.post("/tts", json={"text": "   "}).status_code == 400
    assert client.post("/tts", json={"text": "123"}).status_code == 200  # цифры читаются словами
    assert client.post("/tts", json={"text": "..."}).status_code == 400
    assert client.post("/tts", json={"text": "а" * 1001}).status_code == 422


def test_tts_not_ready_503(client, monkeypatch):
    monkeypatch.setattr(tts.engine, "ready", False)
    assert client.post("/tts", json={"text": "Привет."}).status_code == 503


def test_tts_requires_key(client, fake, monkeypatch):
    monkeypatch.setenv("RAG_API_KEY", "k")
    assert client.post("/tts", json={"text": "Привет."}).status_code == 401
    r = client.post("/tts", json={"text": "Привет."}, headers={"Authorization": "Bearer k"})
    assert r.status_code == 200


def test_missing_weights_disable_tts(tmp_path, monkeypatch):
    monkeypatch.setenv("RAG_TTS_MODEL", str(tmp_path / "none.pt"))
    eng = tts.Engine()
    with pytest.raises(FileNotFoundError):
        eng.load()
    assert not eng.ready


def test_strip_wav_and_google_fallback(monkeypatch):
    import struct

    import tts

    pcm = b"\x01\x00\x02\x00"
    wav = b"RIFF" + struct.pack("<I", 36 + len(pcm)) + b"WAVEfmt " + b"\x00" * 20 + b"data" + struct.pack("<I", len(pcm)) + pcm
    assert tts.strip_wav(wav) == pcm
    assert tts.strip_wav(pcm) == pcm
