import type { Branch, ScenarioDetail, ScenarioStep, StepStats } from '@/types/api'
import { stageForDay } from '@/lib/scenarioSteps'

// Synthetic scenario of contract v4: blocks of several messages, buttons with
// branches and actions, a per-question survey with "multi" and "other".
// Texts are short placeholders, not the customer's copy.

export const SCENARIO_BRANCHES_ID = '6b1f3c2e-8a4d-4f6e-9c1a-0d2b3e4f5a03'

let seq = 0
function sid(): string {
  seq++
  return `7c2e0a13-0000-4000-8000-${String(seq).padStart(12, '0')}`
}

function step(p: Partial<ScenarioStep> & Pick<ScenarioStep, 'day_offset' | 'time' | 'title'>) {
  const s: ScenarioStep = {
    id: sid(),
    stage: stageForDay(p.day_offset),
    sites: [],
    kind: 'text',
    text_ty: '',
    text_vy: '',
    buttons: [],
    survey: null,
    condition: null,
    work_hours_only: true,
    key: `k${seq}`,
    required: false,
    media: [],
    gallery: false,
    alt_text: null,
    hr_task: false,
    flag_required: null,
    no_answer: null,
    advance: null,
    ...p,
  }
  return s
}

function say(vy: string, ty = vy): Pick<ScenarioStep, 'text_ty' | 'text_vy'> {
  return { text_ty: ty, text_vy: vy }
}

const toCorsica: Branch = {
  messages: [
    {
      text_ty: 'Напиши, чего не хватает, — передам HR.',
      text_vy: 'Напишите, чего не хватает, — передам HR.',
      media: [],
      buttons: [],
    },
  ],
  actions: [
    { type: 'open_free_text', prompt: 'Чего не хватает?' },
    { type: 'escalate', category: 'F2', route: 'hr', notify_manager: true },
  ],
  then: 'continue',
}

export function branchesScenario(nowIso: string): ScenarioDetail {
  seq = 0
  const steps: ScenarioStep[] = [
    step({
      day_offset: -3,
      time: '10:00',
      title: 'Приветствие',
      ...say('Здравствуйте, {Имя}! Я Корсик.', 'Привет, {Имя}! Я Корсик.'),
      alt_text: 'Добрый день, {Имя}! На связи Корсик.',
      required: true,
    }),
    step({
      day_offset: -3,
      time: '10:00',
      title: 'Памятка',
      ...say('Вот памятка к первому дню.', 'Вот памятка к первому дню.'),
      media: [{ name: 'Памятка новичка', media_id: 'a1b2c3d4-0000-4000-8000-000000000001' }],
    }),
    step({
      day_offset: -3,
      time: '10:00',
      title: 'Настрой',
      ...say('Готовы к первому дню?', 'Готов к первому дню?'),
      buttons: [
        {
          label: 'Да, жду!',
          action: '',
          branch: {
            messages: [{ text_ty: 'Отлично!', text_vy: 'Отлично!', media: [], buttons: [] }],
            actions: [],
            then: 'continue',
            on: [1],
          },
        },
        { label: 'Скорее да', action: '' },
        {
          label: 'Волнуюсь',
          action: '',
          branch: {
            messages: [
              {
                text_ty: 'Это нормально — первый день спокойный.',
                text_vy: 'Это нормально — первый день спокойный.',
                media: [],
                buttons: [],
              },
            ],
            actions: [{ type: 'set_flag', flag: 'anxious' }],
            then: 'continue',
          },
        },
      ],
      no_answer: { remind_after_h: [24, 48], max: 2 },
    }),
    step({
      day_offset: -1,
      time: '12:00',
      title: 'Пропуск: офис',
      sites: ['GrPP'],
      ...say('Пропуск ждёт на ресепшене.'),
    }),
    step({
      day_offset: -1,
      time: '12:00',
      title: 'Пропуск: Лужники',
      sites: ['Luzhniki'],
      ...say('Пропуск выдают на КПП-2.'),
    }),
    step({
      day_offset: -1,
      time: '15:00',
      title: 'Письмо HR о доступах',
      ...say('Заказать доступы для новичка.'),
      hr_task: true,
    }),
    step({
      day_offset: 0,
      time: '12:00',
      title: 'Проверка доступов',
      ...say('Всё ли есть для работы: пропуск, почта, рабочее место?', 'Всё есть для работы?'),
      required: true,
      buttons: [
        { label: 'Всё есть', action: '' },
        { label: 'Чего-то нет', action: '', branch: toCorsica },
      ],
      no_answer: { remind_after_h: [24, 48], max: 2 },
    }),
    step({
      day_offset: 0,
      time: '12:00',
      title: 'Обед',
      ...say('Где пообедать? Выберите формат.', 'Где пообедать?'),
      buttons: [
        {
          label: 'Столовая',
          action: '',
          branch: {
            messages: [
              {
                text_ty: 'Вот список по площадке.',
                text_vy: 'Вот список по площадке.',
                media: [],
                buttons: [],
              },
            ],
            actions: [{ type: 'send_file', media: 'Заведения рядом', per_tag: true }],
            then: 'continue',
          },
        },
        { label: 'Возьму с собой', action: '' },
      ],
    }),
    step({
      day_offset: 3,
      time: '17:00',
      title: 'Пульс 3-го дня',
      ...say('Как прошли первые дни?'),
      buttons: [
        { label: 'Хорошо', action: '' },
        { label: 'Есть вопросы', action: '', opens_free_text: true },
        {
          label: 'Передать через Корсика',
          action: '',
          branch: {
            messages: [],
            actions: [{ type: 'handoff', to: '' }],
            then: 'continue',
          },
        },
      ],
    }),
    step({
      day_offset: 7,
      time: '10:00',
      title: 'Мини-резюме',
      ...say('Пришлите мини-резюме и фото для знакомства с командой.'),
      flag_required: 'recruiter_enabled',
      media: [{ name: 'Изображение 14' }],
    }),
    step({
      day_offset: 8,
      time: '10:00',
      title: 'Адаптационный план',
      ...say('Вот ваш адаптационный план.', 'Вот твой адаптационный план.'),
      flag_required: 'recruiter_enabled',
      media: [{ name: 'Адаптационный план (личный)', personal: 'adaptation_plan' }],
    }),
    step({
      day_offset: 9,
      time: '11:00',
      title: 'Расскажите о себе',
      kind: 'input',
      ...say('Пришлите пару строк о себе и фото.', 'Пришли пару строк о себе и фото.'),
    }),
    step({
      day_offset: 14,
      time: '10:00',
      title: 'Опрос двух недель',
      kind: 'survey',
      ...say('Две недели позади — пара вопросов.'),
      survey: {
        scale: '1-10',
        threshold: 8,
        questions: [
          {
            text: 'Насколько понятны задачи?',
            options: [],
            type: 'scale',
            scale: '1-10',
            threshold: 8,
            comment: 'below',
          },
          {
            text: 'Познакомили с наставником?',
            options: [],
            type: 'yesno',
            comment: 'on_no',
            required: true,
            branches: {
              '0': {
                messages: [
                  {
                    text_ty: 'Жаль. Передать HR или свяжешься сам?',
                    text_vy: 'Жаль. Передать HR или свяжетесь сами?',
                    media: [],
                    buttons: [
                      { label: 'Передать HR', action: '' },
                      { label: 'Свяжусь сам(а)', action: '' },
                    ],
                  },
                ],
                actions: [{ type: 'escalate', category: 'F4', route: 'hr', notify_manager: false }],
                then: 'continue',
              },
            },
            option_flags: { '0': { code: 'F4', severity: 'medium' } },
          },
          {
            text: 'Кто помогает больше всего?',
            options: ['Наставник', 'Руководитель', 'Коллеги', 'Никто!'],
            type: 'multi',
            other: true,
            option_flags: { '4': { code: 'F3', severity: 'medium' } },
          },
          {
            text: 'Как настроение?',
            options: ['Отлично', 'Безразлично', 'Устал(а), задачи валятся'],
            type: 'choice',
            branches: {
              '3': {
                messages: [
                  {
                    text_ty: 'Понимаю. Давай разберёмся с нагрузкой.',
                    text_vy: 'Понимаю. Давайте разберёмся с нагрузкой.',
                    media: [],
                    buttons: [],
                  },
                ],
                actions: [{ type: 'handoff', to: 'recruiter' }],
                then: 'continue',
              },
            },
            option_flags: {
              '2': { code: 'F6', severity: 'low' },
              '3': { code: 'F6', severity: 'medium' },
            },
          },
          { text: 'Что улучшить?', options: [], type: 'text', comment: 'optional' },
        ],
        outcomes: [
          {
            if: [{ q: 4, in: [2] }],
            group: 'A',
            messages: [
              {
                text_ty: 'Расскажи рекрутеру, что поменять.',
                text_vy: 'Расскажите рекрутеру, что поменять.',
                media: [],
                buttons: [],
              },
            ],
            actions: [{ type: 'handoff', to: 'recruiter' }],
          },
          {
            if: [{ q: 4, in: [3] }],
            group: 'A',
            messages: [
              {
                text_ty: 'Передадим, что задач много.',
                text_vy: 'Передадим, что задач много.',
                media: [],
                buttons: [],
              },
            ],
            actions: [
              { type: 'escalate', category: 'F6', route: 'recruiter', notify_manager: true },
            ],
          },
          {
            if: [{ q: 3, contains: [4] }],
            group: 'B',
            messages: [
              {
                text_ty: 'Найдём, кто поможет.',
                text_vy: 'Найдём, кто поможет.',
                media: [],
                buttons: [],
              },
            ],
            actions: [{ type: 'handoff', to: 'recruiter' }],
          },
        ],
      },
    }),
    step({
      day_offset: 30,
      time: '10:00',
      title: 'Месяц: итоги',
      ...say('Месяц вместе!'),
      advance: 'auto',
    }),
    step({
      day_offset: 30,
      time: '10:00',
      title: 'Месяц: план',
      ...say('Напомним план на второй месяц.'),
      advance: 'auto',
    }),
    step({
      day_offset: 30,
      time: '10:00',
      title: 'Месяц: контакты',
      ...say('Контакты HR — в меню /HR.'),
      advance: 'auto',
    }),
    step({
      day_offset: 30,
      time: '10:00',
      title: 'Месяц: вопрос',
      ...say('Всё ли понятно?'),
      buttons: [
        { label: 'Да', action: '' },
        {
          label: 'Нет',
          action: '',
          branch: {
            messages: [
              { text_ty: 'Запишем вопрос.', text_vy: 'Запишем вопрос.', media: [], buttons: [] },
            ],
            actions: [{ type: 'handoff', to: 'hr' }],
            then: 'end_block',
          },
        },
      ],
    }),
  ]
  return {
    id: SCENARIO_BRANCHES_ID,
    name: 'Ветки v4 (синтетика)',
    commands: [
      {
        name: '/food',
        text_ty: 'Вот где можно пообедать рядом.',
        text_vy: 'Вот где можно пообедать рядом.',
        actions: [{ type: 'send_file', media: 'Заведения рядом', per_tag: true }],
      },
      {
        name: '/hr',
        text_ty: 'Передаю HR — ответят в рабочее время.',
        text_vy: 'Передаю HR — ответят в рабочее время.',
        actions: [{ type: 'handoff', to: 'hr' }],
      },
    ],
    rules: [
      {
        key: 'free_question',
        kind: 'free_question',
        trigger: 'вопрос, на который нет ответа',
        text_ty: 'Пока не знаю ответа. Опиши проблему или напиши рекрутеру.',
        text_vy: 'Пока не знаю ответа. Опишите проблему или напишите рекрутеру.',
        media: [],
        buttons: [
          {
            label: 'Описать проблему',
            action: '',
            branch: {
              messages: [],
              actions: [{ type: 'open_free_text', prompt: 'Опишите проблему' }],
              then: 'continue',
            },
          },
          {
            label: 'Чат с рекрутером',
            action: '',
            branch: {
              messages: [],
              actions: [{ type: 'handoff', to: 'recruiter' }],
              then: 'continue',
            },
          },
        ],
      },
      {
        key: 'survey_reminder',
        kind: 'survey_reminder',
        trigger: 'опрос не пройден',
        text_ty: 'Напоминаю про короткий опрос — это пара минут.',
        text_vy: 'Напоминаю про короткий опрос — это пара минут.',
        media: [{ name: 'Напоминание об опросе' }],
        buttons: [],
        remind_after_h: [24, 48],
      },
    ],
    sites: ['GrPP', 'Luzhniki'],
    version: 2,
    status: 'draft',
    active_newcomers: 0,
    updated_at: nowIso,
    steps,
  }
}

// Synthetic statistics: deterministic per step, so the badges do not jump
// between reloads. "Проверка доступов" carries the case's numbers.
export function syntheticStats(steps: ScenarioStep[], minAnswers: number): StepStats[] {
  return steps.map((s, i) => {
    const sent = s.hr_task ? 0 : 14 + ((i * 7) % 30)
    const answered = s.buttons.length === 0 ? 0 : Math.round(sent * 0.85)
    let picked = s.buttons.map((_, j) => Math.floor(answered / (j + 2)))
    if (s.title === 'Проверка доступов') picked = [answered - 12, 12]
    const rest = answered - picked.reduce((a, b) => a + b, 0)
    if (picked.length > 0) picked[0] += Math.max(0, rest)
    return {
      step_id: s.id,
      sent,
      answered,
      no_answer: sent - answered,
      median_answer_s: answered > 0 ? 600 + i * 120 : null,
      buttons: s.buttons.map((b, j) => ({
        label: b.label,
        picked: picked[j],
        share: answered >= minAnswers ? picked[j] / answered : null,
        tickets: b.branch?.actions.some((a) => a.type === 'escalate' || a.type === 'handoff')
          ? Math.ceil(picked[j] * 0.75)
          : 0,
      })),
    }
  })
}
