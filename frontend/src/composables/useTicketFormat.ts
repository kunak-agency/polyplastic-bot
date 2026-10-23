import { useI18n } from 'vue-i18n'
import { durationParts, slaState } from '@/lib/tickets'
import type { Ticket } from '@/types/api'

// Display text of the SLA timer and categories; the rules stay in lib/tickets.ts.
export function useTicketFormat() {
  const { t, d } = useI18n()

  function duration(ms: number): string {
    const p = durationParts(ms)
    if (p.days > 0) return t('tickets.duration.dh', { d: p.days, h: p.hours })
    if (p.hours > 0 && p.minutes === 0) return t('tickets.duration.h', { h: p.hours })
    if (p.hours > 0) return t('tickets.duration.hm', { h: p.hours, m: p.minutes })
    return t('tickets.duration.m', { m: p.minutes })
  }

  function slaText(ticket: Pick<Ticket, 'status' | 'sla_due_at'>, now: number): string {
    if (ticket.status === 'resolved') return t('tickets.status.resolved')
    const s = slaState(ticket, now)
    if (s.kind === 'none') return t('tickets.sla.none')
    if (s.kind === 'overdue') return t('tickets.sla.overdue', { time: duration(s.ms) })
    return t('tickets.sla.left', { time: duration(s.ms) })
  }

  function slaTitle(ticket: Pick<Ticket, 'sla_due_at'>): string | undefined {
    return ticket.sla_due_at
      ? t('tickets.sla.title', { date: d(new Date(ticket.sla_due_at), 'long') })
      : undefined
  }

  function category(ticket: Pick<Ticket, 'category'>): string {
    const name = t(`tickets.category.${ticket.category}`)
    return ticket.category === 'question' ? name : `${ticket.category} · ${name}`
  }

  return { duration, slaText, slaTitle, category }
}
