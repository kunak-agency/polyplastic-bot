from chunker import CLAUSE_RE, MAX_WORDS, chunk_text, parse_clauses


def test_corpus_has_15_clauses_with_metadata(chunks):
    ids = [cid for ch in chunks for cid in ch.clause_ids]
    assert len(ids) == 15
    assert ids[0] == "1.1" and ids[-1] == "6.1"
    assert "3.2.1" in ids and "4.1.1" in ids
    assert all(ch.doc == "ПВТР-синтетика" for ch in chunks)
    assert all(ch.section for ch in chunks)
    assert all("СИНТЕТ" in (ch.title or "") for ch in chunks)


def test_short_clause_merges_with_same_parent_neighbour(chunks):
    first = chunks[0]
    assert first.clause_ids == ["1.1", "1.2"]
    assert first.section == "Общие положения"


def test_short_clause_without_same_parent_neighbour_stays_alone(chunks):
    by_first = {ch.clause_id: ch for ch in chunks}
    # 3.3 короткий, но соседи 3.2.1 (родитель 3.2) и 4.1 (родитель 4) — другие родители
    assert by_first["3.3"].clause_ids == ["3.3"]
    # 4.1 и 4.1.1 не склеиваются: у 4.1.1 родитель 4.1, а не 4
    assert by_first["4.1"].clause_ids == ["4.1"]
    assert by_first["4.1.1"].clause_ids == ["4.1.1"]


def test_pages_from_markers(chunks):
    pages = {cl.clause_id: cl.page for ch in chunks for cl in ch.clauses}
    assert pages["1.1"] == 1
    assert pages["3.1"] == 2
    assert pages["6.1"] == 3


def test_preamble_is_not_indexed(chunks):
    assert not any("СИНТЕТИЧЕСКИЙ ДОКУМЕНТ" in ch.body for ch in chunks)


def test_clause_regex():
    assert CLAUSE_RE.match("6.7.8. Текст").group(1) == "6.7.8"
    assert CLAUSE_RE.match("6.7 Текст").group(1) == "6.7"
    assert CLAUSE_RE.match("6. Раздел") is None
    assert CLAUSE_RE.match("2026 год") is None


def test_continuation_lines_form_page_breaks_and_default_doc():
    text = "1. Раздел\n1.1. Первая строка\nпродолжение пункта\n\f1.2. Второй пункт на второй странице"
    meta, parsed = parse_clauses(text)
    assert meta == {}
    clauses = [c for _, c in parsed]
    assert clauses[0].text == "Первая строка продолжение пункта"
    assert (clauses[0].page, clauses[1].page) == (1, 2)
    out = chunk_text(text, default_doc="file-stem")
    assert out[0].doc == "file-stem"
    assert out[0].clause_ids == ["1.1", "1.2"]  # оба короткие, один родитель


def test_merge_respects_max_words():
    long = " ".join(["слово"] * (MAX_WORDS - 1))
    text = f"1. Раздел\n1.1. {long}\n1.2. короткий пункт"
    out = chunk_text(text, default_doc="d")
    assert [c.clause_ids for c in out] == [["1.1"], ["1.2"]]


# --- формат настоящего ПВТР (тексты синтетические) ---

def test_subsection_heading_becomes_section_of_its_clauses():
    text = ("6. ПРАВИЛА\n6.1. Порядок приема работников\n6.1.1. Трудовой договор заключается письменно.\n"
            "6.1.2. Работника знакомят с правилами под подпись.\n6.2. Короткий пункт без подпунктов.")
    _, parsed = parse_clauses(text)
    assert [(s, c.clause_id) for s, c in parsed] == [
        ("Порядок приема работников", "6.1.1"), ("Порядок приема работников", "6.1.2"), ("ПРАВИЛА", "6.2")]


def test_long_heading_with_inner_dots_is_heading_but_clause_with_period_is_not():
    text = ("6.10. Трудовая дисциплина. Ответственность за нарушение трудовой\nдисциплины. Материальная ответственность\n"
            "6.10.1. Пункт.\n4.1. Отпуск 28 календарных дней.\n4.1.1. Подпункт.")
    _, parsed = parse_clauses(text)
    sections = {c.clause_id: s for s, c in parsed}
    assert "6.10" not in sections and sections["6.10.1"].startswith("Трудовая дисциплина.")
    assert "4.1" in sections  # кончается точкой — обычный пункт, хоть у него и есть подпункт


def test_section_without_numbered_clauses_is_one_clause_but_toc_and_intro_are_not():
    text = ("3. ТЕРМИНЫ ........ 3\n7. ЗАКЛЮЧЕНИЕ ....\nмусор оглавления\n"
            "3. ТЕРМИНЫ\nрабочее время: время исполнения обязанностей;\nвремя отдыха: свободное время.\n"
            "7. ЗАКЛЮЧЕНИЕ\nвводный текст раздела\n7.1. Правила обязательны для всех.")
    _, parsed = parse_clauses(text)
    assert [c.clause_id for _, c in parsed] == ["3", "7.1"]
    assert parsed[0][0] == "ТЕРМИНЫ" and parsed[0][1].text.startswith("рабочее время")


def test_ocr_glued_clause_number_and_document_code_are_handled():
    assert CLAUSE_RE.match("6.3.11.В случае задержки").group(1) == "6.3.11"
    assert CLAUSE_RE.match("6.10.24. Текст").group(1) == "6.10.24"
    assert CLAUSE_RE.match("3.1107 | ПоложениЕ") is None   # шифр П 3.11-07 после OCR


def test_long_clause_is_split_into_verbatim_parts_with_lead_in_section():
    items = [f"— обязанность номер {i} " + " ".join(["слово"] * 20) + ";" for i in range(12)]
    text = "6. ПРАВИЛА\n6.4. Права и обязанности\n6.4.2. Работник обязан: " + " ".join(items) + \
        "\n6.4.3. Следующий пункт."
    chunks = chunk_text(text, default_doc="d")
    parts = [c for c in chunks if c.clause_id.startswith("6.4.2/")]
    assert len(parts) >= 2 and all(p.words <= 150 for p in parts)
    assert parts[0].clause_id == "6.4.2/1" and parts[0].clauses[0].text.startswith("Работник обязан:")
    assert parts[1].section == "Права и обязанности. Работник обязан"
    assert " ".join(p.clauses[0].text for p in parts) == "Работник обязан: " + " ".join(items)
    assert parts[1].clauses[0].parent == "6.4.2" and parts[1].clauses[0].base_id == "6.4.2"


def test_dash_inside_sentence_is_not_a_list_boundary():
    from chunker import Clause, split_long_clause
    body = " ".join(["слово"] * 120) + " РФ - до 14 календарных дней - " + " ".join(["слово"] * 120) + "."
    out = split_long_clause(None, Clause("6.7.15", body, 1))
    assert [c.clause_id for _, c in out] == ["6.7.15"]  # ни «;» перед тире, ни второго предложения


def test_load_corpus_reads_several_dirs_and_pptx(tmp_path):
    from chunker import load_corpus
    from tests.test_parsers import _pptx
    a, b = tmp_path / "a", tmp_path / "b"
    a.mkdir(), b.mkdir()
    (a / "reg.md").write_text("---\ndoc: Регламент\n---\n1. Раздел\n1.1. Пункт про пропуск.\n", encoding="utf-8")
    (b / "welcome.pptx").write_bytes(_pptx())
    (b / "~$welcome.pptx").write_bytes(b"lock")  # lock-файл PowerPoint пропускается
    chunks = load_corpus(f"{a},{b}, {tmp_path / 'нет'}")
    assert {c.doc for c in chunks} == {"Регламент", "welcome"}
    assert [c.clause_id for c in chunks if c.doc == "welcome"] == ["слайд 1", "слайд 2"]
