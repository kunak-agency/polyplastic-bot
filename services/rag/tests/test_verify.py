import pytest

from textnorm import normalize_for_match
from verify import verify_sources, wrong_address_form

DOC = "ПВТР-синтетика"


def _by_first(chunks, cid):
    return next(ch for ch in chunks if ch.clause_id == cid)


def test_exact_quote_accepted_and_page_from_metadata(chunks):
    src = [{"doc": DOC, "clause_id": "6.1", "quote": "аванс за первую половину месяца выплачивается 25-го числа"}]
    res = verify_sources(src, [_by_first(chunks, "6.1")])
    assert res.ok, res.errors
    assert res.sources[0].page == 3


def test_quote_normalization_case_yo_punctuation_homoglyphs(chunks):
    # «ё»→«е», регистр, кавычки/тире, латинская «o» и «c» вместо кириллических
    quote = "«ОЧЕРЕДНОСТЬ отпусков определяется графиком отпусков» —"
    quote = quote.replace("о", "o", 1).replace("с", "c", 1)
    res = verify_sources([{"doc": DOC, "clause_id": "4.2", "quote": quote}], [_by_first(chunks, "4.2")])
    assert res.ok, res.errors


def test_fabricated_clause_id_rejected(chunks):
    src = [{"doc": DOC, "clause_id": "6.7.8", "quote": "аванс за первую половину месяца"}]
    res = verify_sources(src, [_by_first(chunks, "6.1")])
    assert not res.ok
    assert "не среди найденных" in res.errors[0]


def test_clause_not_retrieved_rejected(chunks):
    # 4.2 существует в корпусе, но не был найден для этого вопроса
    src = [{"doc": DOC, "clause_id": "4.2", "quote": "Очерёдность отпусков определяется графиком отпусков"}]
    assert not verify_sources(src, [_by_first(chunks, "6.1")]).ok


def test_paraphrase_rejected(chunks):
    src = [{"doc": DOC, "clause_id": "6.1", "quote": "зарплату платят дважды в месяц"}]
    assert not verify_sources(src, [_by_first(chunks, "6.1")]).ok


def test_quote_from_other_clause_of_merged_chunk_rejected(chunks):
    merged = _by_first(chunks, "1.1")
    assert merged.clause_ids == ["1.1", "1.2"]
    src = [{"doc": DOC, "clause_id": "1.1", "quote": "знакомят под подпись до начала работы"}]  # это текст 1.2
    assert not verify_sources(src, [merged]).ok
    src[0]["clause_id"] = "1.2"
    assert verify_sources(src, [merged]).ok


def test_wrong_doc_rejected(chunks):
    src = [{"doc": "Другой документ", "clause_id": "6.1", "quote": "аванс за первую половину месяца"}]
    assert not verify_sources(src, [_by_first(chunks, "6.1")]).ok


def test_too_short_quote_rejected(chunks):
    src = [{"doc": DOC, "clause_id": "6.1", "quote": "аванс"}]
    assert not verify_sources(src, [_by_first(chunks, "6.1")]).ok


def test_empty_sources_rejected(chunks):
    assert not verify_sources([], [_by_first(chunks, "6.1")]).ok


def test_one_bad_source_fails_whole_answer(chunks):
    good = {"doc": DOC, "clause_id": "6.1", "quote": "аванс за первую половину месяца"}
    bad = {"doc": DOC, "clause_id": "9.9", "quote": "выдуманный пункт про ДМС"}
    assert not verify_sources([good, bad], [_by_first(chunks, "6.1")]).ok


def test_normalize_for_match():
    assert normalize_for_match("  Ёлка,\n «ТЕСТ» — 9:00 ") == "елка тест 9 00"


# --- форма обращения ---


@pytest.mark.parametrize("text,bad", [
    ("В офисе ты работаешь с 9:00 до 18:00.", ["ты", "работаешь"]),
    ("Тебе выдадут пропуск, его получишь на проходной.", ["Тебе", "получишь"]),
    ("Если опоздаешь, обратись к руководителю.", ["опоздаешь", "обратись"]),
    ("Твоя смена начинается в 20:00, не ошибёшься.", ["Твоя", "ошибёшься"]),
])
def test_ty_forms_detected_for_vy(text, bad):
    assert wrong_address_form(text, "vy") == bad
    assert wrong_address_form(text, None) == bad  # по умолчанию «вы»
    assert wrong_address_form(text, "ty") == []


@pytest.mark.parametrize("text,bad", [
    ("Вы работаете с 9:00 до 18:00.", ["Вы", "работаете"]),
    ("Пропуск выдадут вам на проходной, ваш руководитель подаст заявку.", ["вам", "ваш"]),
    ("Обратитесь к руководителю, он всё расскажет. ВЫ можете уточнить у HR.", ["Обратитесь", "ВЫ", "можете"]),
])
def test_vy_forms_detected_for_ty(text, bad):
    assert wrong_address_form(text, "ty") == bad
    assert wrong_address_form(text, "vy") == []


@pytest.mark.parametrize("text", [
    "Рабочий день длится с 9:00 до 18:00.",
    "Аванс выплачивают 25-го числа, выдают его лишь на карту.",   # «выплачивают», «лишь»
    "Доплата — до 5 тыс. рублей, сумма указана в анкете и в лимите на визите.",  # -ете/-ите в сущ.
    "Тысяча рублей выплачивается при выходе в выходной день.",
    "Сотрудник обязан явиться вовремя; выход без СИЗ запрещён.",
    "Пункт: «вы можете уйти в 18:00» и «ты получишь пропуск».",  # кавычки — цитата документа
    "",
])
def test_impersonal_and_quoted_pass_both_forms(text):
    assert wrong_address_form(text, "ty") == []
    assert wrong_address_form(text, "vy") == []
