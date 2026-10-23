import type {
  Newcomer,
  ScenarioDetail,
  ScenarioStep,
  TicketDetail,
  TimelineEvent,
} from '@/types/api'
import { addDays, daysBetween, localIsoDate, resolveFireDate, stepAppliesTo } from '@/lib/calendar'
import { stageForDay } from '@/lib/scenarioSteps'
import { uuidV4 } from '@/lib/ids'
import { scenarioFixtures } from './fixtures/scenarios'
import { newcomerSeeds } from './fixtures/newcomers'
import { ticketSeeds } from './fixtures/tickets'

// In-memory "backend" shared by the newcomers and scenarios handlers: created
// newcomers, time travel and published versions live until the page reloads.

const SITE_ZONE = '+03:00'
const BOT_LINK = 'https://max.ru/korsik_demo_bot?start=inv_'

export function today(): string {
  return localIsoDate()
}

function randomHex(len: number): string {
  const bytes = new Uint8Array(Math.ceil(len / 2))
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0'))
    .join('')
    .slice(0, len)
}

export const newId = uuidV4

export function inviteLink(): string {
  return BOT_LINK + randomHex(16)
}

export const scenarios = new Map<string, ScenarioDetail>()

// Stored without the computed `day` and `stage`: those follow the calendar.
type StoredNewcomer = Omit<Newcomer, 'day' | 'stage'>
export const newcomers = new Map<string, StoredNewcomer>()
export const timelines = new Map<string, TimelineEvent[]>()

// Tickets keep their messages next to them; handlers strip what the list or a
// restricted ticket must not show (handlers/tickets.ts).
export const tickets = new Map<string, TicketDetail>()

// open_tickets follows the ticket mock, so taking or resolving a ticket shows
// up in the registry too.
function openTicketsOf(newcomerId: string, fallback: number): number {
  if (tickets.size === 0) return fallback
  let count = 0
  for (const t of tickets.values()) {
    if (t.newcomer_id === newcomerId && t.status !== 'resolved') count++
  }
  return count
}

export function view(n: StoredNewcomer): Newcomer {
  const day = daysBetween(n.start_date, today())
  return { ...n, day, stage: stageForDay(day), open_tickets: openTicketsOf(n.id, n.open_tickets) }
}

export function activeCount(scenarioId: string): number {
  let count = 0
  for (const n of newcomers.values()) {
    if (n.scenario_id === scenarioId && view(n).stage !== 'done') count++
  }
  return count
}

function answerFor(step: ScenarioStep, pulse: number | null): string | null {
  if (step.survey) {
    if (step.survey.scale === '1-5') return String(pulse ?? 4)
    if (step.survey.scale === '1-10') return '8'
    if (step.survey.scale === 'yesno') return 'yes'
    return step.survey.questions.map((q) => q.options[0] ?? '').join(' / ')
  }
  return step.buttons[0]?.label ?? null
}

interface HistoryHints {
  connection: Newcomer['connection']
  silentDays: number | null
  pulse: number | null
  // The day the newcomer stopped the bot, for 'stopped'.
  stoppedOn: number | null
}

// Events of one newcomer from the scenario version at launch, as the planner
// would schedule them. For seeded people the past is filled in plausibly.
// The server's fire time of a step for a start date, ISO with the site zone.
export function fireAt(startDate: string, step: ScenarioStep): string {
  const fire = resolveFireDate(startDate, step.day_offset, step.time, step.work_hours_only)
  return `${fire.date}T${step.time}:00${SITE_ZONE}`
}

export function buildTimeline(
  n: StoredNewcomer,
  scenario: ScenarioDetail,
  hints: HistoryHints | null,
): TimelineEvent[] {
  const now = today()
  return scenario.steps
    .filter((s) => stepAppliesTo(s, n.site))
    .slice()
    .sort((a, b) => a.day_offset - b.day_offset || a.time.localeCompare(b.time))
    .map((s) => {
      const fire = resolveFireDate(n.start_date, s.day_offset, s.time, s.work_hours_only)
      let status: TimelineEvent['status'] = 'scheduled'
      let answer: string | null = null
      if (hints && fire.date < now) {
        const ago = daysBetween(fire.date, now)
        if (hints.connection === 'invited') status = 'skipped'
        else if (hints.stoppedOn !== null && s.day_offset > hints.stoppedOn) status = 'failed'
        else if (hints.silentDays !== null && ago <= hints.silentDays) status = 'sent'
        else {
          answer = answerFor(s, hints.pulse)
          status = answer ? 'answered' : 'sent'
        }
      }
      return {
        event_id: newId(),
        step_id: s.id,
        day_offset: s.day_offset,
        title: s.title,
        kind: s.kind,
        fire_at: fireAt(n.start_date, s),
        status,
        answer,
        text: null,
      }
    })
}

let seeded = false

// Lazy: the handlers module is bundled even when mocks are off, and a seed at
// import time would run in every build. Each handler calls this first.
export function ensureSeeded(): void {
  if (seeded) return
  seeded = true
  for (const s of scenarioFixtures(new Date().toISOString())) scenarios.set(s.id, s)
  const now = today()
  for (const s of newcomerSeeds) {
    const scenario = scenarios.get(s.scenario_id)
    if (!scenario) continue
    const lastActivity =
      s.silent_days === null
        ? null
        : new Date(Date.now() - s.silent_days * 86_400_000 - 3_600_000).toISOString()
    const stored: StoredNewcomer = {
      id: s.id,
      full_name: s.full_name,
      phone: s.phone,
      site: s.site,
      department: s.department,
      position: s.position,
      manager_name: s.manager_name,
      mentor_name: s.mentor_name,
      recruiter_name: s.recruiter_name,
      start_date: addDays(now, -s.day),
      shift: s.shift,
      scenario_id: s.scenario_id,
      scenario_version: scenario.version,
      address_form: s.connection === 'invited' ? null : 'vy',
      consent: s.connection === 'invited' ? null : 'given',
      connection: s.connection,
      invite_link: inviteLink(),
      last_activity_at: lastActivity,
      last_pulse: s.last_pulse,
      risk: s.risk,
      risk_reason: s.risk_reason,
      open_tickets: s.open_tickets,
    }
    newcomers.set(s.id, stored)
    timelines.set(
      s.id,
      buildTimeline(stored, scenario, {
        connection: s.connection,
        silentDays: s.silent_days,
        pulse: s.last_pulse,
        stoppedOn: s.connection === 'stopped' ? s.day - (s.silent_days ?? 0) : null,
      }),
    )
  }
  seedTickets()
}

function minutesAgo(min: number): string {
  return new Date(Date.now() - min * 60_000).toISOString()
}

function seedTickets(): void {
  const now = today()
  for (const s of ticketSeeds) {
    const n = newcomers.get(s.newcomer_id)
    if (!n) continue
    const created = minutesAgo(s.created_min_ago)
    tickets.set(s.id, {
      id: s.id,
      number: s.number,
      newcomer_id: n.id,
      newcomer_name: n.full_name,
      site: n.site,
      // The newcomer's day when the ticket was created.
      day: daysBetween(n.start_date, now) - Math.floor(s.created_min_ago / 1440),
      category: s.category,
      priority: s.priority,
      status: s.status,
      route: s.route,
      assignee_name: s.assignee_name,
      anonymous: s.anonymous,
      summary: s.summary,
      quote: s.quote,
      sla_due_at: s.sla_in_min === null ? null : minutesAgo(-s.sla_in_min),
      created_at: created,
      updated_at: minutesAgo(s.updated_min_ago),
      notify_manager: s.notify_manager,
      restricted: s.category === 'F5' || s.category === 'F9',
      messages: s.messages
        .slice()
        .sort((a, b) => b.min_ago - a.min_ago)
        .map((m) => ({
          from: m.from,
          author: m.author,
          text: m.text,
          at: minutesAgo(m.min_ago),
          internal: m.internal ?? false,
          hidden: false,
        })),
    })
  }
}
