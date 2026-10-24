"""Ручная разметка тональности и метрики eval.sentiment (без сети)."""

import csv
from collections import Counter

from eval.sentiment import GOLD, LABELS, metrics


def test_gold_shape():
    rows = list(csv.DictReader(open(GOLD, encoding="utf-8")))
    assert len(rows) == 50
    assert {r["gold"] for r in rows} <= set(LABELS)
    assert min(Counter(r["gold"] for r in rows).values()) >= 10
    assert len({r["text"] for r in rows}) == 50


def test_metrics():
    m = metrics([("pos", "pos"), ("neu", "neu"), ("neg", "neg"), ("neg", "neu")])
    assert m["accuracy"] == 0.75
    assert m["confusion"]["neg"]["neu"] == 1
    assert abs(m["f1"]["pos"] - 1.0) < 1e-9
    assert abs(m["macro_f1"] - (1 + 2 / 3 * 1 + 2 / 3) / 3) < 1e-9
