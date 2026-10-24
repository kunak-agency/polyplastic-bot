import { http } from '@/api/http'
import type {
  HrLeadDashboard,
  ListResponse,
  NudgeList,
  NudgeResult,
  RecruiterDashboard,
  Role,
  SiteCode,
  TeamResponse,
  User,
  UserCreate,
  UserPatch,
} from '@/types/api'

// api_contract_auth.md v5: role dashboards, users and reassignment.

export async function apiGetRecruiterDashboard(site: SiteCode | null): Promise<RecruiterDashboard> {
  const r = await http.get<RecruiterDashboard>('/recruiter/dashboard', {
    params: { site: site || undefined },
  })
  return r.data
}

export async function apiGetHrLeadDashboard(site: SiteCode | null): Promise<HrLeadDashboard> {
  const r = await http.get<HrLeadDashboard>('/hrlead/dashboard', {
    params: { site: site || undefined },
  })
  return r.data
}

// "Mark: contacted" — the Today row of a silent newcomer goes away and comes
// back in 2 working days if there is still no answer.
export async function apiMarkContacted(newcomerId: string, note?: string): Promise<void> {
  await http.post(`/newcomers/${encodeURIComponent(newcomerId)}/contacted`, {
    note: note || undefined,
  })
}

// "Remind": one manual reminder through the bot, as a reply to the open
// question (api_contract.md, "Nudges"): 200 sent|failed, 202 planned for the
// newcomer's working hours; 409 — nothing to remind about (thrown as ApiError).
// `/remind` is the alias of `/nudge`.
export async function apiRemind(
  newcomerId: string,
): Promise<{ httpStatus: number; result: NudgeResult | null }> {
  const r = await http.post<NudgeResult | ''>(`/newcomers/${encodeURIComponent(newcomerId)}/remind`)
  return { httpStatus: r.status, result: r.data && typeof r.data === 'object' ? r.data : null }
}

// Reminder history of the card, oldest first (the card reverses it).
export async function apiGetNudges(newcomerId: string): Promise<NudgeList> {
  const r = await http.get<NudgeList>(`/newcomers/${encodeURIComponent(newcomerId)}/nudges`)
  return r.data
}

// Dev (agent L): the recruiters' morning summary now, without the 9-12 window.
// The answer shape is loose: a count, a list of sent summaries, or nothing.
export async function apiDevRecruiterMorning(): Promise<number | null> {
  const r = await http.post<{ sent?: number; items?: unknown[]; total?: number } | unknown[] | ''>(
    '/dev/recruiter-morning',
    {},
    { timeout: 30_000 },
  )
  const d = r.data
  if (Array.isArray(d)) return d.length
  if (d && typeof d === 'object') {
    if (typeof d.sent === 'number') return d.sent
    if (typeof d.total === 'number') return d.total
    if (Array.isArray(d.items)) return d.items.length
  }
  return null
}

export async function apiGetTeam(): Promise<TeamResponse> {
  const r = await http.get<TeamResponse>('/team')
  return r.data
}

export async function apiListUsers(query: { role?: Role; active?: boolean } = {}): Promise<User[]> {
  const r = await http.get<ListResponse<User>>('/users', {
    params: { role: query.role, active: query.active ? 1 : undefined },
  })
  return r.data.items
}

export async function apiCreateUser(body: UserCreate): Promise<User> {
  const r = await http.post<User>('/users', body)
  return r.data
}

export async function apiPatchUser(id: string, body: UserPatch): Promise<User> {
  const r = await http.patch<User>(`/users/${encodeURIComponent(id)}`, body)
  return r.data
}

export async function apiReassign(
  newcomerIds: string[],
  recruiterId: string | null,
): Promise<{ updated: number }> {
  const r = await http.post<{ updated: number }>('/newcomers/reassign', {
    newcomer_ids: newcomerIds,
    recruiter_id: recruiterId,
  })
  return r.data
}
