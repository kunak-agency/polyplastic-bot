"""Чанкинг регламента по пунктам.

Формат входа (txt/md, UTF-8):
- необязательный front-matter `---` с полями `doc`, `title`, `version`;
- заголовок раздела: строка `6. Рабочее время`;
- пункт: строка, начинающаяся с номера `6.1` / `6.1.3` (regex `^(\\d+\\.\\d+(\\.\\d+)?)`);
  следующие строки без номера — продолжение пункта;
- разрыв страницы: символ `\\f` или строка `--- стр. N ---`.

Чанк = пункт. Пункты короче MIN_WORDS слов склеиваются с соседями того же родителя
(родитель 6.1.3 — 6.1, родитель 6.1 — раздел 6). Склейка идёт только между соседями
в порядке документа и не раздувает чанк больше MAX_WORDS слов.
"""

from __future__ import annotations

import logging
import re
from dataclasses import dataclass, field
from pathlib import Path

log = logging.getLogger("rag.chunker")

CLAUSE_RE = re.compile(r"^(\d+\.\d+(\.\d+)?)\.?(?:\s+|$)")
SECTION_RE = re.compile(r"^(\d+)\.\s+(\S.*)$")
PAGE_RE = re.compile(r"^-{2,}\s*стр\.?\s*(\d+)\s*-{2,}$", re.IGNORECASE)
MIN_WORDS = 50
MAX_WORDS = 400
SUPPORTED_SUFFIXES = {".txt", ".md"}


@dataclass
class Clause:
    clause_id: str
    text: str
    page: int | None

    @property
    def parent(self) -> str:
        return self.clause_id.rsplit(".", 1)[0]

    @property
    def words(self) -> int:
        return len(self.text.split())


@dataclass
class Chunk:
    doc: str
    section: str | None
    clauses: list[Clause] = field(default_factory=list)
    title: str | None = None
    version: str | None = None

    @property
    def clause_id(self) -> str:
        """Основной пункт чанка — первый из склеенных."""
        return self.clauses[0].clause_id

    @property
    def clause_ids(self) -> list[str]:
        return [c.clause_id for c in self.clauses]

    @property
    def page(self) -> int | None:
        return self.clauses[0].page

    @property
    def body(self) -> str:
        """Дословный текст пунктов (с номерами) — по нему проверяются цитаты."""
        return "\n".join(f"{c.clause_id}. {c.text}" for c in self.clauses)

    @property
    def index_text(self) -> str:
        """Текст для поиска: контекст документа и раздела + тело."""
        head = f"{self.doc}, раздел «{self.section}», п. {', '.join(self.clause_ids)}:" if self.section \
            else f"{self.doc}, п. {', '.join(self.clause_ids)}:"
        return f"{head}\n{self.body}"

    @property
    def words(self) -> int:
        return sum(c.words for c in self.clauses)

    def clause(self, clause_id: str) -> Clause | None:
        return next((c for c in self.clauses if c.clause_id == clause_id), None)


def _parse_front_matter(lines: list[str]) -> tuple[dict[str, str], list[str]]:
    if not lines or lines[0].strip() != "---":
        return {}, lines
    meta: dict[str, str] = {}
    for i, line in enumerate(lines[1:], start=1):
        if line.strip() == "---":
            return meta, lines[i + 1:]
        key, sep, value = line.partition(":")
        if sep:
            meta[key.strip().lower()] = value.strip()
    return {}, lines  # незакрытый front-matter — считаем обычным текстом


def parse_clauses(text: str) -> tuple[dict[str, str], list[tuple[str | None, Clause]]]:
    """Возвращает метаданные и список (название раздела, пункт)."""
    meta, lines = _parse_front_matter(text.replace("\r\n", "\n").split("\n"))
    page: int | None = None
    if any(PAGE_RE.match(l.strip()) or "\f" in l for l in lines):
        page = 1
    sections: dict[str, str] = {}
    out: list[tuple[str | None, Clause]] = []
    current: Clause | None = None

    for raw in lines:
        if "\f" in raw:
            page = (page or 1) + raw.count("\f")
            raw = raw.replace("\f", "")
        line = raw.strip()
        if not line:
            continue
        if m := PAGE_RE.match(line):
            page = int(m.group(1))  # пункт может продолжаться на следующей странице
            continue
        if m := CLAUSE_RE.match(line):
            clause_id = m.group(1)
            current = Clause(clause_id=clause_id, text=line[m.end():].strip(), page=page)
            out.append((sections.get(clause_id.split(".")[0]), current))
            continue
        if m := SECTION_RE.match(line):
            sections[m.group(1)] = m.group(2).strip()
            current = None
            continue
        if current is not None:
            current.text = f"{current.text} {line}".strip()
        # текст до первого пункта (преамбула) не индексируется

    return meta, out


def chunk_text(text: str, default_doc: str) -> list[Chunk]:
    meta, parsed = parse_clauses(text)
    doc = meta.get("doc") or default_doc
    chunks: list[Chunk] = []
    for section, clause in parsed:
        if not clause.text:
            log.warning("%s: пустой пункт %s пропущен", doc, clause.clause_id)
            continue
        prev = chunks[-1] if chunks else None
        can_merge = (
            prev is not None
            and prev.clauses[-1].parent == clause.parent
            and prev.words + clause.words <= MAX_WORDS
            and (prev.words < MIN_WORDS or clause.words < MIN_WORDS)
        )
        if can_merge:
            prev.clauses.append(clause)
        else:
            chunks.append(Chunk(doc=doc, section=section, clauses=[clause],
                                title=meta.get("title"), version=meta.get("version")))
    return chunks


def load_corpus(corpus_dir: str | Path) -> list[Chunk]:
    root = Path(corpus_dir)
    if not root.is_dir():
        log.warning("каталог корпуса %s не найден — индекс пуст", root)
        return []
    chunks: list[Chunk] = []
    for path in sorted(p for p in root.rglob("*") if p.suffix.lower() in SUPPORTED_SUFFIXES and p.is_file()):
        file_chunks = chunk_text(path.read_text(encoding="utf-8"), default_doc=path.stem)
        log.info("%s: %d чанков, %d пунктов", path.name, len(file_chunks),
                 sum(len(c.clauses) for c in file_chunks))
        chunks.extend(file_chunks)
    return chunks
