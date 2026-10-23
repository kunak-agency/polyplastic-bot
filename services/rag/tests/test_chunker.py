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
