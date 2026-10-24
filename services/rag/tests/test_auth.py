"""Общий ключ RAG_API_KEY: пустой — сервис открыт, задан — всё, кроме /health, требует Bearer."""

import pytest
from fastapi.testclient import TestClient

from app import app

KEY = "test-secret-key"


@pytest.fixture(scope="module")
def client():
    with TestClient(app) as c:  # with — чтобы отработал lifespan и собрался индекс
        yield c


@pytest.mark.parametrize("value", [None, "", "  "])
def test_without_key_everything_open(client, monkeypatch, value):
    if value is None:
        monkeypatch.delenv("RAG_API_KEY", raising=False)
    else:
        monkeypatch.setenv("RAG_API_KEY", value)
    assert client.get("/health").status_code == 200
    assert client.get("/documents").status_code == 200
    r = client.post("/ask", json={"question": "Когда выплачивают аванс?"})
    assert r.status_code == 200


@pytest.fixture
def keyed(client, monkeypatch):
    monkeypatch.setenv("RAG_API_KEY", KEY)
    return client


def test_with_key_no_header_401(keyed):
    for method, path in (("GET", "/documents"), ("POST", "/ask"), ("POST", "/flags"),
                         ("POST", "/documents"), ("POST", "/transcribe"), ("GET", "/openapi.json")):
        r = keyed.request(method, path)
        assert r.status_code == 401, (method, path)
        assert r.json() == {"detail": "unauthorized"}


@pytest.mark.parametrize("header", ["Bearer wrong", "Bearer", "Basic " + KEY, KEY, "Bearer " + KEY + "x"])
def test_with_key_wrong_header_401(keyed, header):
    r = keyed.get("/documents", headers={"Authorization": header})
    assert r.status_code == 401
    assert r.json() == {"detail": "unauthorized"}


def test_with_key_right_header_200(keyed):
    auth = {"Authorization": f"Bearer {KEY}"}
    assert keyed.get("/documents", headers=auth).status_code == 200
    r = keyed.post("/ask", json={"question": "Когда выплачивают аванс?"}, headers=auth)
    assert r.status_code == 200
    assert r.json()["sources"]


def test_health_always_open(keyed):
    r = keyed.get("/health")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}
    assert keyed.get("/health", headers={"Authorization": "Bearer wrong"}).status_code == 200
