import type {
  FunnelStep,
  IndicatorPeriodKey,
  IndicatorTile,
  Indicators,
  SiteCellIndicator,
  SiteCode,
  SiteRowKey,
  SiteVerdict,
} from '@/types/api'
import { overviewFixtures } from './overview'

// Synthetic "Indicators" (api_contract.md "Показатели"): per-site raw counts of
// a plausible demo flow, rolled up by the same rules as services/indicators.go
// (thresholds 5 / 20 / 30, verdicts, comparison with the previous period). No
// real people: every employee of the demo flow is "(синтетика)".

const SITES: SiteCode[] = ['GrPP', 'Luzhniki', 'GTP']
const MIN = 5

interface Raw {
  cohort: number
  conn: number
  notConnected: number
  pulseSent: number
  pulseAns: number
  respondents: number
  lowResp: number
  inAdapt: number
  tickets: number
  qAll: number
  qClosed: number
  fr: number[] // first response, working minutes
  slaIn: number
  slaN: number
  ipDecided: number
  ipKeep: number
}

// Site profiles: what differs between the sites in the demo flow.
const PROFILE: Record<
  SiteCode,
  {
    conn: number
    pulse: number
    low: number
    tk: number
    bot: number
    ip: number
    fr: number
    adapt: number
  }
> = {
  GrPP: { conn: 0.86, pulse: 0.84, low: 0.13, tk: 0.86, bot: 0.63, ip: 0.86, fr: 38, adapt: 74 },
  Luzhniki: { conn: 0.71, pulse: 0.7, low: 0.27, tk: 1.32, bot: 0.55, ip: 0.67, fr: 64, adapt: 31 },
  GTP: { conn: 0.93, pulse: 0.89, low: 0.1, tk: 0.79, bot: 0.69, ip: 0.9, fr: 33, adapt: 112 },
}

const COHORT: Record<IndicatorPeriodKey, Record<SiteCode, [number, number]>> = {
  // [current, previous]
  '30d': { GrPP: [22, 19], Luzhniki: [4, 6], GTP: [35, 31] },
  '90d': { GrPP: [64, 52], Luzhniki: [21, 17], GTP: [99, 84] },
  q: { GrPP: [17, 58], Luzhniki: [3, 18], GTP: [28, 91] },
}
const DAYS: Record<IndicatorPeriodKey, number> = { '30d': 30, '90d': 90, q: 24 }
const IP_DUE: Record<IndicatorPeriodKey, Record<SiteCode, [number, number]>> = {
  '30d': { GrPP: [9, 7], Luzhniki: [3, 4], GTP: [14, 12] },
  '90d': { GrPP: [27, 0], Luzhniki: [11, 0], GTP: [41, 0] },
  q: { GrPP: [8, 26], Luzhniki: [2, 10], GTP: [12, 39] },
}

function raw(site: SiteCode, period: IndicatorPeriodKey, prev: boolean): Raw {
  const p = PROFILE[site]
  const k = prev ? 0.95 : 1 // the previous period was a little weaker
  const cohort = COHORT[period][site][prev ? 1 : 0]
  const days = prev && period === 'q' ? 92 : DAYS[period]
  const conn = Math.round(cohort * Math.min(1, p.conn * k))
  const inAdapt = Math.round(p.adapt * (period === '30d' ? 1 : 1.25))
  const pulseSent = Math.round((inAdapt * days) / 7 / 1.8)
  const pulseAns = Math.round(pulseSent * p.pulse * k)
  const respondents = Math.round(inAdapt * 0.82)
  const tickets = Math.round(inAdapt * p.tk * (days / 30) * 0.3)
  const qAll = Math.round(inAdapt * (days / 30) * 0.9)
  const answered = Math.max(0, tickets - 2)
  const fr = Array.from(
    { length: answered },
    (_, i) => p.fr * (0.4 + (i % 7) * 0.2) * (prev ? 1.15 : 1),
  )
  const ipDecided = IP_DUE[period][site][prev ? 1 : 0]
  return {
    cohort,
    conn,
    notConnected: Math.round((cohort - conn) * 0.4),
    pulseSent,
    pulseAns,
    respondents,
    lowResp: Math.round(respondents * p.low),
    inAdapt,
    tickets,
    qAll,
    qClosed: Math.round(qAll * p.bot * k),
    fr,
    slaIn: Math.round(answered * (p.fr < 40 ? 0.91 : 0.74)),
    slaN: answered,
    ipDecided,
    ipKeep: Math.round(ipDecided * p.ip),
  }
}

function sum(rs: Raw[]): Raw {
  const out: Raw = {
    cohort: 0,
    conn: 0,
    notConnected: 0,
    pulseSent: 0,
    pulseAns: 0,
    respondents: 0,
    lowResp: 0,
    inAdapt: 0,
    tickets: 0,
    qAll: 0,
    qClosed: 0,
    fr: [],
    slaIn: 0,
    slaN: 0,
    ipDecided: 0,
    ipKeep: 0,
  }
  for (const r of rs) {
    for (const k of Object.keys(out) as (keyof Raw)[]) {
      if (k === 'fr') out.fr = out.fr.concat(r.fr)
      else (out[k] as number) += r[k] as number
    }
  }
  return out
}

const pct = (num: number, n: number) => (n === 0 ? null : Math.round((num * 100) / n))

function median(xs: number[]): number | null {
  if (xs.length === 0) return null
  const s = xs.slice().sort((a, b) => a - b)
  const m = Math.floor(s.length / 2)
  return Math.round(s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2)
}

function cmp(cur: number | null, prev: number | null, same: number) {
  if (cur === null || prev === null) return { delta: null, direction: null }
  const delta = cur - prev
  const direction = Math.abs(delta) < same ? 'same' : delta > 0 ? 'up' : 'down'
  return { delta, direction: direction as 'same' | 'up' | 'down' }
}

function rateTile(
  key: IndicatorTile['key'],
  num: number,
  n: number,
  pnum: number,
  pn: number,
): IndicatorTile {
  const t: IndicatorTile = {
    key,
    status: 'ok',
    unit: 'pct',
    value: null,
    num,
    n,
    prev: null,
    prev_n: pn,
    delta: null,
    direction: null,
    demo: false,
  }
  if (n < MIN) return { ...t, status: 'few' }
  t.value = pct(num, n)
  if (pn >= MIN) t.prev = pct(pnum, pn)
  return { ...t, ...cmp(t.value, t.prev, 2) }
}

function firstResponse(cur: Raw, prev: Raw): IndicatorTile {
  const t: IndicatorTile = {
    key: 'first_response',
    status: 'ok',
    unit: 'min',
    value: null,
    num: 0,
    n: cur.fr.length,
    prev: null,
    prev_n: prev.fr.length,
    delta: null,
    direction: null,
    demo: false,
    sla_pct: cur.slaN >= MIN ? pct(cur.slaIn, cur.slaN) : null,
    sla_n: cur.slaN,
  }
  if (t.n < MIN) return { ...t, status: 'few' }
  t.value = median(cur.fr)
  if (t.prev_n >= MIN) {
    t.prev = median(prev.fr)
    return { ...t, ...cmp(t.value, t.prev, Math.max(5, Math.round((t.prev ?? 0) * 0.1))) }
  }
  return t
}

function funnel(r: Raw, period: IndicatorPeriodKey): Indicators['funnel'] {
  const n = r.cohort
  const percents = n >= 20
  const hidden = n < MIN
  const p = (x: number) => (percents ? Math.round((x * 100) / n) : null)
  const late = n - r.conn - r.notConnected
  const d0Pending = Math.round(r.conn * (period === '90d' ? 0.01 : 0.04))
  const d0Silent = Math.round(r.conn * 0.04)
  const d0Stopped = r.conn > 20 ? 1 : 0
  const d0 = r.conn - d0Pending - d0Silent - d0Stopped
  const reached30 = period === '90d' ? Math.round(d0 * 0.64) : 0
  const d30Silent = Math.round(reached30 * 0.11)
  const d30Stopped = reached30 > 30 ? 2 : 0
  const d30 = reached30 - d30Silent - d30Stopped
  const step = (
    key: FunnelStep['key'],
    count: number,
    pending: number,
    lost: FunnelStep['lost'],
  ): FunnelStep =>
    hidden && key !== 'invited'
      ? { key, count: null, percent: null, pending: 0, lost: [], hidden: true }
      : {
          key,
          count,
          percent: p(count),
          pending,
          lost: lost.filter((l) => l.count > 0),
          hidden: false,
        }
  return {
    cohort: n,
    percents,
    hidden,
    steps: [
      step('invited', n, 0, []),
      step('connected_before_d0', r.conn, 0, [
        { reason: 'not_connected', count: r.notConnected },
        { reason: 'connected_late', count: late },
      ]),
      step('passed_d0', d0, d0Pending, [
        { reason: 'silent', count: d0Silent },
        { reason: 'stopped', count: d0Stopped },
      ]),
      step('active_d30', d30, d0 - reached30, [
        { reason: 'silent', count: d30Silent },
        { reason: 'stopped', count: d30Stopped },
      ]),
      step('active_d90', 0, d30, []),
      step('ip_decision', 0, 0, []),
    ],
  }
}

function verdict(pp: number, n: number): SiteVerdict {
  if (n < 30 && Math.abs(pp) < 5) return 'noise'
  if (Math.abs(pp) < 1) return 'same'
  return pp > 0 ? 'above' : 'below'
}

function metric(key: SiteRowKey, r: Raw): [number, number] {
  switch (key) {
    case 'connected_before_d0':
      return [r.conn, r.cohort]
    case 'pulse_response':
      return [r.pulseAns, r.pulseSent]
    case 'low_pulse':
      return [r.lowResp, r.respondents]
    case 'tickets':
      return [r.tickets, r.inAdapt]
    case 'ip_passed':
      return [r.ipKeep, r.ipDecided]
  }
  return [r.cohort, r.cohort]
}

function sitesTable(per: Record<SiteCode, Raw>): Indicators['sites'] {
  const all = sum(SITES.map((s) => per[s]))
  const flagged = new Set<SiteCode>()
  const keys: SiteRowKey[] = [
    'cohort',
    'connected_before_d0',
    'pulse_response',
    'low_pulse',
    'tickets',
    'ip_passed',
  ]
  const round = (v: number, unit: string) =>
    unit === 'per10' ? Math.round(v * 10) / 10 : Math.round(v)
  const rows = keys.map((key) => {
    const unit =
      key === 'cohort' ? 'count' : key === 'low_pulse' || key === 'tickets' ? 'per10' : 'pct'
    const scale = unit === 'count' ? 1 : unit === 'per10' ? 10 : 100
    const [anum, an] = metric(key, all)
    const avgShare = key !== 'cohort' && an >= MIN ? anum / an : null
    const avg =
      key === 'cohort' ? all.cohort : avgShare === null ? null : round(avgShare * scale, unit)
    const cells: SiteCellIndicator[] = SITES.map((site) => {
      const r = per[site]
      const [num, n] = metric(key, r)
      if (key === 'cohort') {
        return {
          site,
          value: r.cohort,
          n: r.cohort,
          hidden: false,
          diff: null,
          diff_pp: null,
          verdict: null,
          flag: false,
        }
      }
      if (r.inAdapt < MIN || n < MIN) {
        return {
          site,
          value: null,
          n,
          hidden: true,
          diff: null,
          diff_pp: null,
          verdict: 'hidden',
          flag: false,
        }
      }
      const sh = num / n
      const cell: SiteCellIndicator = {
        site,
        value: round(sh * scale, unit),
        n,
        hidden: false,
        diff: null,
        diff_pp: null,
        verdict: null,
        flag: false,
      }
      if (avgShare !== null) {
        const pp = Math.round((sh - avgShare) * 1000) / 10
        cell.diff_pp = pp
        cell.diff = round((sh - avgShare) * scale, unit)
        cell.verdict = verdict(pp, n)
        cell.flag = Math.abs(pp) >= 10
        if (cell.flag) flagged.add(site)
      }
      return cell
    })
    return { key, unit: unit as 'count' | 'pct' | 'per10', avg, avg_n: an, cells }
  })
  return {
    sites: SITES,
    cohorts: Object.fromEntries(SITES.map((s) => [s, per[s].cohort])),
    rows,
    flagged: SITES.filter((s) => flagged.has(s)),
  }
}

// Topics of the period: [current, previous] people per category (all sites).
const TOPICS: Record<IndicatorPeriodKey, Record<string, [number, number]>> = {
  '30d': {
    F1: [14, 9],
    F2: [11, 12],
    F3: [3, 6],
    F4: [2, 1],
    F5: [1, 0],
    F6: [9, 4],
    F7: [6, 0],
    F8: [7, 7],
    F9: [0, 1],
    F10: [3, 2],
  },
  '90d': {
    F1: [31, 22],
    F2: [27, 30],
    F3: [9, 8],
    F4: [5, 2],
    F5: [2, 1],
    F6: [19, 11],
    F7: [12, 5],
    F8: [16, 15],
    F9: [1, 1],
    F10: [6, 4],
  },
  q: {
    F1: [11, 26],
    F2: [8, 29],
    F3: [2, 9],
    F4: [1, 4],
    F5: [0, 2],
    F6: [7, 15],
    F7: [5, 9],
    F8: [6, 14],
    F9: [0, 1],
    F10: [2, 5],
  },
}

function topics(site: SiteCode | null, period: IndicatorPeriodKey): Indicators['topics'] {
  const base = overviewFixtures[site ?? 'all'].heatmap
  const share = site === null ? 1 : site === 'GTP' ? 0.5 : site === 'GrPP' ? 0.32 : 0.18
  const trends = base.categories.map((c) => {
    const [cur, prev] = TOPICS[period][c.code] ?? [0, 0]
    const cn = Math.round(cur * share)
    const pn = Math.round(prev * share)
    const count = cn >= MIN ? cn : null
    const prevV = pn >= MIN ? pn : null
    let trend: Indicators['topics']['trends'][number]['trend'] = null
    if (count !== null && prevV === null) trend = 'new'
    else if (count === null && prevV !== null) trend = 'down'
    else if (count !== null && prevV !== null)
      trend = Math.abs(count - prevV) <= 1 ? 'same' : count > prevV ? 'up' : 'down'
    return { category: c.code, name: c.name, count, prev: prevV, trend }
  })
  // The captured heat map is of 30 days: scale its visible cells to the period.
  const k = period === '90d' ? 2.4 : period === 'q' ? 0.8 : 1
  const heatmap = { ...structuredClone(base), period_days: DAYS[period] }
  heatmap.respondents = Object.fromEntries(
    Object.entries(heatmap.respondents).map(([s, v]) => [s, Math.round((v ?? 0) * k)]),
  )
  heatmap.cells = heatmap.cells.map((c) => {
    if (c.count === null || c.count === 0) return c
    const count = Math.round(c.count * k)
    if (count < MIN) return { ...c, count: null, share: null, hidden: true }
    const total = heatmap.respondents[c.site] ?? 1
    return { ...c, count, share: Math.round((count / total) * 100) / 100 }
  })
  return { heatmap, trends }
}

function periodDates(period: IndicatorPeriodKey): Indicators['period'] {
  switch (period) {
    case '90d':
      return {
        key: period,
        from: '2026-07-27',
        to: '2026-10-24',
        prev_from: '2026-04-28',
        prev_to: '2026-07-26',
        days: 90,
      }
    case 'q':
      return {
        key: period,
        from: '2026-10-01',
        to: '2026-10-24',
        prev_from: '2026-07-01',
        prev_to: '2026-09-30',
        days: 24,
      }
  }
  return {
    key: '30d',
    from: '2026-09-25',
    to: '2026-10-24',
    prev_from: '2026-08-26',
    prev_to: '2026-09-24',
    days: 30,
  }
}

export function indicatorsFixture(site: SiteCode | null, period: IndicatorPeriodKey): Indicators {
  const cur = Object.fromEntries(SITES.map((s) => [s, raw(s, period, false)])) as Record<
    SiteCode,
    Raw
  >
  const prev = Object.fromEntries(SITES.map((s) => [s, raw(s, period, true)])) as Record<
    SiteCode,
    Raw
  >
  const scope = site ? [site] : SITES
  const c = sum(scope.map((s) => cur[s]))
  const p = sum(scope.map((s) => prev[s]))
  const ip = rateTile('ip_passed', c.ipKeep, c.ipDecided, p.ipKeep, p.ipDecided)
  if (c.ipDecided === 0) ip.status = 'later_90'
  ip.demo = true
  const bot = rateTile('bot_closed', c.qClosed, c.qAll, p.qClosed, p.qAll)
  const fr = firstResponse(c, p)
  const tiles: IndicatorTile[] = [
    rateTile('connected_before_d0', c.conn, c.cohort, p.conn, p.cohort),
    rateTile('pulse_response', c.pulseAns, c.pulseSent, p.pulseAns, p.pulseSent),
    bot,
    fr,
    ip,
    {
      key: 'early_leave',
      status: 'no_marker',
      unit: 'pct',
      value: null,
      num: 0,
      n: c.cohort,
      prev: null,
      prev_n: 0,
      delta: null,
      direction: null,
      demo: true,
    },
  ]
  return {
    site,
    generated_at: new Date().toISOString(),
    period: periodDates(period),
    cohort: c.cohort,
    cohort_prev: p.cohort,
    synthetic: c.cohort,
    min_group: MIN,
    today: { total: 7, urgent: 1, source: 'hrlead' },
    tiles,
    funnel: funnel(c, period),
    sites: sitesTable(cur),
    topics: topics(site, period),
    hr_load: {
      tickets: c.tickets,
      employees: c.inAdapt,
      per_100: c.inAdapt >= MIN ? Math.round((c.tickets * 1000) / c.inAdapt) / 10 : null,
      bot_closed: bot,
      first_response: fr,
      kb_gaps: site === 'Luzhniki' ? 2 : 4,
      kb_questions: site === 'Luzhniki' ? 6 : 17,
    },
  }
}
