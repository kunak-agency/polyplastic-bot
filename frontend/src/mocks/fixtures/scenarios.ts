import type {
  ScenarioDetail,
  ScenarioStep,
  SiteCode,
  StepButton,
  StepKind,
  StepSurvey,
} from '@/types/api'
import { stageForDay } from '@/lib/scenarioSteps'
import { branchesScenario } from './scenarioBranches'

// Synthetic scenarios after the timeline of ux-spec 2.3 (the customer's Excel
// plus the marked changes). Texts are short placeholders, not the customer's copy.

export const SCENARIO_GTP_ID = '6b1f3c2e-8a4d-4f6e-9c1a-0d2b3e4f5a01'
export const SCENARIO_OFFICE_ID = '6b1f3c2e-8a4d-4f6e-9c1a-0d2b3e4f5a02'

const OFFICE: SiteCode[] = ['GrPP', 'Luzhniki']

interface StepSeed {
  day: number
  time: string
  kind: StepKind
  title: string
  ty: string
  vy: string
  buttons?: string[]
  sites?: SiteCode[]
  survey?: StepSurvey
  condition?: string
  workHoursOnly?: boolean
}

function btn(labels: string[] = []): StepButton[] {
  return labels.map((label, i) => ({ label, action: `reply_${i + 1}` }))
}

let seq = 0
function stepId(prefix: string): string {
  seq++
  return `${prefix}-0000-4000-8000-${String(seq).padStart(12, '0')}`
}

function build(prefix: string, seeds: StepSeed[]): ScenarioStep[] {
  return seeds.map((s) => ({
    id: stepId(prefix),
    day_offset: s.day,
    time: s.time,
    stage: stageForDay(s.day),
    sites: s.sites ?? [],
    kind: s.kind,
    title: s.title,
    text_ty: s.ty,
    text_vy: s.vy,
    buttons: btn(s.buttons),
    survey: s.survey ?? null,
    condition: s.condition ?? null,
    work_hours_only: s.workHoursOnly ?? true,
  }))
}

const pulse5: StepSurvey = {
  scale: '1-5',
  threshold: 3,
  questions: [{ text: 'Как прошёл день?', options: [] }],
}

const COMMON_BEFORE: StepSeed[] = [
  {
    day: -3,
    time: '10:00',
    kind: 'text',
    title: 'Знакомство',
    ty: 'Привет, {Имя}! Я Корсик, помогу освоиться. Как удобнее общаться?',
    vy: 'Здравствуйте, {Имя}! Я Корсик, помогу освоиться. Как удобнее общаться?',
    buttons: ['На «ты»', 'На «вы»'],
  },
  {
    day: -3,
    time: '10:01',
    kind: 'text',
    title: 'Согласие на обработку данных',
    ty: 'Чтобы я мог писать тебе, нужно твоё согласие на обработку данных.',
    vy: 'Чтобы я мог писать вам, нужно ваше согласие на обработку данных.',
    buttons: ['Согласен(на)', 'Подробнее', 'Не согласен(на)'],
  },
]

const COMMON_LATE: StepSeed[] = [
  {
    day: 14,
    time: '10:00',
    kind: 'survey',
    title: 'Чек 9 вопросов',
    ty: 'Две недели позади! Ответишь на 9 коротких вопросов?',
    vy: 'Две недели позади! Ответите на 9 коротких вопросов?',
    buttons: ['Пропустить вопрос', 'Напомнить завтра'],
    survey: {
      scale: '1-10',
      threshold: 8,
      questions: [
        { text: 'Насколько понятны ваши задачи?', options: [] },
        { text: 'Насколько комфортно в команде?', options: [] },
        { text: 'Получаете ли вы помощь от наставника?', options: [] },
      ],
    },
  },
  {
    day: 30,
    time: '09:00',
    kind: 'card',
    title: 'Месяц в компании',
    ty: 'Месяц вместе! Как идёт план адаптации?',
    vy: 'Месяц вместе! Как идёт план адаптации?',
    buttons: ['Всё хорошо', 'Немного отстаю', 'Отстаю'],
  },
  {
    day: 45,
    time: '10:00',
    kind: 'text',
    title: 'Встреча с рекрутером',
    ty: 'Скоро встреча с рекрутером — расскажи, как дела.',
    vy: 'Скоро встреча с рекрутером — расскажите, как дела.',
    buttons: ['Понял(а)'],
  },
  {
    day: 52,
    time: '10:00',
    kind: 'survey',
    title: 'Опрос настроения',
    ty: 'Как настроение? Три вопроса.',
    vy: 'Как настроение? Три вопроса.',
    survey: {
      scale: 'choice',
      threshold: null,
      questions: [
        {
          text: 'Как вы себя чувствуете на работе?',
          options: ['Отлично', 'Нормально', 'Тяжело'],
        },
        { text: 'К кому вы идёте с вопросами?', options: ['Наставник', 'Руководитель', 'Никто!'] },
      ],
    },
  },
  {
    day: 60,
    time: '10:00',
    kind: 'card',
    title: 'Финишная прямая',
    ty: 'Осталось чуть-чуть до конца испытательного срока!',
    vy: 'Осталось чуть-чуть до конца испытательного срока!',
    buttons: ['Спасибо!'],
  },
  {
    day: 75,
    time: '10:00',
    kind: 'text',
    title: 'Книги',
    ty: 'Подборка книг для вдохновения.',
    vy: 'Подборка книг для вдохновения.',
  },
  {
    day: 90,
    time: '09:00',
    kind: 'survey',
    title: 'Итоговый опрос',
    ty: 'Испытательный срок позади! Ответишь на итоговый опрос?',
    vy: 'Испытательный срок позади! Ответите на итоговый опрос?',
    survey: {
      scale: '1-10',
      threshold: 8,
      questions: [
        { text: 'Планируете ли вы остаться в компании?', options: [] },
        { text: 'Как вам Корсик?', options: [] },
      ],
    },
  },
  {
    day: 90,
    time: '12:00',
    kind: 'text',
    title: 'Прощание',
    ty: 'Было здорово! Я остаюсь справочником — пиши, если что.',
    vy: 'Было здорово! Я остаюсь справочником — пишите, если что.',
    buttons: ['Пока, Корсик!'],
  },
]

function firstDays(site: 'factory' | 'office'): StepSeed[] {
  const factory = site === 'factory'
  return [
    {
      day: -3,
      time: '10:02',
      kind: 'file',
      title: 'Список документов',
      ty: factory
        ? 'Вот что взять с собой в первый день. Не забудь фото 3×4.'
        : 'Вот что взять с собой в первый день.',
      vy: factory
        ? 'Вот что взять с собой в первый день. Не забудьте фото 3×4.'
        : 'Вот что взять с собой в первый день.',
      buttons: ['Открыть памятку', 'Всё понятно'],
    },
    factory
      ? {
          day: -3,
          time: '10:03',
          kind: 'text',
          title: 'Корпоративный транспорт',
          ty: 'До завода ходит корпоративный автобус. Рассказать?',
          vy: 'До завода ходит корпоративный автобус. Рассказать?',
          buttons: ['Рассказать', 'Не нужно'],
        }
      : {
          day: -3,
          time: '10:03',
          kind: 'text',
          title: 'Как добраться',
          ty: 'Подсказать, как добраться до офиса?',
          vy: 'Подсказать, как добраться до офиса?',
          buttons: ['Рассказать', 'Не нужно'],
        },
    {
      day: -3,
      time: '10:04',
      kind: 'text',
      title: 'Факты о компании',
      ty: 'Три факта о компании, которые стоит знать.',
      vy: 'Три факта о компании, которые стоит знать.',
      buttons: ['Интересно!'],
    },
    {
      day: -3,
      time: '10:05',
      kind: 'text',
      title: 'Напишу за день до выхода',
      ty: 'На сегодня всё. Напишу за день до выхода!',
      vy: 'На сегодня всё. Напишу за день до выхода!',
    },
    {
      day: -1,
      time: '10:00',
      kind: 'text',
      title: 'Завтра первый день',
      ty: 'Завтра первый день! Готов(а)?',
      vy: 'Завтра первый день! Готовы?',
      buttons: ['Жду не дождусь!', 'Готов(а)', 'Немного волнуюсь'],
    },
    {
      day: -1,
      time: '11:00',
      kind: 'file',
      title: 'Напоминание о документах',
      ty: 'Проверь, всё ли собрал(а) из списка.',
      vy: 'Проверьте, всё ли собрали из списка.',
      buttons: ['Собрал(а)', 'Открыть список'],
    },
    {
      day: 0,
      time: '10:00',
      kind: 'card',
      title: 'Поздравление и доступы',
      ty: 'С первым днём, {Имя}! Твои доступы — ниже.',
      vy: 'С первым днём, {Имя}! Ваши доступы — ниже.',
      buttons: ['Спасибо!'],
    },
    {
      day: 0,
      time: '11:00',
      kind: 'text',
      title: factory ? 'Карта территории' : 'Карта этажей',
      ty: factory ? 'Показать карту территории завода?' : 'Показать карту этажей?',
      vy: factory ? 'Показать карту территории завода?' : 'Показать карту этажей?',
      buttons: ['Показать карту', 'Не нужно'],
      sites: factory ? ['GTP'] : ['Luzhniki'],
    },
    {
      day: 0,
      time: '12:00',
      kind: 'text',
      title: 'Проверка доступов',
      ty: 'Всё ли есть: техника, учётка, пропуск?',
      vy: 'Всё ли есть: техника, учётка, пропуск?',
      buttons: ['Да, всё есть', 'Чего-то нет'],
    },
    {
      day: 0,
      time: '12:30',
      kind: 'text',
      title: 'Где пообедать',
      ty: 'Рассказать, где пообедать?',
      vy: 'Рассказать, где пообедать?',
      buttons: ['Да!', 'Нет, спасибо'],
    },
    {
      day: 0,
      time: '13:00',
      kind: 'text',
      title: 'Справочник контактов',
      ty: 'Справочник контактов открывается только из корпоративной сети.',
      vy: 'Справочник контактов открывается только из корпоративной сети.',
      buttons: ['Открыть справочник'],
    },
    {
      day: 0,
      time: '15:00',
      kind: 'file',
      title: 'Презентация о компании',
      ty: 'Короткая презентация о компании.',
      vy: 'Короткая презентация о компании.',
      buttons: ['Открыть'],
    },
    {
      day: 0,
      time: '18:00',
      kind: 'survey',
      title: 'Итог первого дня',
      ty: 'Первый день позади! Как прошёл день?',
      vy: 'Первый день позади! Как прошёл день?',
      survey: pulse5,
    },
    {
      day: 1,
      time: '10:00',
      kind: 'task',
      title: 'Мини-резюме для коллег',
      ty: 'Напишешь пару строк о себе для коллег?',
      vy: 'Напишете пару строк о себе для коллег?',
      buttons: ['Напишу сейчас', 'Позже'],
      condition: 'мини-резюме включено',
    },
    {
      day: 2,
      time: '09:00',
      kind: 'file',
      title: 'План адаптации от руководителя',
      ty: 'Вот твой план адаптации.',
      vy: 'Вот ваш план адаптации.',
      buttons: ['Открыть план', 'Плана ещё нет'],
      condition: 'по отмашке рекрутера',
    },
    ...(factory
      ? [
          {
            day: 2,
            time: '11:00',
            kind: 'text',
            title: 'Спортзал',
            ty: 'На заводе есть спортзал. Рассказать?',
            vy: 'На заводе есть спортзал. Рассказать?',
            buttons: ['Рассказать', 'Не нужно'],
            sites: ['GTP'],
          } satisfies StepSeed,
        ]
      : []),
    {
      day: 2,
      time: '17:30',
      kind: 'survey',
      title: 'Опрос по итогам третьего дня',
      ty: 'Три вопроса о первых днях.',
      vy: 'Три вопроса о первых днях.',
      survey: {
        scale: 'choice',
        threshold: null,
        questions: [
          { text: 'Понятны ли задачи?', options: ['А', 'Б', 'В'] },
          { text: 'Помогает ли наставник?', options: ['А', 'Б', 'В'] },
          { text: 'Всё ли с рабочим местом?', options: ['А', 'Б', 'В'] },
        ],
      },
    },
    {
      day: 5,
      time: '10:00',
      kind: 'survey',
      title: 'Пульс первой недели',
      ty: 'Неделя позади. Три быстрых вопроса.',
      vy: 'Неделя позади. Три быстрых вопроса.',
      survey: {
        scale: '1-5',
        threshold: 3,
        questions: [
          { text: 'Есть ли всё для работы?', options: [] },
          { text: 'Понятно ли, что делать?', options: [] },
          { text: 'Понятно ли, к кому идти?', options: [] },
        ],
      },
    },
    ...(factory
      ? []
      : [
          {
            day: 10,
            time: '10:00',
            kind: 'card',
            title: 'Корпоративные мероприятия',
            ty: 'Скоро корпоративные мероприятия — присоединяйся!',
            vy: 'Скоро корпоративные мероприятия — присоединяйтесь!',
            buttons: ['Здорово!'],
            sites: OFFICE,
          } satisfies StepSeed,
        ]),
  ]
}

function seeds(site: 'factory' | 'office'): StepSeed[] {
  return [...COMMON_BEFORE, ...firstDays(site), ...COMMON_LATE]
}

export function scenarioFixtures(nowIso: string): ScenarioDetail[] {
  return [
    branchesScenario(nowIso),
    {
      id: SCENARIO_GTP_ID,
      name: 'Завод GTP',
      sites: ['GTP'],
      version: 12,
      status: 'published',
      active_newcomers: 0,
      updated_at: nowIso,
      steps: build('7c2e0a11', seeds('factory')),
    },
    {
      id: SCENARIO_OFFICE_ID,
      name: 'Офис GrPP',
      sites: OFFICE,
      version: 4,
      status: 'published',
      active_newcomers: 0,
      updated_at: nowIso,
      steps: build('7c2e0a12', seeds('office')),
    },
  ]
}

// What an import of the customer's Excel yields until the real file and the
// parser exist: a short draft, so the "import → editor" path can be shown.
export function importedScenario(id: string, fileName: string, nowIso: string): ScenarioDetail {
  // The server names the draft after the file (or the form's `name` field).
  const base = fileName.replace(/\.(xlsx|xls)$/i, '')
  return {
    id,
    name: base,
    sites: [],
    version: 0,
    status: 'draft',
    active_newcomers: 0,
    updated_at: nowIso,
    steps: build(id.slice(0, 8), [...COMMON_BEFORE, ...firstDays('office').slice(0, 3)]),
  }
}
