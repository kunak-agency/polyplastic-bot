"""Парсеры для загрузки документов HR: PDF (текстовый слой или OCR), PPTX, XLSX, txt/md.

Каждый парсер возвращает ParsedDocument: чанки в том же формате, что у корпуса, и номера
страниц, которым нельзя доверять без вычитки (OCR с низкой уверенностью или пустая страница).

- PDF: текст берётся из текстового слоя (pypdfium2). Страница почти без текста считается
  сканом и идёт в Tesseract `rus+eng --psm 4` при 300 dpi, если tesseract есть в системе.
  Затем пост-обработка (омоглифы, переносы, колонтитулы, номера пунктов) и обычный
  клаузный чанкинг `chunker.chunk_text`. Если пунктов `N.N` нет, чанк = страница.
- PPTX: слайд = чанк (заголовок, текст фигур, таблицы строками `a | b`, заметки докладчика).
- XLSX: строка = чанк «Заголовок: значение | …»; первая непустая строка листа — заголовки.
"""

from __future__ import annotations

import io
import logging
import re
import shutil
import subprocess
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path

from chunker import CLAUSE_RE, SECTION_RE, Chunk, Clause, chunk_text

log = logging.getLogger("rag.parsers")

OCR_LANG = "rus+eng"
OCR_DPI = 300
OCR_PSM = "4"
OCR_TIMEOUT_S = 120
OCR_LOW_CONF = 70.0          # средняя уверенность Tesseract по словам, ниже — страница «под вычитку»
TEXT_LAYER_MIN_CHARS = 30    # меньше непробельных символов в текстовом слое — считаем страницу сканом
SUPPORTED = {".pdf", ".pptx", ".xlsx", ".txt", ".md"}


class UnsupportedDocument(ValueError):
    """Формат не поддерживается (→ 415)."""


class CorruptDocument(ValueError):
    """Формат поддерживается, но файл не открывается или не в той кодировке (→ 422)."""


@dataclass
class ParsedDocument:
    doc: str
    kind: str                                   # pdf | pptx | xlsx | text
    chunks: list[Chunk]
    low_confidence_pages: list[int] = field(default_factory=list)
    ocr_pages: list[int] = field(default_factory=list)

    @property
    def clauses(self) -> int:
        return sum(len(c.clauses) for c in self.chunks)


# --- пост-обработка текста (OCR и текстовый слой) ---

_CYR = "а-яёА-ЯЁ"
# латиница, которую OCR путает с кириллицей
_LAT2CYR = str.maketrans({
    "A": "А", "B": "В", "C": "С", "E": "Е", "H": "Н", "K": "К", "M": "М", "O": "О", "P": "Р",
    "T": "Т", "X": "Х", "Y": "У", "a": "а", "c": "с", "e": "е", "o": "о", "p": "р", "x": "х",
    "y": "у", "k": "к", "m": "м", "u": "и", "n": "п", "r": "г",
})
_MIXED_WORD = re.compile(rf"\b(?=\w*[{_CYR}])(?=\w*[A-Za-z])[\w]+\b")
_HYPHEN_BREAK = re.compile(r"([а-яёА-ЯЁA-Za-z])[-‐‑]\s*\n\s*([а-яёa-z])")
_SOFT_HYPHEN = "­"
# «6,1.» / «6,1,3» / «6.1 .» в начале строки → «6.1.». Пробел между цифрами не чиним:
# «3. 15 минут» в заголовке неотличим от «3.15».
_CLAUSE_COMMA = re.compile(r"^(\s*)(\d{1,2}),(\d{1,2})(?:[.,](\d{1,2}))?[.,]?(?=\s)", re.MULTILINE)
_CLAUSE_SPACE_DOT = re.compile(r"^(\s*\d{1,2}\.\d{1,2}(?:\.\d{1,2})?) \.(?=\s)", re.MULTILINE)
_PAGE_NUM_LINE = re.compile(r"^\s*(?:[-–—]\s*)?(?:стр\.?|страница|page)?\s*\d{1,4}\s*(?:из\s*\d{1,4})?(?:\s*[-–—])?\s*$",
                            re.IGNORECASE)


def fix_homoglyphs(text: str) -> str:
    """Латинские буквы внутри кириллических слов → кириллица («пpопуск» → «пропуск»).

    Слова целиком латиницей (MAX, Wi-Fi, e-mail) не трогаем: по одному слову не отличить
    английский термин от ошибки OCR.
    """
    return _MIXED_WORD.sub(lambda m: m.group(0).translate(_LAT2CYR), text)


def join_hyphenation(text: str) -> str:
    """«трудо-\\nвого» → «трудового»; мягкие переносы удаляются."""
    text = text.replace(_SOFT_HYPHEN + "\n", "").replace(_SOFT_HYPHEN, "")
    return _HYPHEN_BREAK.sub(r"\1\2", text)


def fix_clause_numbers(text: str) -> str:
    def repl(m: re.Match) -> str:
        parts = [m.group(2), m.group(3)] + ([m.group(4)] if m.group(4) else [])
        return m.group(1) + ".".join(parts) + "."
    return _CLAUSE_SPACE_DOT.sub(r"\1.", _CLAUSE_COMMA.sub(repl, text))


def _norm_furniture(line: str) -> str:
    return re.sub(r"\d+", "#", line.strip().lower())


def _is_furniture(line: str, furniture: set[str]) -> bool:
    stripped = line.strip()
    if CLAUSE_RE.match(stripped) or SECTION_RE.match(stripped):
        return False  # пункт или раздел никогда не считаем колонтитулом
    return _norm_furniture(line) in furniture or bool(_PAGE_NUM_LINE.match(line))


def strip_headers_footers(pages: list[str], edge_lines: int = 2) -> list[str]:
    """Удаляет колонтитулы: строки из первых/последних `edge_lines` строк страницы, которые
    (с точностью до цифр) повторяются на половине страниц и больше, и строки-номера страниц.
    """
    split = [[l for l in p.split("\n")] for p in pages]
    candidates: Counter[str] = Counter()
    for lines in split:
        nonempty = [l for l in lines if l.strip()]
        edges = set(_norm_furniture(l) for l in nonempty[:edge_lines] + nonempty[-edge_lines:])
        candidates.update(e for e in edges if e)
    min_repeats = max(2, (len(pages) + 1) // 2)
    furniture = {k for k, n in candidates.items() if n >= min_repeats} if len(pages) >= 2 else set()

    out = []
    for lines in split:
        nonempty_idx = [i for i, l in enumerate(lines) if l.strip()]
        edge_idx = set(nonempty_idx[:edge_lines] + nonempty_idx[-edge_lines:])
        kept = [l for i, l in enumerate(lines)
                if not (i in edge_idx and _is_furniture(l, furniture))]
        out.append("\n".join(kept))
    return out


def postprocess_pages(pages: list[str]) -> list[str]:
    pages = strip_headers_footers([p.replace("\r\n", "\n").replace("\r", "\n") for p in pages])
    return [fix_clause_numbers(fix_homoglyphs(join_hyphenation(p))) for p in pages]


# --- OCR ---

def tesseract_available(lang: str = OCR_LANG) -> bool:
    exe = shutil.which("tesseract")
    if not exe:
        return False
    try:
        out = subprocess.run([exe, "--list-langs"], capture_output=True, text=True, timeout=10)
    except (OSError, subprocess.SubprocessError):
        return False
    langs = set((out.stdout + out.stderr).split())
    return all(l in langs for l in lang.split("+"))


def ocr_image(png: bytes, lang: str = OCR_LANG) -> tuple[str, float]:
    """PNG → (текст, средняя уверенность слов 0–100). Tesseract вызывается без shell."""
    exe = shutil.which("tesseract")
    if not exe:
        raise RuntimeError("tesseract не установлен")
    res = subprocess.run([exe, "stdin", "stdout", "-l", lang, "--psm", OCR_PSM, "tsv"],
                         input=png, capture_output=True, timeout=OCR_TIMEOUT_S, check=True)
    rows = res.stdout.decode("utf-8", errors="replace").splitlines()
    lines: dict[tuple[int, int, int], list[str]] = {}
    confs: list[float] = []
    for row in rows[1:]:
        cols = row.split("\t")
        if len(cols) < 12 or cols[0] != "5":  # level 5 = слово
            continue
        word = cols[11].strip()
        if not word:
            continue
        try:
            conf = float(cols[10])
        except ValueError:
            conf = -1
        if conf >= 0:
            confs.append(conf)
        key = (int(cols[2]), int(cols[3]), int(cols[4]))  # block, paragraph, line
        lines.setdefault(key, []).append(word)
    text = "\n".join(" ".join(ws) for _, ws in sorted(lines.items()))
    return text, (sum(confs) / len(confs) if confs else 0.0)


# --- PDF ---

def _page_marked(pages: list[str]) -> str:
    return "\n".join(f"--- стр. {i} ---\n{t}" for i, t in enumerate(pages, start=1))


def parse_pdf(data: bytes, doc: str, use_ocr: bool | None = None) -> ParsedDocument:
    import pypdfium2 as pdfium  # noqa: PLC0415

    try:
        pdf = pdfium.PdfDocument(data)
    except pdfium.PdfiumError as exc:
        raise CorruptDocument("не удалось открыть PDF") from exc
    ocr_ok = tesseract_available() if use_ocr is None else use_ocr
    raw_pages: list[str] = []
    low_conf: list[int] = []
    ocr_pages: list[int] = []
    try:
        for i in range(len(pdf)):
            page = pdf[i]
            textpage = page.get_textpage()
            text = textpage.get_text_range().replace("\r\n", "\n")
            textpage.close()
            if len(re.sub(r"\s", "", text)) < TEXT_LAYER_MIN_CHARS:
                if ocr_ok:
                    bitmap = page.render(scale=OCR_DPI / 72, grayscale=True)
                    buf = io.BytesIO()
                    bitmap.to_pil().save(buf, format="PNG")
                    text, conf = ocr_image(buf.getvalue())
                    ocr_pages.append(i + 1)
                    if conf < OCR_LOW_CONF or not text.strip():
                        low_conf.append(i + 1)
                else:
                    low_conf.append(i + 1)  # скан без OCR — страницу никто не прочитал
            raw_pages.append(text)
            page.close()
    finally:
        pdf.close()
    if ocr_pages:
        log.info("%s: OCR на %d стр., низкая уверенность: %s", doc, len(ocr_pages), low_conf)
    elif low_conf and not ocr_ok:
        log.warning("%s: %d стр. без текстового слоя, tesseract недоступен", doc, len(low_conf))

    pages = postprocess_pages(raw_pages)
    chunks = chunk_text(_page_marked(pages), default_doc=doc)
    if not chunks:  # нет нумерованных пунктов — чанк = страница
        chunks = [Chunk(doc=doc, section=None, clauses=[Clause(f"стр. {i}", " ".join(t.split()), i)])
                  for i, t in enumerate(pages, start=1) if t.strip()]
    for ch in chunks:
        ch.doc = doc
    return ParsedDocument(doc=doc, kind="pdf", chunks=chunks, low_confidence_pages=low_conf, ocr_pages=ocr_pages)


# --- PPTX ---

def _shape_texts(shape) -> list[str]:
    out: list[str] = []
    if getattr(shape, "has_table", False) and shape.has_table:
        for row in shape.table.rows:
            cells = [" ".join(c.text.split()) for c in row.cells]
            if any(cells):
                out.append(" | ".join(cells))
    elif getattr(shape, "has_text_frame", False) and shape.has_text_frame:
        t = "\n".join(p.text for p in shape.text_frame.paragraphs if p.text.strip())
        if t.strip():
            out.append(t)
    if getattr(shape, "shape_type", None) == 6 and hasattr(shape, "shapes"):  # группа
        for sub in shape.shapes:
            out.extend(_shape_texts(sub))
    return out


def parse_pptx(data: bytes, doc: str) -> ParsedDocument:
    from pptx import Presentation  # noqa: PLC0415

    try:
        prs = Presentation(io.BytesIO(data))
    except Exception as exc:  # битый zip, не pptx
        raise CorruptDocument("не удалось открыть PPTX") from exc
    chunks: list[Chunk] = []
    empty: list[int] = []
    for n, slide in enumerate(prs.slides, start=1):
        title_shape = slide.shapes.title
        title = " ".join(title_shape.text.split()) if title_shape is not None and title_shape.has_text_frame else ""
        parts = []
        for shape in slide.shapes:
            if title_shape is not None and shape.shape_id == title_shape.shape_id:
                continue
            parts.extend(_shape_texts(shape))
        if slide.has_notes_slide:
            notes = slide.notes_slide.notes_text_frame.text if slide.notes_slide.notes_text_frame else ""
            if notes.strip():
                parts.append(f"Заметки: {notes.strip()}")
        body = "\n".join(([title] if title else []) + parts).strip()
        if not body:
            empty.append(n)  # слайд-картинка: текста нет, OCR слайдов не делаем
            continue
        chunks.append(Chunk(doc=doc, section=title or None,
                            clauses=[Clause(f"слайд {n}", " ".join(body.split()), n)]))
    return ParsedDocument(doc=doc, kind="pptx", chunks=chunks, low_confidence_pages=empty)


# --- XLSX ---

def _cell(v) -> str:
    if v is None:
        return ""
    if isinstance(v, float) and v.is_integer():
        v = int(v)
    return " ".join(str(v).split())


def parse_xlsx(data: bytes, doc: str) -> ParsedDocument:
    from openpyxl import load_workbook  # noqa: PLC0415

    try:
        wb = load_workbook(io.BytesIO(data), read_only=True, data_only=True)
    except Exception as exc:
        raise CorruptDocument("не удалось открыть XLSX") from exc
    chunks: list[Chunk] = []
    try:
        sheets = wb.worksheets
        for ws in sheets:
            sheet_doc = doc if len(sheets) == 1 else f"{doc} / {ws.title}"
            header: list[str] | None = None
            for r, row in enumerate(ws.iter_rows(values_only=True), start=1):
                cells = [_cell(v) for v in row]
                if not any(cells):
                    continue
                if header is None:
                    header = [c or f"Колонка {i}" for i, c in enumerate(cells, start=1)]
                    continue
                pairs = [f"{header[i] if i < len(header) else f'Колонка {i + 1}'}: {c}"
                         for i, c in enumerate(cells) if c]
                chunks.append(Chunk(doc=sheet_doc, section=ws.title,
                                    clauses=[Clause(f"строка {r}", " | ".join(pairs), None)]))
    finally:
        wb.close()
    return ParsedDocument(doc=doc, kind="xlsx", chunks=chunks)


# --- диспетчер ---

def parse_document(filename: str, data: bytes, doc: str | None = None) -> ParsedDocument:
    suffix = Path(filename).suffix.lower()
    name = (doc or "").strip() or Path(filename).stem or "документ"
    if suffix == ".pdf":
        return parse_pdf(data, name)
    if suffix == ".pptx":
        return parse_pptx(data, name)
    if suffix == ".xlsx":
        return parse_xlsx(data, name)
    if suffix in (".txt", ".md"):
        try:
            text = data.decode("utf-8-sig")
        except UnicodeDecodeError as exc:
            raise CorruptDocument("текст должен быть в UTF-8") from exc
        chunks = chunk_text(text, default_doc=name)
        if doc:
            for ch in chunks:
                ch.doc = name
        return ParsedDocument(doc=chunks[0].doc if chunks else name, kind="text", chunks=chunks)
    raise UnsupportedDocument(f"формат {suffix or '(без расширения)'} не поддерживается; "
                              f"ожидается {', '.join(sorted(SUPPORTED))}")
