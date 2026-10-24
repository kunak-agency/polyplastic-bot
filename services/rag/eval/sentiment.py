"""Качество тональности POST /flags на ручной разметке `eval/sentiment-gold.csv` (50 придуманных
текстов новичков, не из материалов заказчика): accuracy и macro-F1 по pos/neu/neg.

    .venv/bin/python -m eval.sentiment                       # rag на 127.0.0.1:8093
    RAG_BASE_URL=... RAG_API_KEY=... .venv/bin/python -m eval.sentiment --json out.json

Ключ берётся из окружения и никуда не печатается. Ответ degraded (LLM недоступна) в метрики не
идёт и считается отдельно: «не размечено» ≠ «нейтрально».
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import sys
import time
from pathlib import Path

import httpx

LABELS = ("pos", "neu", "neg")
GOLD = Path(__file__).resolve().parent / "sentiment-gold.csv"


def metrics(pairs: list[tuple[str, str]]) -> dict:
    """pairs — (gold, pred). Accuracy, macro-F1, F1 по классам и матрица ошибок."""
    n = len(pairs)
    acc = sum(g == p for g, p in pairs) / n if n else 0.0
    f1s = {}
    for c in LABELS:
        tp = sum(g == c and p == c for g, p in pairs)
        fp = sum(g != c and p == c for g, p in pairs)
        fn = sum(g == c and p != c for g, p in pairs)
        prec = tp / (tp + fp) if tp + fp else 0.0
        rec = tp / (tp + fn) if tp + fn else 0.0
        f1s[c] = 2 * prec * rec / (prec + rec) if prec + rec else 0.0
    confusion = {g: {p: sum(x == g and y == p for x, y in pairs) for p in LABELS} for g in LABELS}
    return {"n": n, "accuracy": acc, "macro_f1": sum(f1s.values()) / len(LABELS), "f1": f1s, "confusion": confusion}


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--gold", default=str(GOLD))
    ap.add_argument("--base-url", default=os.getenv("RAG_BASE_URL", "http://127.0.0.1:8093"))
    ap.add_argument("--json", default="")
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args(argv)
    rows = list(csv.DictReader(open(args.gold, encoding="utf-8")))
    headers = {}
    if key := os.getenv("RAG_API_KEY", "").strip():
        headers["Authorization"] = f"Bearer {key}"
    pairs, degraded, errors, lat = [], 0, 0, []
    details = []
    with httpx.Client(base_url=args.base_url.rstrip("/"), headers=headers, timeout=40) as c:
        for r in rows:
            body = {"text": r["text"], "context": r.get("context") or "free_text"}
            t0 = time.perf_counter()
            try:
                resp = c.post("/flags", json=body)
                resp.raise_for_status()
                out = resp.json()
            except Exception as e:  # сеть, 401, 5xx — строка не размечена
                errors += 1
                print(f"ошибка /flags: {type(e).__name__}", file=sys.stderr)
                continue
            lat.append((time.perf_counter() - t0) * 1000)
            if out.get("degraded"):
                degraded += 1
                continue
            pred = out.get("sentiment", "neu")
            pairs.append((r["gold"], pred))
            details.append({"text": r["text"], "gold": r["gold"], "pred": pred, "flags": [f["code"] for f in out.get("flags", [])]})
            if args.verbose and pred != r["gold"]:
                print(f"  {r['gold']} → {pred}: {r['text']}")
    m = metrics(pairs)
    m.update({"total": len(rows), "degraded": degraded, "errors": errors,
              "p50_ms": sorted(lat)[len(lat) // 2] if lat else None})
    print(f"n={m['n']}/{m['total']} degraded={degraded} errors={errors}  accuracy={m['accuracy']:.2f}  macro-F1={m['macro_f1']:.2f}  "
          + "  ".join(f"F1[{c}]={m['f1'][c]:.2f}" for c in LABELS))
    print("матрица (строка — gold, столбец — pred pos/neu/neg):")
    for g in LABELS:
        print(f"  {g}: " + " ".join(f"{m['confusion'][g][p]:3d}" for p in LABELS))
    if args.json:
        Path(args.json).write_text(json.dumps({"metrics": m, "rows": details}, ensure_ascii=False, indent=1), encoding="utf-8")
    return 0 if m["n"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
