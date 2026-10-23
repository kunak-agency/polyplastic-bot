import type { Digest, ManagerSummary, ManagerView } from '@/types/api'

// Synthetic digest of one week (no real people): numbers mirror a smoke run on
// the 150-persona demo base.
export const digestFixture: Digest = {
  week: '2026-W43',
  from: '2026-10-19',
  to: '2026-10-25',
  site: null,
  generated_at: '2026-10-24T10:00:00+03:00',
  partial: true,
  min_group: 5,
  totals: {
    on_pulse: 67,
    answered: 55,
    response_rate_pct: 82,
    w_avg: 81,
    pulse_avg: 7,
    respondents: 53,
    problems: 40,
    critical: 5,
  },
  previous: {
    on_pulse: 54,
    answered: 51,
    response_rate_pct: 94,
    w_avg: 84,
    pulse_avg: 7.2,
    respondents: 49,
    problems: 42,
    critical: 9,
  },
  themes: [
    {
      key: 'F6',
      label: 'перегрузка',
      category: 'F6',
      newcomers: 13,
      mentions: 16,
      sites: ['GrPP', 'Luzhniki', 'GTP'],
      owner: 'руководитель подразделения',
      keywords: [],
      examples: [],
      prev_count: 7,
      prev_hidden: false,
      delta: 6,
    },
    {
      key: 'F2',
      label: 'проблемы с доступами, техникой, СИЗ',
      category: 'F2',
      newcomers: 11,
      mentions: 18,
      sites: ['GrPP', 'Luzhniki', 'GTP'],
      owner: 'HR',
      keywords: ['пропуск', 'проходной'],
      examples: ['пропуск не работает на проходной', 'третий день нет доступа в 1С'],
      prev_count: 7,
      prev_hidden: false,
      delta: 4,
    },
  ],
  hidden_themes: 6,
  confidential: { present: true, count: null },
  risk_zones: { red: 1, yellow: 0, items: [] },
  sites: [
    { site: 'GrPP', on_pulse: 24, answered: 19, w_avg: 82, problems: 11, top_theme: null },
    {
      site: 'Luzhniki',
      on_pulse: 15,
      answered: 11,
      w_avg: 91,
      problems: 7,
      top_theme: 'перегрузка',
    },
    { site: 'GTP', on_pulse: 28, answered: 25, w_avg: 75, problems: 22, top_theme: 'перегрузка' },
  ],
  probation: { count: 1, this_week: false },
  text: {
    summary:
      'На связи 67 новичков, ответили 55 (82 %). Средний индекс W — 81, снизился на 3 пункта. Проблем — 40, из них критичных — 5.',
    themes: null,
    changes: null,
    source: 'template',
    rejected: [],
  },
  max_message: {
    text: 'Пульс адаптации · 19–25 окт · все площадки\nНа пульсе 67 · ответили 55 (82 %) · W 81 (−3)',
    length: 80,
    link: 'http://localhost:5173/analytics?week=2026-W43',
  },
}

export const managersFixture: ManagerSummary[] = [
  { manager_name: 'Руководитель Цех экструзии (синтетика)', newcomers: 3, connected: true },
  { manager_name: 'Руководитель ОТК (синтетика)', newcomers: 2, connected: false },
]

export const managerViewFixture: ManagerView = {
  manager_name: 'Руководитель Цех экструзии (синтетика)',
  connected: true,
  total: 1,
  items: [
    {
      newcomer_id: 'mock-n1',
      full_name: 'Миронова Ульяна (синтетика)',
      site: 'GTP',
      department: 'Цех экструзии',
      position: 'Оператор линии экструзии',
      start_date: '2026-09-21',
      day: 33,
      stage: 'probation',
      connection: 'connected',
      last_pulse: 4,
      probation_date: '2026-12-20',
      attention: ['второй низкий пульс подряд'],
      meetings: [
        {
          id: 'mock-m1',
          kind: 'low_pulse_twice',
          status: 'open',
          text: 'Миронова Ульяна (синтетика) (D+33): второй низкий пульс подряд. Рекомендую встретиться 1:1 на этой неделе.',
          created_at: '2026-10-23T10:00:00+03:00',
        },
      ],
    },
  ],
}
