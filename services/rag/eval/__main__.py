"""Оценка RAG на golden set: `python -m eval` или `./run.sh eval` из services/rag.

Сравнивает конфигурации на eval/golden.csv по синтетическому регламенту из testdata:
  bm25   — BM25 + лексический gate (покрытие по IDF, порог 0.5), как в итерации 1;
  final  — BM25 + bge-m3 через RRF k=60 → bge-reranker-v2-m3 → gate по sigmoid;
  final+llm — final + генерация LLM (промпт rag-answer v2) и проверки ответа кодом. Только если
           задан LLM_BASE_URL (LLM_API_KEY, LLM_MODEL, LLM_STRUCTURED — из окружения). С
           --presence-penalty 1.0,0.3 прогоняется по разу на каждое значение (A/B из
           agency/prompts/README.md).

Метрики (детерминированные, без LLM):
  Hit@5         — доля отвечаемых вопросов, где ожидаемый пункт есть среди top-5 чанков;
  MRR@5         — 1/ранг первого чанка с ожидаемым пунктом (0, если его нет в top-5);
  answer-acc    — доля отвечаемых, где бот ответил и сослался на ожидаемый пункт;
  false-refusal — доля отвечаемых, где бот отказал;
  abstain-acc   — доля ловушек вне корпуса, где бот отказал (needs_escalation);
  injection-safe— инъекция отклонена или отвечена только дословной цитатой ожидаемого пункта;
  citation-valid— доля ответов, где каждая цитата дословно входит в указанный пункт корпуса
                  (перепроверка по всему корпусу, независимо от verify внутри конвейера);
  p50/p95       — задержка `Pipeline.ask` в мс: модели и LLM прогреты `Pipeline.warmup` (как на
                  старте сервиса), LRU-кэши вопросов сброшены перед каждым вопросом.
Только для конфигураций с LLM (в остальных «—»):
  llm-verify-pass — доля ответов LLM (не отказ, не fallback), прошедших verify цитат, проверку
                  чисел и длины; обрезанный по предложению ответ считается прошедшим;
  llm-fallback  — доля вызовов LLM, закончившихся экстрактивным ответом (сеть, HTTP, не JSON,
                  не по схеме, обрезка по max_tokens);
  llm-declined  — доля вызовов LLM, где модель сама вернула needs_escalation;
  разбивка отказов проверок (verify_failed / foreign_numbers / too_long / truncated) — в --verbose.

Модификаторы конфигурации (через `+`, после final / final+llm):
  syn  — словарь synonyms.yaml в retrieve и реранкере (RAG_SYNONYMS=rerank);
  synr — словарь только в retrieve (RAG_SYNONYMS=retrieve);
  rw   — LLM-переформулировка вопроса при провале gate (RAG_REWRITE=1, нужен LLM_BASE_URL).
  Например `final+syn+rw` — без генерации, ответ дословной цитатой; `final+llm+syn+rw` — всё.

Флаги: --configs bm25,final,final+llm  --presence-penalty 1.0,0.3  --threshold 0.3  --sweep
       --rerank-top 8  --golden eval/golden-real.csv  --corpus corpus-real  --real
       --json out.json  --verbose

--real — golden set и корпус настоящих документов ПОЛИПЛАСТИК (`eval/golden-real.csv`,
`corpus-real/`). Оба в .gitignore: это коммерческая тайна заказчика.

expected_clause_ids через `;`. Элемент — номер пункта (`6.6.1`) или `документ#пункт`
(`Антикоррупционная политика#слайд 8`), когда номера слайдов повторяются в разных документах.
Номер без части (`6.4.2`) засчитывает любую часть длинного пункта (`6.4.2/5`).
"""

from __future__ import annotations

import argparse
import csv
import dataclasses
import json
import logging
import os
import statistics
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))
os.environ.setdefault("HF_HOME", str(ROOT / ".cache"))

from chunker import load_corpus  # noqa: E402
from llm import LLMClient, LLMConfig  # noqa: E402
from pipeline import DEFAULT_GATE, Pipeline, Settings  # noqa: E402
from rewrite import QueryRewriter  # noqa: E402
from verify import verify_sources  # noqa: E402

GOLDEN = ROOT / "eval" / "golden.csv"
CORPUS = ROOT / "testdata" / "corpus"
GOLDEN_REAL = ROOT / "eval" / "golden-real.csv"
CORPUS_REAL = ROOT / "corpus-real"


def key(doc: str, clause_id: str) -> str:
    return f"{doc}#{clause_id}"


def matches(expected: str, k: str) -> bool:
    """Ожидаемый пункт `6.4.2` / `док#слайд 8` против ключа `док#6.4.2/5`."""
    doc, _, cid = k.partition("#")
    exp_doc, sep, exp_cid = expected.rpartition("#")
    if sep and exp_doc != doc:
        return False
    return cid == exp_cid or cid.split("/", 1)[0] == exp_cid


def any_match(expected: list[str], keys: list[str]) -> bool:
    return any(matches(e, k) for e in expected for k in keys)


def all_match(expected: list[str], keys: list[str]) -> bool:
    return bool(keys) and all(any(matches(e, k) for e in expected) for k in keys)


@dataclass
class Item:
    question: str
    expected: list[str]
    is_trap: bool
    topic: str
    kind: str  # literal | paraphrase | trap | injection


@dataclass
class Row:
    item: Item
    ranked: list[list[str]]       # ключи `док#пункт` top-5 чанков
    gate: float                   # max gate_score по top-5 (до решения gate)
    answered: bool
    cited: list[str]              # ключи `док#пункт` источников ответа
    abstain_reason: str | None
    citation_ok: bool
    latency_ms: float
    extractive: bool = False
    mode: str | None = None       # extractive | llm | extractive_fallback | None
    check: str | None = None      # см. pipeline.Answer.check
    rewrites: int = 0             # формулировок LLM в повторном поиске
    rewrite_used: bool = False    # gate прошёл благодаря переформулировке


@dataclass
class Report:
    name: str
    rows: list[Row] = field(default_factory=list)
    load_s: float = 0.0
    threshold: float = 0.0


def load_golden(path: Path = GOLDEN) -> list[Item]:
    with path.open(encoding="utf-8") as f:
        return [Item(question=r["question"].strip(),
                     expected=[c for c in r["expected_clause_ids"].split(";") if c.strip()],
                     is_trap=r["is_trap"].strip() == "1", topic=r["topic"].strip(),
                     kind=(r.get("kind") or ("trap" if r["is_trap"].strip() == "1" else "literal")).strip())
                for r in csv.DictReader(f)]


RERANK_TOP: int | None = None  # --rerank-top; None — значение по умолчанию из pipeline
TOP_K: int | None = None       # --top-k: сколько чанков уходит в LLM и в проверку цитат


CORPUS_DIR: Path | str = CORPUS  # --corpus / --real


REWRITE_TIMEOUT: float | None = None  # --rewrite-timeout


def split_config(name: str) -> tuple[str, set[str]]:
    """`final+llm+syn+rw` → ("final+llm", {"syn", "rw"}). Модификаторы: syn — словарь в
    retrieve и реранкер, synr — словарь только в retrieve, rw — LLM-переформулировка."""
    parts = name.split("+")
    base = "+".join(parts[:2]) if parts[1:2] == ["llm"] else parts[0]
    mods = set(parts[2:] if base.endswith("+llm") else parts[1:])
    unknown = mods - {"syn", "synr", "rw"}
    if unknown:
        raise SystemExit(f"неизвестные модификаторы {sorted(unknown)} в {name!r}: syn | synr | rw")
    return base, mods


def make_settings(name: str, threshold: float | None) -> Settings:
    base, mods = split_config(name)
    s = _make_settings(base, threshold)
    s.synonyms = "rerank" if "syn" in mods else "retrieve" if "synr" in mods else "off"
    s.rewrite = "rw" in mods
    s.rewrite_speculative = (os.getenv("RAG_REWRITE_SPECULATIVE") or "0") in ("1", "true", "on", "yes")
    if REWRITE_TIMEOUT:
        s.rewrite_timeout = REWRITE_TIMEOUT
    if RERANK_TOP:
        s.rerank_top = RERANK_TOP
    if TOP_K:
        s.top_k = TOP_K
    return s


def _make_settings(name: str, threshold: float | None) -> Settings:
    if name == "bm25":
        return Settings(corpus_dir=str(CORPUS_DIR), retriever="bm25", reranker="none",
                        gate_threshold=threshold if threshold is not None else DEFAULT_GATE["lexical"])
    if name in ("final", "final+llm"):
        return Settings(corpus_dir=str(CORPUS_DIR), retriever="hybrid", reranker="bge",
                        gate_threshold=threshold if threshold is not None else DEFAULT_GATE["reranker"])
    if name == "hybrid":  # без реранкера — для разбора вклада компонентов
        return Settings(corpus_dir=str(CORPUS_DIR), retriever="hybrid", reranker="none",
                        gate_threshold=threshold if threshold is not None else DEFAULT_GATE["lexical"])
    raise SystemExit(f"неизвестная конфигурация {name!r}: bm25 | hybrid | final | final+llm")


def run_config(name: str, items: list[Item], chunks, threshold: float | None, generator=None,
               label: str | None = None, llm_client=None) -> Report:
    settings = make_settings(name, threshold)
    t0 = time.perf_counter()
    rewriter = None
    if settings.rewrite:
        if llm_client is None:
            raise SystemExit(f"{name}: для rw нужен LLM_BASE_URL")
        rewriter = QueryRewriter(llm_client, timeout_s=settings.rewrite_timeout, max_queries=settings.rewrite_max)
    pipe = Pipeline(settings, chunks, generator=generator, rewriter=rewriter)
    name = label or name
    rep = Report(name=name, load_s=time.perf_counter() - t0, threshold=settings.gate_threshold)
    pipe.warmup()  # то же, что на старте сервиса: модели и LLM прогреты
    for it in items:
        pipe.clear_caches()  # меряем холодный вопрос: без LRU эмбеддинга и скоров реранкера
        t1 = time.perf_counter()
        ans = pipe.ask(it.question)
        latency = (time.perf_counter() - t1) * 1000
        hits = ans.hits or pipe.retrieve(it.question)
        cited = [key(s.doc, s.clause_id) for s in ans.sources]
        raw = [{"doc": s.doc, "clause_id": s.clause_id, "quote": s.quote} for s in ans.sources]
        citation_ok = bool(raw) and verify_sources(raw, chunks).ok
        rep.rows.append(Row(item=it, ranked=[[key(h.chunk.doc, c) for c in h.chunk.clause_ids] for h in hits],
                            gate=ans.gate if ans.gate is not None else max((h.gate_score for h in hits), default=0.0),
                            answered=not ans.needs_escalation, cited=cited, abstain_reason=ans.abstain_reason,
                            citation_ok=citation_ok, latency_ms=latency,
                            extractive=bool(ans.answer) and all(ans.answer == s.quote for s in ans.sources),
                            mode=ans.mode, check=ans.check, rewrites=ans.rewrites,
                            rewrite_used=ans.rewrite_used))
    return rep


def _rank(row: Row) -> int | None:
    for i, ids in enumerate(row.ranked[:5], start=1):
        if any_match(row.item.expected, ids):
            return i
    return None


def _pct(xs: list[bool]) -> float:
    return sum(xs) / len(xs) if xs else float("nan")


def metrics(rep: Report) -> dict:
    answerable = [r for r in rep.rows if not r.item.is_trap]
    traps = [r for r in rep.rows if r.item.kind == "trap"]
    inj = [r for r in rep.rows if r.item.kind == "injection"]
    answered = [r for r in rep.rows if r.answered]

    def split(kind):
        return [r for r in answerable if r.item.kind == kind]

    def hit5(rows):
        return _pct([_rank(r) is not None for r in rows])

    def acc(rows):
        return _pct([r.answered and any_match(r.item.expected, r.cited) for r in rows])

    lat = sorted(r.latency_ms for r in rep.rows)
    llm_calls = [r for r in rep.rows if r.mode in ("llm", "extractive_fallback")]
    llm_answers = [r for r in llm_calls if r.mode == "llm" and r.check != "declined"]
    checks: dict[str, int] = {}
    for r in llm_calls:
        key = "fallback" if (r.check or "").startswith("fallback:") else (r.check or "-")
        checks[key] = checks.get(key, 0) + 1
    return {
        "config": rep.name,
        "threshold": rep.threshold,
        "n": len(rep.rows), "n_answerable": len(answerable), "n_traps": len(traps), "n_injections": len(inj),
        "hit@5": hit5(answerable),
        "hit@5_literal": hit5(split("literal")),
        "hit@5_paraphrase": hit5(split("paraphrase")),
        "mrr@5": statistics.fmean([1 / k if (k := _rank(r)) else 0.0 for r in answerable]) if answerable else 0.0,
        "answer_acc": acc(answerable),
        "answer_acc_literal": acc(split("literal")),
        "answer_acc_paraphrase": acc(split("paraphrase")),
        "false_refusal": _pct([not r.answered for r in answerable]),
        "abstain_acc": _pct([not r.answered for r in traps]),
        # LLM-ответ на инъекцию безопасен, если сослался только на ожидаемый пункт: он уже прошёл
        # verify и проверку чисел, то есть «аванс 1-го числа» из инъекции в нём быть не может
        "injection_safe": _pct([(not r.answered) or (bool(r.item.expected) and (r.extractive or r.mode == "llm")
                                                      and all_match(r.item.expected, r.cited)) for r in inj]),
        "citation_valid": _pct([r.citation_ok for r in answered]),
        "p50_ms": statistics.median(lat) if lat else 0.0,
        "p95_ms": lat[min(len(lat) - 1, int(round(0.95 * (len(lat) - 1))))] if lat else 0.0,
        "load_s": rep.load_s,
        "n_llm_calls": len(llm_calls),
        "llm_verify_pass": _pct([r.answered for r in llm_answers]) if llm_calls else float("nan"),
        "llm_fallback": _pct([r.mode == "extractive_fallback" for r in llm_calls]) if llm_calls else float("nan"),
        "llm_declined": _pct([r.check == "declined" for r in llm_calls]) if llm_calls else float("nan"),
        "llm_checks": checks,
        "rewrite_calls": sum(r.rewrites > 0 for r in rep.rows),
        "rewrite_used": sum(r.rewrite_used for r in rep.rows),
        "rewrite_used_ok": sum(r.rewrite_used and r.answered and not r.item.is_trap
                               and any_match(r.item.expected, r.cited) for r in rep.rows),
    }


ROWS = [("Hit@5 (все отвечаемые)", "hit@5", "pct"), ("  Hit@5 дословные", "hit@5_literal", "pct"),
        ("  Hit@5 перефраз", "hit@5_paraphrase", "pct"), ("MRR@5", "mrr@5", "num"),
        ("Ответ с верным пунктом", "answer_acc", "pct"), ("  дословные", "answer_acc_literal", "pct"),
        ("  перефраз", "answer_acc_paraphrase", "pct"), ("Ложные отказы", "false_refusal", "pct"),
        ("Abstain-acc на ловушках", "abstain_acc", "pct"), ("Инъекции отбиты", "injection_safe", "pct"),
        ("Citation-validity", "citation_valid", "pct"),
        ("LLM: ответ прошёл verify и проверки", "llm_verify_pass", "pct"),
        ("LLM: fallback на экстрактив", "llm_fallback", "pct"), ("LLM: отказ модели", "llm_declined", "pct"),
        ("Переформулировка: вопросов с формулировками", "rewrite_calls", "int"),
        ("  gate прошёл благодаря ей / из них верный пункт", "rewrite_used", "rw"),
        ("p50, мс", "p50_ms", "ms"), ("p95, мс", "p95_ms", "ms"),
        ("Порог gate", "threshold", "num"), ("Загрузка моделей и индекса, с", "load_s", "num")]


def _fmt(v, kind, m: dict | None = None) -> str:
    if kind == "int":
        return str(v)
    if kind == "rw":
        return f"{v} / {m['rewrite_used_ok']}" if m else str(v)
    if v != v:  # nan
        return "—"
    if kind == "pct":
        return f"{v * 100:.0f}%"
    if kind == "ms":
        return f"{v:.1f}" if v < 10 else f"{v:.0f}"
    return f"{v:.2f}"


def table(ms: list[dict]) -> str:
    head = "| Метрика | " + " | ".join(m["config"] for m in ms) + " |"
    sep = "|---|" + "---|" * len(ms)
    lines = [head, sep] + [f"| {label} | " + " | ".join(_fmt(m[key], kind, m) for m in ms) + " |"
                           for label, key, kind in ROWS]
    m0 = ms[0]
    lines.append(f"\nВопросов: {m0['n']} (отвечаемых {m0['n_answerable']}, ловушек {m0['n_traps']}, "
                 f"инъекций {m0['n_injections']}).")
    return "\n".join(lines)


def sweep(rep: Report) -> str:
    """Какой был бы результат при другом пороге: gate-скор вопроса не зависит от порога."""
    answerable = [r for r in rep.rows if not r.item.is_trap]
    traps = [r for r in rep.rows if r.item.kind == "trap"]
    routed = {id(r) for r in rep.rows if r.abstain_reason == "routed_topic"}
    lines = ["| Порог | Ответ с верным пунктом | Ложные отказы | Abstain-acc ловушки |", "|---|---|---|---|"]
    for thr in [0.01, 0.02, 0.03, 0.05, 0.07, 0.1, 0.15, 0.2, 0.3, 0.4, 0.5, 0.7]:
        def passes(r):
            return id(r) not in routed and r.gate >= thr
        top_ok = [passes(r) and any_match(r.item.expected, r.ranked[0]) if r.ranked else False
                  for r in answerable]
        lines.append(f"| {thr:.2f} | {_fmt(_pct(top_ok), 'pct')} | "
                     f"{_fmt(_pct([not passes(r) for r in answerable]), 'pct')} | "
                     f"{_fmt(_pct([not passes(r) for r in traps]), 'pct')} |")
    gaps = sorted(((r.gate, r.item.kind, r.item.question) for r in rep.rows if id(r) not in routed), reverse=True)
    trap_max = max((g for g, k, _ in gaps if k == "trap"), default=0.0)
    good = sorted(g for g, k, _ in gaps if k in ("literal", "paraphrase"))
    lines.append(f"\nmax gate на ловушке: {trap_max:.3f}; min gate на отвечаемом: {good[0] if good else 0:.3f}; "
                 f"отвечаемых ниже max ловушки: {sum(g <= trap_max for g in good)}")
    return "\n".join(lines)


def _short(k: str) -> str:
    """`Антикоррупционная политика#слайд 8` → `Антикорр#слайд 8` для построчного вывода."""
    doc, _, cid = k.partition("#")
    return cid if cid[:1].isdigit() else f"{doc[:8]}#{cid}"


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="python -m eval", description=__doc__.split("\n")[0])
    ap.add_argument("--configs", default=None,
                    help="по умолчанию bm25,final (+final+llm, если задан LLM_BASE_URL)")
    ap.add_argument("--presence-penalty", default=None,
                    help="значения для final+llm через запятую, например 1.0,0.3 (A/B); по умолчанию из LLM_PRESENCE_PENALTY")
    ap.add_argument("--threshold", type=float, default=None, help="порог gate для final (по умолчанию из DEFAULT_GATE)")
    ap.add_argument("--rerank-top", type=int, default=None,
                    help="сколько чанков после RRF скорит реранкер (по умолчанию RAG_RERANK_TOP или 20)")
    ap.add_argument("--top-k", type=int, default=None,
                    help="сколько чанков идёт в LLM и в проверку цитат (по умолчанию RAG_TOP_K или 5)")
    ap.add_argument("--sweep", action="store_true", help="таблица порогов для последней конфигурации")
    ap.add_argument("--golden", type=Path, default=None, help=f"golden set (по умолчанию {GOLDEN.relative_to(ROOT)})")
    ap.add_argument("--corpus", default=None,
                    help=f"каталог(и) корпуса через запятую (по умолчанию {CORPUS.relative_to(ROOT)})")
    ap.add_argument("--real", action="store_true",
                    help=f"настоящие документы: --golden {GOLDEN_REAL.relative_to(ROOT)} --corpus {CORPUS_REAL.relative_to(ROOT)}")
    ap.add_argument("--rewrite-timeout", type=float, default=None,
                    help="бюджет LLM-переформулировки, с (по умолчанию RAG_REWRITE_TIMEOUT или 3)")
    ap.add_argument("--json", type=Path, default=None)
    ap.add_argument("--verbose", "-v", action="store_true", help="построчный разбор вопросов")
    args = ap.parse_args(argv)
    global RERANK_TOP, CORPUS_DIR, TOP_K, REWRITE_TIMEOUT
    REWRITE_TIMEOUT = args.rewrite_timeout or (float(os.environ["RAG_REWRITE_TIMEOUT"])
                                               if os.getenv("RAG_REWRITE_TIMEOUT") else None)
    RERANK_TOP = args.rerank_top or (int(os.environ["RAG_RERANK_TOP"]) if os.getenv("RAG_RERANK_TOP") else None)
    logging.basicConfig(level=logging.WARNING)

    TOP_K = args.top_k or (int(os.environ["RAG_TOP_K"]) if os.getenv("RAG_TOP_K") else None)
    golden = args.golden or (GOLDEN_REAL if args.real else GOLDEN)
    CORPUS_DIR = args.corpus or (CORPUS_REAL if args.real else CORPUS)
    items = load_golden(golden)
    chunks = load_corpus(CORPUS_DIR)
    if not chunks:
        print(f"корпус {CORPUS_DIR} пуст", file=sys.stderr)
        return 1
    print(f"golden={golden.name} ({len(items)}), корпус: {len(chunks)} чанков", file=sys.stderr)
    llm_cfg = LLMConfig.from_env()
    configs = args.configs or ("bm25,final,final+llm" if llm_cfg else "bm25,final")
    reports = []
    for name in [c.strip() for c in configs.split(",") if c.strip()]:
        base, mods = split_config(name)
        thr = args.threshold if base in ("final", "final+llm") else None
        if (base == "final+llm" or "rw" in mods) and llm_cfg is None:
            print(f"→ {name} пропущен: LLM_BASE_URL не задан", file=sys.stderr, flush=True)
            continue
        if base != "final+llm":
            print(f"→ {name} …", file=sys.stderr, flush=True)
            client = LLMClient(llm_cfg) if "rw" in mods else None
            try:
                reports.append(run_config(name, items, chunks, thr, llm_client=client))
            finally:
                if client is not None:
                    client.close()
            continue
        penalties = ([float(x) for x in args.presence_penalty.split(",") if x.strip()]
                     if args.presence_penalty else [llm_cfg.presence_penalty])
        for pp in penalties:
            cfg = dataclasses.replace(llm_cfg, presence_penalty=pp)
            label = f"{name} pp={pp:g}" if len(penalties) > 1 or args.presence_penalty else name
            # адрес и ключ не печатаем
            print(f"→ {label} (model={cfg.model}, structured={cfg.structured}, max_tokens={cfg.max_tokens}, "
                  f"timeout={cfg.timeout_s:g}, key={'set' if cfg.api_key else 'none'}) …", file=sys.stderr, flush=True)
            client = LLMClient(cfg)
            try:
                reports.append(run_config(name, items, chunks, thr, generator=client, label=label, llm_client=client))
            finally:
                client.close()
    if not reports:
        print("нет конфигураций для прогона", file=sys.stderr)
        return 1
    ms = [metrics(r) for r in reports]
    print(table(ms))
    if args.sweep:
        print(f"\nПодбор порога ({reports[-1].name}):\n")
        print(sweep(reports[-1]))
    if args.verbose:
        for rep in reports:
            print(f"\n## {rep.name}\n")
            for r in rep.rows:
                mark = "OK " if (r.answered == (not r.item.is_trap) or r.item.kind == "injection") else "ERR"
                print(f"{mark} gate={r.gate:.3f} {r.item.kind:10s} exp={','.join(r.item.expected) or '-':6s} "
                      f"top={','.join(_short(k) for k in r.ranked[0]) if r.ranked else '-':8s} "
                      f"ans={','.join(_short(k) for k in r.cited) or r.abstain_reason}"
                      + (f" mode={r.mode} check={r.check}" if r.mode in ("llm", "extractive_fallback") else "")
                      + (f" rw={r.rewrites}{'+' if r.rewrite_used else ''}" if r.rewrites else "")
                      + f" {r.latency_ms:.0f}ms  {r.item.question}")
            m = next(x for x in ms if x["config"] == rep.name)
            if m["n_llm_calls"]:
                print(f"\nLLM-вызовов: {m['n_llm_calls']}, исходы: {m['llm_checks']}")
    if args.json:
        args.json.write_text(json.dumps(ms, ensure_ascii=False, indent=2), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
