"""Реестр документов индекса: корпус со старта + загруженные через POST /documents.

Хранится в памяти процесса: после рестарта остаётся только корпус из RAG_CORPUS_DIR.
Документ с тем же именем `doc` при повторной загрузке заменяет предыдущий целиком,
иначе в индексе окажутся две версии одного регламента и цитаты из устаревшей.
"""

from __future__ import annotations

import hashlib
import threading
from dataclasses import dataclass, field
from datetime import datetime, timezone

from chunker import Chunk


@dataclass
class DocRecord:
    doc_id: str
    doc: str
    filename: str | None
    kind: str                      # corpus | pdf | pptx | xlsx | text
    source: str                    # corpus | upload
    chunks: list[Chunk]
    low_confidence_pages: list[int] = field(default_factory=list)
    uploaded_at: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat(timespec="seconds"))

    @property
    def clauses(self) -> int:
        return sum(len(c.clauses) for c in self.chunks)

    def summary(self) -> dict:
        return {"doc_id": self.doc_id, "doc": self.doc, "filename": self.filename, "kind": self.kind,
                "source": self.source, "chunks": len(self.chunks), "clauses": self.clauses,
                "low_confidence_pages": self.low_confidence_pages, "uploaded_at": self.uploaded_at}


def make_doc_id(doc: str, data: bytes) -> str:
    h = hashlib.sha256()
    h.update(doc.encode("utf-8"))
    h.update(b"\0")
    h.update(data)
    return h.hexdigest()[:16]


class DocumentStore:
    def __init__(self, corpus_chunks: list[Chunk]):
        self._lock = threading.Lock()
        self._docs: dict[str, DocRecord] = {}
        by_doc: dict[str, list[Chunk]] = {}
        for ch in corpus_chunks:
            by_doc.setdefault(ch.doc, []).append(ch)
        for doc, chunks in by_doc.items():
            body = "\n".join(c.body for c in chunks).encode("utf-8")
            rec = DocRecord(doc_id=make_doc_id(doc, body), doc=doc, filename=None, kind="corpus",
                            source="corpus", chunks=chunks)
            self._docs[rec.doc_id] = rec

    def put(self, rec: DocRecord) -> list[Chunk]:
        """Добавляет документ (заменяя одноимённый) и возвращает все чанки индекса."""
        with self._lock:
            for key in [k for k, r in self._docs.items() if r.doc == rec.doc]:
                del self._docs[key]
            self._docs[rec.doc_id] = rec
            return self._all_chunks()

    def _all_chunks(self) -> list[Chunk]:
        return [ch for r in self._docs.values() for ch in r.chunks]

    def all_chunks(self) -> list[Chunk]:
        with self._lock:
            return self._all_chunks()

    def list(self) -> list[DocRecord]:
        with self._lock:
            return list(self._docs.values())
