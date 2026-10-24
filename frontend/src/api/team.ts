import { http } from '@/api/http'
import type {
  HrLeadDashboard,
  ListResponse,
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
// question (the backend counts it in the daily limit).
export async function apiRemind(newcomerId: string): Promise<void> {
  await http.post(`/newcomers/${encodeURIComponent(newcomerId)}/remind`)
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
