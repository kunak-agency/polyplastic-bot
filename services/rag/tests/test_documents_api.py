import pytest
from fastapi.testclient import TestClient

import app as app_module
from app import app

MEMO = """---
doc: Памятка-синтетика
---
1. Питание
1.1. Горячий обед в столовой корпуса Б для сотрудников производственной смены бесплатный, талоны выдаёт мастер смены в начале смены.
"""


@pytest.fixture
def client():
    with TestClient(app) as c:  # свой lifespan: загрузки не протекают в другие тесты
        yield c


def test_list_contains_corpus(client):
    body = client.get("/documents").json()
    assert body["total"] == 1
    item = body["items"][0]
    assert item["doc"] == "ПВТР-синтетика" and item["source"] == "corpus" and item["clauses"] == 15


def test_upload_markdown_then_answer_from_it(client):
    assert client.post("/ask", json={"question": "Где взять талоны на обед?"}).json()["needs_escalation"]
    r = client.post("/documents", files={"file": ("memo.md", MEMO.encode(), "text/markdown")})
    assert r.status_code == 200, r.text
    out = r.json()
    assert set(out) == {"doc_id", "chunks", "clauses", "low_confidence_pages"}
    assert out["chunks"] == 1 and out["clauses"] == 1 and out["low_confidence_pages"] == []

    body = client.post("/ask", json={"question": "Где взять талоны на обед?"}).json()
    assert body["needs_escalation"] is False
    assert body["sources"][0]["doc"] == "Памятка-синтетика" and body["sources"][0]["clause_id"] == "1.1"

    docs = client.get("/documents").json()
    assert docs["total"] == 2
    up = next(i for i in docs["items"] if i["source"] == "upload")
    assert up["doc_id"] == out["doc_id"] and up["filename"] == "memo.md" and up["kind"] == "text"


def test_reupload_same_doc_replaces_previous(client):
    client.post("/documents", files={"file": ("memo.md", MEMO.encode())})
    v2 = MEMO.replace("бесплатный", "стоит 100 рублей")
    client.post("/documents", files={"file": ("memo-v2.md", v2.encode())})
    docs = client.get("/documents").json()
    assert docs["total"] == 2
    body = client.post("/ask", json={"question": "Где взять талоны на обед?"}).json()
    assert "100 рублей" in body["answer"]


def test_doc_name_from_form_field(client):
    r = client.post("/documents", data={"doc": "Памятка цеха"},
                    files={"file": ("x.md", b"1. A\n1.1. Text about the canteen and lunch\n")})
    assert r.status_code == 200
    assert any(i["doc"] == "Памятка цеха" for i in client.get("/documents").json()["items"])


@pytest.mark.parametrize("name,data,code", [
    ("a.docx", b"PK\x03\x04", 415),
    ("a.md", b"", 422),
    ("a.md", "просто текст без пунктов".encode(), 422),
    ("a.pdf", b"%PDF-1.4 broken", 422),
    ("a.txt", b"\xff\xfe\x00bad", 422),
])
def test_upload_rejections(client, name, data, code):
    assert client.post("/documents", files={"file": (name, data)}).status_code == code


def test_upload_size_limit(client, monkeypatch):
    monkeypatch.setattr(app_module, "MAX_UPLOAD_BYTES", 10)
    assert client.post("/documents", files={"file": ("a.md", b"1. A\n1.1. " + b"x" * 100)}).status_code == 413
