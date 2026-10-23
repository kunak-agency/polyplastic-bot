"""Чанкинг регламента по пунктам.

Формат входа (txt/md, UTF-8):
- необязательный front-matter `---` с полями `doc`, `title`, `version`;
- заголовок раздела: строка `6. Рабочее время`;
- пункт: строка, начинающаяся с номера `6.1` / `6.1.3` / `6.10.24` (1–2 цифры, затем до 3 цифр);
  следующие строки без номера — продолжение пункта;
- подраздел: пункт-заголовок вида `6.1. Порядок приема работников` — короткий (≤ 14 слов), без
  точки или двоеточия в конце, и за ним идут его подпункты `6.1.N`. Такой пункт не индексируется,
  а его текст становится разделом подпунктов;
- раздел без нумерованных пунктов (`3. Термины и определения` + абзацы): его текст — один пункт
  с номером раздела (`3`). Текст раздела до его первого пункта по-прежнему не индексируется;
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

# 1–2 цифры раздела, затем до 3 цифр: «6.10.24» — пункт, «3.1107» (шифр «П 3.11-07» после OCR) — нет
# После номера — пробел, конец строки или сразу заглавная буква: OCR склеивает «6.3.11.В случае».
CLAUSE_RE = re.compile(r"^(\d{1,2}\.\d{1,3}(\.\d{1,3})?)(?:\.?\s+|\.?$|\.(?=[А-ЯЁA-Z«]))")
SECTION_RE = re.compile(r"^(\d{1,2})\.\s+(\S.*)$")
HEADING_MAX_WORDS = 14
# Пункт длиннее LONG_WORDS (перечень обязанностей на 500+ слов) режется на части ~PART_WORDS слов
# по пунктам перечня («— …;»), иначе по предложениям: окно реранкера и bge-m3 — 1024 токена, и
# хвост перечня просто не попадал бы в скор. Части — `6.4.2/1`, `6.4.2/2`; вводная фраза
# перечня («Работник обязан:») дописывается в раздел каждой части, текст части остаётся дословным.
LONG_WORDS = 200
PART_WORDS = 120
PAGE_RE = re.compile(r"^-{2,}\s*стр\.?\s*(\d+)\s*-{2,}$", re.IGNORECASE)
MIN_WORDS = 50
MAX_WORDS = 250
SUPPORTED_SUFFIXES = {".txt", ".md"}
CORPUS_SUFFIXES = SUPPORTED_SUFFIXES | {".pdf", ".pptx", ".xlsx"}


@dataclass
class Clause:
    clause_id: str
    text: str
    page: int | None

    @property
    def parent(self) -> str:
        if "/" in self.clause_id:  # часть длинного пункта: родитель — сам пункт
            return self.clause_id.split("/", 1)[0]
        return self.clause_id.rsplit(".", 1)[0]

    @property
    def base_id(self) -> str:
        """Номер пункта без номера части: `6.4.2/3` → `6.4.2`."""
        return self.clause_id.split("/", 1)[0]

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


def _is_heading(text: str) -> bool:
    words = text.split()
    return 0 < len(words) <= HEADING_MAX_WORDS and text.rstrip()[-1] not in ".:;,"


def parse_clauses(text: str) -> tuple[dict[str, str], list[tuple[str | None, Clause]]]:
    """Возвращает метаданные и список (название раздела, пункт)."""
    meta, lines = _parse_front_matter(text.replace("\r\n", "\n").split("\n"))
    page: int | None = None
    if any(PAGE_RE.match(l.strip()) or "\f" in l for l in lines):
        page = 1
    sections: dict[str, str] = {}
    out: list[tuple[str | None, Clause]] = []
    intros: set[int] = set()  # id() пунктов-«введений» раздела: текст после заголовка раздела
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
            out.append((clause_id.split(".")[0], current))
            continue
        if m := SECTION_RE.match(line):
            sections[m.group(1)] = m.group(2).strip()
            current = Clause(clause_id=m.group(1), text="", page=page)
            intros.add(id(current))
            out.append((m.group(1), current))
            continue
        if current is not None:
            current.text = f"{current.text} {line}".strip()
        # текст до первого раздела или пункта (преамбула) не индексируется

    # Введение раздела индексируется, только если в разделе нет нумерованных пунктов (иначе это
    # текст до первого пункта). Из повторов одного номера (оглавление + сам раздел) — самый длинный.
    numbered = {c.clause_id.split(".")[0] for _, c in out if id(c) not in intros}
    best_intro: dict[str, Clause] = {}
    for _, c in out:
        if id(c) in intros and c.clause_id not in numbered and c.text:
            if c.clause_id not in best_intro or c.words > best_intro[c.clause_id].words:
                best_intro[c.clause_id] = c
    keep = {id(c) for c in best_intro.values()}
    flat = [(num, c) for num, c in out if id(c) not in intros or id(c) in keep]

    # Пункт-заголовок подраздела («6.1. Порядок приема работников» + подпункты 6.1.N) — не пункт,
    # а раздел своих подпунктов.
    subsections: dict[str, str] = {}
    result: list[tuple[str | None, Clause]] = []
    for i, (num, c) in enumerate(flat):
        nxt = flat[i + 1][1] if i + 1 < len(flat) else None
        if nxt is not None and "." in c.clause_id and nxt.clause_id.startswith(c.clause_id + ".") \
                and _is_heading(c.text):
            subsections[c.clause_id] = c.text
            continue
        section = sections.get(num)
        for prefix, title in subsections.items():
            if c.clause_id.startswith(prefix + "."):
                section = title
        result.append((section, c))
    return meta, result


# пункт перечня: тире после «;», «:» или «.» — тире внутри фразы («РФ - до 14 дней») не граница
_BULLET_SPLIT = re.compile(r"(?<=[;:.])\s+(?=[—–-]{1,2}\s)")
_SENTENCE_SPLIT = re.compile(r"(?<=[.;!?])\s+(?=[А-ЯЁA-Z«—–-])")


def split_long_clause(section: str | None, clause: Clause) -> list[tuple[str | None, Clause]]:
    """Длинный пункт → части `N/1`, `N/2` … по ~PART_WORDS слов, границы — пункты перечня,
    иначе предложения. Короткий пункт возвращается как есть."""
    if clause.words <= LONG_WORDS:
        return [(section, clause)]
    pieces = _BULLET_SPLIT.split(clause.text)
    if len(pieces) < 3:
        pieces = _SENTENCE_SPLIT.split(clause.text)
    if len(pieces) < 2:
        return [(section, clause)]
    lead = pieces[0] if pieces[0].rstrip().endswith(":") and len(pieces[0].split()) <= 30 else None
    parts: list[list[str]] = [[]]
    for piece in pieces:
        if parts[-1] and sum(len(x.split()) for x in parts[-1]) + len(piece.split()) > PART_WORDS:
            parts.append([])
        parts[-1].append(piece)
    if len(parts) > 1 and sum(len(x.split()) for x in parts[-1]) < MIN_WORDS:  # хвост — к предыдущей
        parts[-2].extend(parts.pop())
    if len(parts) == 1:
        return [(section, clause)]
    out = []
    for n, chunk_pieces in enumerate(parts, start=1):
        part_section = section
        if lead and n > 1:
            part_section = f"{section}. {lead.rstrip(': ')}" if section else lead.rstrip(": ")
        out.append((part_section, Clause(f"{clause.clause_id}/{n}", " ".join(chunk_pieces), clause.page)))
    return out


def chunk_text(text: str, default_doc: str) -> list[Chunk]:
    meta, parsed = parse_clauses(text)
    doc = meta.get("doc") or default_doc
    chunks: list[Chunk] = []
    expanded = [part for section, clause in parsed for part in split_long_clause(section, clause)]
    for section, clause in expanded:
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


def _load_file(path: Path) -> list[Chunk]:
    if path.suffix.lower() in SUPPORTED_SUFFIXES:
        return chunk_text(path.read_text(encoding="utf-8"), default_doc=path.stem)
    from parsers import parse_document  # noqa: PLC0415 — parsers импортирует chunker

    parsed = parse_document(path.name, path.read_bytes())
    if parsed.low_confidence_pages:
        log.warning("%s: страницы/слайды под вычитку: %s", path.name, parsed.low_confidence_pages)
    return parsed.chunks


def load_corpus(corpus_dir: str | Path) -> list[Chunk]:
    """Чанки всех документов корпуса. `corpus_dir` — каталог или несколько через запятую
    (`testdata/corpus,corpus-real`). Кроме txt/md берутся pdf, pptx и xlsx через parsers:
    PDF-скан распознаётся на старте, поэтому скан лучше один раз выгрузить в md
    (`python parsers.py export`) и класть в корпус его."""
    dirs = [Path(d.strip()) for d in str(corpus_dir).split(",") if d.strip()]
    chunks: list[Chunk] = []
    for root in dirs:
        if not root.is_dir():
            log.warning("каталог корпуса %s не найден — пропущен", root)
            continue
        files = sorted(p for p in root.rglob("*")
                       if p.is_file() and p.suffix.lower() in CORPUS_SUFFIXES and not p.name.startswith((".", "~$")))
        for path in files:
            file_chunks = _load_file(path)
            log.info("%s: %d чанков, %d пунктов", path.name, len(file_chunks),
                     sum(len(c.clauses) for c in file_chunks))
            chunks.extend(file_chunks)
    if not chunks:
        log.warning("корпус %s пуст", corpus_dir)
    return chunks
