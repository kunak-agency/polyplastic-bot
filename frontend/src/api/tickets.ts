import { http } from '@/api/http'
import type {
  ListResponse,
  Ticket,
  TicketAssignRequest,
  TicketDetail,
  TicketListQuery,
  TicketNoteRequest,
  TicketPatchRequest,
  TicketReplyRequest,
  TicketResolveRequest,
} from '@/types/api'

// api_contract.md v2. The contract names what assign answers (the ticket) but
// not what reply, note and resolve answer, so screens re-read the ticket after
// any action instead of relying on a response body.

function path(id: string, action?: string): string {
  const base = `/tickets/${encodeURIComponent(id)}`
  return action ? `${base}/${action}` : base
}

export async function apiListTickets(query: TicketListQuery = {}): Promise<ListResponse<Ticket>> {
  const r = await http.get<ListResponse<Ticket>>('/tickets', {
    params: {
      status: query.status || undefined,
      site: query.site || undefined,
      route: query.route || undefined,
      newcomer_id: query.newcomer_id || undefined,
      mine: query.mine ? 'true' : undefined,
    },
  })
  return r.data
}

export async function apiGetTicket(id: string): Promise<TicketDetail> {
  const r = await http.get<TicketDetail>(path(id))
  return r.data
}

// "Notify the manager" (no text, category only). 422 for the categories hidden
// from the manager (F0, F4, F5, F9, F10).
export async function apiPatchTicket(id: string, body: TicketPatchRequest): Promise<TicketDetail> {
  const r = await http.patch<TicketDetail>(path(id), body)
  return r.data
}

export async function apiAssignTicket(id: string, body: TicketAssignRequest): Promise<Ticket> {
  const r = await http.post<Ticket>(path(id, 'assign'), body)
  return r.data
}

// Goes to the newcomer through the bot, signed "<Name>, HR (ticket #N):"; the
// ticket turns human_active (resolved with resolve: true) and the LLM pauses.
export async function apiReplyTicket(id: string, body: TicketReplyRequest): Promise<void> {
  await http.post(path(id, 'reply'), body)
}

// Internal note: never leaves the cabinet.
export async function apiNoteTicket(id: string, body: TicketNoteRequest): Promise<void> {
  await http.post(path(id, 'note'), body)
}

// done_note — "what was done", one line: an internal note in the thread and,
// for an addressed ticket (not anonymous, not F5/F9/F10), a message to the
// newcomer "On your signal: ...".
export async function apiResolveTicket(id: string, body: TicketResolveRequest = {}): Promise<void> {
  await http.post(path(id, 'resolve'), body)
}
