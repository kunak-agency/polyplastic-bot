"""Оценочный набор флагов и тона и метрики eval.flags_eval (без сети)."""

from collections import Counter

from eval.flags_eval import CRITICAL, flag_metrics, load_gold
from eval.sentiment import LABELS


def test_gold_shape():
    rows = load_gold()
    assert len(rows) == 60 and len({r["text"] for r in rows}) == 60
    assert {r["tone"] for r in rows} <= set(LABELS)
    tones = Counter(r["tone"] for r in rows)
    assert tones["pos"] >= 8 and tones["neu"] >= 15 and tones["neg"] >= 20
    codes = Counter(c for r in rows for c in r["flags"])
    for c in ("F1", "F2", "F3", "F4", "F6", "F7"):
        assert 4 <= codes[c] <= 6, c
    for c in ("F0", "F5", "F9", "F10"):
        assert codes[c] == 2, c
    assert sum(not r["flags"] for r in rows) == 15
    assert sum(r["voice"] for r in rows) == 5
    assert Counter(r["source"] for r in rows) == {"manual": 20, "template": 20, "variant": 20}


def test_flag_metrics():
    pairs = [(("F7",), ("F7",)), (("F5",), ()), ((), ("F2",)), ((), ()), (("F3",), ("F4",)), (("F2",), ("F2",))] * 1
    pairs += [(("F2",), ("F2",)), (("F2",), ()), (("F2", "F6"), ("F2",))]
    m = flag_metrics(pairs)
    assert set(m["per_code"]) == {"F2"}  # n ≥ 4 только у F2
    f2 = m["per_code"]["F2"]
    assert (f2["tp"], f2["fp"], f2["fn"]) == (3, 1, 1)
    assert m["critical"]["tp"] == 1 and m["critical"]["fn"] == 1 and m["critical"]["fp"] == 0
    assert m["f3_f4"]["tp"] == 1
    assert m["false_on_clean"] == 0.5
    assert set(CRITICAL) == {"F0", "F5", "F7", "F9"}
