import type {
  TicketCategory,
  TicketMessageFrom,
  TicketPriority,
  TicketRoute,
  TicketStatus,
} from '@/types/api'

// Synthetic tickets only (api_contract.md v2): people from fixtures/newcomers.ts,
// texts invented. Times are minutes relative to "now", so the SLA timers stay
// meaningful whatever the date: a negative sla_in_min is already overdue.
// F5/F9 carry no quote at all — the mock never holds what the list must hide.

export interface TicketMessageSeed {
  from: TicketMessageFrom
  author: string | null
  text: string
  min_ago: number
  internal?: boolean
}

export interface TicketSeed {
  id: string
  number: number
  newcomer_id: string
  category: TicketCategory
  priority: TicketPriority
  status: TicketStatus
  route: TicketRoute
  assignee_name: string | null
  anonymous: boolean
  summary: string
  quote: string | null
  created_min_ago: number
  updated_min_ago: number
  // null — no deadline (resolved, or the server set none).
  sla_in_min: number | null
  notify_manager: boolean
  messages: TicketMessageSeed[]
}

const N = (n: number) => `3f0c1a7e-5b2d-4c8e-a1f0-${String(n).padStart(12, '0')}`
const T = (n: number) => `7a2e5d10-4c3b-4e8f-b2a1-${String(n).padStart(12, '0')}`

const HOUR = 60
const DAY = 24 * HOUR

export const ticketSeeds: TicketSeed[] = [
  {
    id: T(1),
    number: 1042,
    newcomer_id: N(5),
    category: 'F2',
    priority: 'P1',
    status: 'open',
    route: 'hr',
    assignee_name: null,
    anonymous: false,
    summary:
      'Новичок на D0 ответил «Чего-то нет» → «Учётка» в шаге «Проверка доступов». Пароля от 1С нет, мастер не знает, кому писать.',
    quote: 'Пароль от 1С не дали, мастер не знает кому писать',
    created_min_ago: 48,
    updated_min_ago: 48,
    sla_in_min: 3 * HOUR + 12,
    notify_manager: true,
    messages: [
      {
        from: 'bot',
        author: null,
        text: 'Проверим доступы: у вас есть пропуск, учётка и рабочее место?',
        min_ago: 52,
      },
      { from: 'newcomer', author: null, text: '[Чего-то нет] → [Учётка]', min_ago: 50 },
      {
        from: 'newcomer',
        author: null,
        text: 'Пароль от 1С не дали, мастер не знает кому писать',
        min_ago: 49,
      },
      {
        from: 'bot',
        author: null,
        text: 'Передал ваш вопрос HR. Переписку они видят, повторять не нужно. Обращение #1042.',
        min_ago: 48,
      },
    ],
  },
  {
    id: T(2),
    number: 1043,
    newcomer_id: N(1),
    category: 'F2',
    priority: 'P1',
    status: 'assigned',
    route: 'hr',
    assignee_name: 'Анна К.',
    anonymous: false,
    summary:
      'Пульс 2/5 второй раз подряд. Причина та же, что на D+5: спецодежду по размеру так и не выдали.',
    quote: 'Спецовку так и не выдали по размеру, хожу в своей',
    created_min_ago: 5 * HOUR,
    updated_min_ago: 4 * HOUR,
    sla_in_min: -(1 * HOUR + 5),
    notify_manager: true,
    messages: [
      {
        from: 'bot',
        author: null,
        text: 'Как прошла неделя? Оцените от 1 до 5.',
        min_ago: 5 * HOUR + 10,
      },
      { from: 'newcomer', author: null, text: '2', min_ago: 5 * HOUR + 8 },
      {
        from: 'newcomer',
        author: null,
        text: 'Спецовку так и не выдали по размеру, хожу в своей',
        min_ago: 5 * HOUR + 6,
      },
      {
        from: 'bot',
        author: null,
        text: 'Понял, без СИЗ по размеру работать неудобно и небезопасно. Передал HR, обращение #1043.',
        min_ago: 5 * HOUR,
      },
      {
        from: 'staff',
        author: 'Анна К.',
        text: 'Звонила на склад СИЗ: размер 52 ждут в четверг. Уточнить у Смирнова А.',
        min_ago: 4 * HOUR,
        internal: true,
      },
    ],
  },
  {
    id: T(3),
    number: 1044,
    newcomer_id: N(4),
    category: 'question',
    priority: 'P2',
    status: 'assigned',
    route: 'hr',
    assignee_name: 'Мария Л.',
    anonymous: false,
    summary:
      'В базе знаний нет ответа, как оформить пропуск на парковку. Новичок нажал «Спросить человека».',
    quote: 'Где оформить пропуск на машину? На проходной сказали — в HR',
    created_min_ago: 1 * DAY + 2 * HOUR,
    updated_min_ago: 1 * DAY,
    sla_in_min: 2 * DAY - 3 * HOUR,
    notify_manager: false,
    messages: [
      {
        from: 'newcomer',
        author: null,
        text: 'Где оформить пропуск на машину? На проходной сказали — в HR',
        min_ago: 1 * DAY + 2 * HOUR + 3,
      },
      {
        from: 'bot',
        author: null,
        text: 'В регламентах этого нет, не хочу гадать. Передал HR, обращение #1044.',
        min_ago: 1 * DAY + 2 * HOUR,
      },
    ],
  },
  {
    id: T(4),
    number: 1045,
    newcomer_id: N(7),
    category: 'F5',
    priority: 'P0',
    status: 'open',
    route: 'security',
    assignee_name: null,
    anonymous: false,
    summary:
      'Сработал флаг «Грубость, унижение» в свободном тексте. Подробности видят только HR-партнёр и СБ.',
    quote: null,
    created_min_ago: 1 * HOUR + 30,
    updated_min_ago: 1 * HOUR + 30,
    sla_in_min: -30,
    notify_manager: false,
    messages: [
      {
        from: 'bot',
        author: null,
        text: 'Передаю HR-партнёру. Эту переписку видит только он(а).',
        min_ago: 1 * HOUR + 30,
      },
    ],
  },
  {
    id: T(5),
    number: 1046,
    newcomer_id: N(9),
    category: 'F9',
    priority: 'P0',
    status: 'assigned',
    route: 'security',
    assignee_name: 'Дежурный СБ',
    anonymous: true,
    summary: 'Сработал regex-триггер категории «Этика и нарушения». Подробности — у СБ.',
    quote: null,
    created_min_ago: 20,
    updated_min_ago: 12,
    sla_in_min: 40,
    notify_manager: false,
    messages: [
      {
        from: 'bot',
        author: null,
        text: 'Передаю в службу безопасности. Эту переписку видит только она.',
        min_ago: 20,
      },
    ],
  },
  {
    id: T(6),
    number: 1047,
    newcomer_id: N(2),
    category: 'F7',
    priority: 'P1',
    status: 'human_active',
    route: 'recruiter',
    assignee_name: 'Анна К.',
    anonymous: false,
    summary:
      'Новичок пишет, что задумывается об уходе: задачи не совпадают с тем, что обсуждали на собеседовании. Попросил передать рекрутеру.',
    quote: 'Честно, думаю уходить. Делаю не то, о чём договаривались',
    created_min_ago: 1 * DAY + 5 * HOUR,
    updated_min_ago: 3 * HOUR,
    sla_in_min: 2 * HOUR + 40,
    notify_manager: true,
    messages: [
      {
        from: 'newcomer',
        author: null,
        text: 'Честно, думаю уходить. Делаю не то, о чём договаривались',
        min_ago: 1 * DAY + 5 * HOUR + 4,
      },
      {
        from: 'bot',
        author: null,
        text: 'Слышу вас. Спасибо, что сказали прямо. Могу передать рекрутеру, чтобы с вами связались. Как поступим?',
        min_ago: 1 * DAY + 5 * HOUR + 3,
      },
      { from: 'newcomer', author: null, text: '[Передать рекрутеру]', min_ago: 1 * DAY + 5 * HOUR },
      {
        from: 'staff',
        author: 'Анна К.',
        text: 'Здравствуйте, Анна! Это Анна из HR. Давайте созвонимся завтра в 11:00 и разберём задачи вместе с руководителем?',
        min_ago: 3 * HOUR + 10,
      },
      { from: 'newcomer', author: null, text: 'Да, давайте', min_ago: 3 * HOUR },
    ],
  },
  {
    id: T(7),
    number: 1048,
    newcomer_id: N(9),
    category: 'F4',
    priority: 'P2',
    status: 'assigned',
    route: 'hr',
    assignee_name: 'Мария Л.',
    anonymous: true,
    summary: 'Наставник неделю не отвечает на вопросы. Новичок просил передать HR без его имени.',
    quote: 'Наставник игнорит, спрашиваю у ребят со смены',
    created_min_ago: 4 * DAY,
    updated_min_ago: 3 * DAY,
    sla_in_min: -3 * HOUR,
    notify_manager: false,
    messages: [
      {
        from: 'newcomer',
        author: null,
        text: 'Наставник игнорит, спрашиваю у ребят со смены',
        min_ago: 4 * DAY + 5,
      },
      {
        from: 'newcomer',
        author: null,
        text: '[Передать HR без моего имени]',
        min_ago: 4 * DAY + 1,
      },
      {
        from: 'bot',
        author: null,
        text: 'Передал HR без вашего имени. Обращение #1048.',
        min_ago: 4 * DAY,
      },
    ],
  },
  {
    id: T(8),
    number: 1049,
    newcomer_id: N(8),
    category: 'F3',
    priority: 'P1',
    status: 'human_active',
    route: 'hr',
    assignee_name: 'Мария Л.',
    anonymous: false,
    summary:
      'Руководитель второй раз переносит встречу 1:1, новичок не понимает, к кому идти с вопросами по клиентам.',
    quote: 'Руководителя не видела с первого дня, встречу опять перенесли',
    created_min_ago: 6 * HOUR,
    updated_min_ago: 2 * HOUR,
    sla_in_min: 5 * HOUR,
    notify_manager: false,
    messages: [
      {
        from: 'newcomer',
        author: null,
        text: 'Руководителя не видела с первого дня, встречу опять перенесли',
        min_ago: 6 * HOUR + 2,
      },
      {
        from: 'bot',
        author: null,
        text: 'Понял: без встречи с руководителем непонятно, куда двигаться. Передал HR, обращение #1049.',
        min_ago: 6 * HOUR,
      },
      {
        from: 'staff',
        author: 'Мария Л.',
        text: 'Ксения, добрый день! Это Мария из HR. Пока встреча не состоялась, по клиентам можно идти к Соколову К. — он в курсе.',
        min_ago: 2 * HOUR,
      },
    ],
  },
  {
    id: T(9),
    number: 1050,
    newcomer_id: N(10),
    category: 'F10',
    priority: 'P2',
    status: 'open',
    route: 'hr',
    assignee_name: null,
    anonymous: false,
    summary: 'Новичок спрашивает, как отпроситься на полдня по личным обстоятельствам.',
    quote: 'Нужно в пятницу уйти после обеда по семейным делам, как это оформить?',
    created_min_ago: 3 * HOUR,
    updated_min_ago: 3 * HOUR,
    sla_in_min: 2 * DAY + 4 * HOUR,
    notify_manager: false,
    messages: [
      {
        from: 'newcomer',
        author: null,
        text: 'Нужно в пятницу уйти после обеда по семейным делам, как это оформить?',
        min_ago: 3 * HOUR + 2,
      },
      {
        from: 'bot',
        author: null,
        text: 'Это лучше уточнить у HR. Передал, обращение #1050.',
        min_ago: 3 * HOUR,
      },
    ],
  },
  {
    id: T(10),
    number: 1038,
    newcomer_id: N(1),
    category: 'F1',
    priority: 'P2',
    status: 'resolved',
    route: 'hr',
    assignee_name: 'Анна К.',
    anonymous: false,
    summary: 'На D+2 новичок не понимал, какие задачи у него на первой неделе.',
    quote: 'Не понимаю, что от меня хотят на этой неделе',
    created_min_ago: 10 * DAY,
    updated_min_ago: 9 * DAY,
    sla_in_min: null,
    notify_manager: true,
    messages: [
      {
        from: 'newcomer',
        author: null,
        text: 'Не понимаю, что от меня хотят на этой неделе',
        min_ago: 10 * DAY + 3,
      },
      { from: 'bot', author: null, text: 'Передал HR, обращение #1038.', min_ago: 10 * DAY },
      {
        from: 'staff',
        author: 'Анна К.',
        text: 'Пётр, добрый день! Договорилась со Смирновым А.: план первой недели пришлёт сегодня до 15:00.',
        min_ago: 9 * DAY + 2 * HOUR,
      },
      {
        from: 'bot',
        author: null,
        text: '✅ Вопрос закрыт. Если что — спрашивайте.',
        min_ago: 9 * DAY,
      },
    ],
  },
  {
    id: T(11),
    number: 1040,
    newcomer_id: N(6),
    category: 'F8',
    priority: 'P1',
    status: 'resolved',
    route: 'hr',
    assignee_name: 'Мария Л.',
    anonymous: false,
    summary: 'Аванс пришёл меньше ожидаемого, новичок просил объяснить расчёт.',
    quote: 'Аванс меньше, чем говорили на собеседовании',
    created_min_ago: 6 * DAY,
    updated_min_ago: 5 * DAY,
    sla_in_min: null,
    notify_manager: false,
    messages: [
      {
        from: 'newcomer',
        author: null,
        text: 'Аванс меньше, чем говорили на собеседовании',
        min_ago: 6 * DAY + 5,
      },
      { from: 'bot', author: null, text: 'Передал HR, обращение #1040.', min_ago: 6 * DAY },
      {
        from: 'staff',
        author: 'Мария Л.',
        text: 'Аванс считается за отработанные дни первой половины месяца. Расчётный лист пришлю в МАКС.',
        min_ago: 5 * DAY + 1 * HOUR,
      },
      {
        from: 'bot',
        author: null,
        text: '✅ Вопрос закрыт. Если что — спрашивайте.',
        min_ago: 5 * DAY,
      },
    ],
  },
]
