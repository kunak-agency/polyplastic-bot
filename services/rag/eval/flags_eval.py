"""Качество POST /flags на `eval/flags-gold.csv`: тон и флаги F0–F10.

60 придуманных текстов новичков (не из материалов заказчика): 20 написаны вручную (5 — «голос как
слышится»: без пунктуации, «э-э», обрывы), 20 по шаблонам, 20 вариативных. Синтетика, разметка —
один разметчик (агент), поэтому цифры — ориентир, а не замер на живых данных.

    .venv/bin/python -m eval.flags_eval                          # rag на 127.0.0.1:8093
    .venv/bin/python -m eval.flags_eval --base-url http://127.0.0.1:8094 --label v2 --json out.json -v

Метрики:
  - тон: accuracy, macro-F1, recall neg, матрица 3×3 (строка — эталон, столбец — ответ);
  - флаги: P/R по категориям с n ≥ 4 в эталоне, P/R критических F0/F5/F7/F9 вместе (микро),
    доля ложных флагов на чистых текстах (хоть один флаг там, где эталон пуст).
Ответ degraded в метрики не идёт и считается отдельно. Ключ RAG_API_KEY берётся из окружения и
не печатается.
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import sys
import time
from collections import Counter
from pathlib import Path

import httpx

from eval.sentiment import LABELS, metrics as tone_metrics

GOLD = Path(__file__).resolve().parent / "flags-gold.csv"
CRITICAL = ("F0", "F5", "F7", "F9")
MIN_SUPPORT = 4


def load_gold(path: str | Path = GOLD) -> list[dict]:
    rows = []
    for r in csv.DictReader(open(path, encoding="utf-8")):
        rows.append({
            "text": r["text"],
            "tone": r["tone"],
            "flags": tuple(c for c in (r.get("flags") or "").split(";") if c),
            "voice": (r.get("voice") or "0").strip() == "1",
            "source": r.get("source", ""),
        })
    return rows


def _pr(tp: int, fp: int, fn: int) -> dict:
    p = tp / (tp + fp) if tp + fp else 0.0
    r = tp / (tp + fn) if tp + fn else 0.0
    return {"tp": tp, "fp": fp, "fn": fn, "precision": p, "recall": r, "n": tp + fn}


def flag_metrics(pairs: list[tuple[tuple[str, ...], tuple[str, ...]]]) -> dict:
    """pairs — (эталонные коды, предсказанные коды) по текстам."""
    support = Counter(c for g, _ in pairs for c in g)
    per_code = {}
    for code in sorted(support, key=lambda c: int(c[1:])):
        if support[code] < MIN_SUPPORT:
            continue
        tp = sum(code in g and code in p for g, p in pairs)
        fp = sum(code not in g and code in p for g, p in pairs)
        fn = sum(code in g and code not in p for g, p in pairs)
        per_code[code] = _pr(tp, fp, fn)
    # F3/F4 — соседние классы («руководитель» против «наставник, коллеги»): отдельно и вместе
    grp = {"F3", "F4"}
    tp = sum(bool(grp & set(g)) and bool(grp & set(p)) for g, p in pairs)
    fp = sum(not grp & set(g) and bool(grp & set(p)) for g, p in pairs)
    fn = sum(bool(grp & set(g)) and not grp & set(p) for g, p in pairs)
    f34 = _pr(tp, fp, fn)
    tp = sum(len(set(CRITICAL) & set(g) & set(p)) for g, p in pairs)
    fp = sum(len((set(CRITICAL) & set(p)) - set(g)) for g, p in pairs)
    fn = sum(len((set(CRITICAL) & set(g)) - set(p)) for g, p in pairs)
    critical = _pr(tp, fp, fn)
    tp = sum(len(set(g) & set(p)) for g, p in pairs)
    fp = sum(len(set(p) - set(g)) for g, p in pairs)
    fn = sum(len(set(g) - set(p)) for g, p in pairs)
    micro = _pr(tp, fp, fn)
    clean = [p for g, p in pairs if not g]
    false_on_clean = sum(bool(p) for p in clean) / len(clean) if clean else 0.0
    missed_flagged = sum(not p for g, p in pairs if g) / max(1, sum(bool(g) for g, _ in pairs))
    return {"per_code": per_code, "f3_f4": f34, "critical": critical, "micro": micro,
            "clean_n": len(clean), "false_on_clean": false_on_clean, "missed_flagged": missed_flagged}


def tone_recall_neg(m: dict) -> float:
    row = m["confusion"]["neg"]
    total = sum(row.values())
    return row["neg"] / total if total else 0.0


def run(base_url: str, rows: list[dict], verbose: bool = False) -> dict:
    headers = {}
    if key := os.getenv("RAG_API_KEY", "").strip():
        headers["Authorization"] = f"Bearer {key}"
    tone_pairs, flag_pairs, details, lat = [], [], [], []
    degraded = errors = 0
    extras = Counter()
    with httpx.Client(base_url=base_url.rstrip("/"), headers=headers, timeout=60) as c:
        for r in rows:
            body = {"text": r["text"], "context": "voice" if r["voice"] else "free_text"}
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
            pred_tone = out.get("sentiment", "neu")
            pred_codes = tuple(f["code"] for f in out.get("flags", []))
            tone_pairs.append((r["tone"], pred_tone))
            flag_pairs.append((r["flags"], pred_codes))
            summary = out.get("summary")
            if summary is not None:
                extras["has_summary_field"] += 1
                extras["summary_nonempty"] += bool(summary)
                extras["summary_over_120"] += len(summary) > 120
                extras["summary_f10_nonempty"] += bool(summary) and "F10" in pred_codes
            if out.get("tone_confidence") is not None:
                extras["tone_confidence"] += 1
            if out.get("clarify_hint"):
                extras["clarify_hint"] += 1
            if out.get("quote"):
                extras["quote_nonempty"] += 1
            details.append({**{k: r[k] for k in ("text", "tone", "voice", "source")}, "gold_flags": list(r["flags"]),
                            "pred_tone": pred_tone, "pred_flags": [f"{f['code']}/{f['severity']}/{f['confidence']}" for f in out.get("flags", [])],
                            "tone_confidence": out.get("tone_confidence"), "summary": summary,
                            "clarify_hint": out.get("clarify_hint"), "quote": out.get("quote")})
            if verbose and (pred_tone != r["tone"] or set(pred_codes) != set(r["flags"])):
                print(f"  тон {r['tone']}→{pred_tone}  флаги {list(r['flags']) or '[]'}→{list(pred_codes) or '[]'}: {r['text']}")
    tm = tone_metrics(tone_pairs)
    tm["recall_neg"] = tone_recall_neg(tm) if tone_pairs else 0.0
    return {"total": len(rows), "degraded": degraded, "errors": errors,
            "p50_ms": sorted(lat)[len(lat) // 2] if lat else None,
            "tone": tm, "flags": flag_metrics(flag_pairs), "extras": dict(extras), "rows": details}


def print_report(res: dict, label: str = "") -> None:
    tm, fm = res["tone"], res["flags"]
    print(f"[{label}] n={tm['n']}/{res['total']} degraded={res['degraded']} errors={res['errors']} p50={res['p50_ms'] and round(res['p50_ms'])} мс")
    print(f"тон: accuracy={tm['accuracy']:.2f} macro-F1={tm['macro_f1']:.2f} recall neg={tm['recall_neg']:.2f}")
    print("матрица (строка — эталон, столбец — ответ pos/neu/neg):")
    for g in LABELS:
        print(f"  {g}: " + " ".join(f"{tm['confusion'][g][p]:3d}" for p in LABELS))
    print("флаги (n ≥ 4): " + "  ".join(f"{c} P={v['precision']:.2f} R={v['recall']:.2f} (n={v['n']})" for c, v in fm["per_code"].items()))
    v = fm["f3_f4"]
    print(f"F3∪F4: P={v['precision']:.2f} R={v['recall']:.2f} (n={v['n']})")
    v = fm["critical"]
    print(f"критические F0/F5/F7/F9: P={v['precision']:.2f} R={v['recall']:.2f} (tp={v['tp']} fp={v['fp']} fn={v['fn']})")
    v = fm["micro"]
    print(f"все флаги (микро): P={v['precision']:.2f} R={v['recall']:.2f}")
    print(f"ложные флаги на чистых: {fm['false_on_clean']:.2f} (из {fm['clean_n']});  текстов с флагом без ответа-флага: {fm['missed_flagged']:.2f}")
    if res["extras"]:
        print("поля v2: " + ", ".join(f"{k}={v}" for k, v in sorted(res["extras"].items())))


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--gold", default=str(GOLD))
    ap.add_argument("--base-url", default=os.getenv("RAG_BASE_URL", "http://127.0.0.1:8093"))
    ap.add_argument("--label", default="")
    ap.add_argument("--json", default="")
    ap.add_argument("-v", "--verbose", action="store_true")
    args = ap.parse_args(argv)
    res = run(args.base_url, load_gold(args.gold), args.verbose)
    print_report(res, args.label or args.base_url)
    if args.json:
        Path(args.json).write_text(json.dumps(res, ensure_ascii=False, indent=1), encoding="utf-8")
    return 0 if res["tone"]["n"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
