import { http } from '@/api/http'
import type {
  IpDecision,
  ListResponse,
  Newcomer,
  NewcomerCreate,
  NewcomerListQuery,
  NewcomerPatch,
  SimulateStartRequest,
  TimelineResponse,
} from '@/types/api'

// Empty filters are left out of the query (axios drops undefined), so the
// server never receives `site=` or `q=` meaning "no filter".
export async function apiListNewcomers(
  query: NewcomerListQuery = {},
): Promise<ListResponse<Newcomer>> {
  const r = await http.get<ListResponse<Newcomer>>('/newcomers', {
    params: {
      site: query.site || undefined,
      q: query.q?.trim() || undefined,
      risk: query.risk || undefined,
      mine: query.mine ? 1 : undefined,
      recruiter_id: query.recruiter_id || undefined,
    },
  })
  return r.data
}

// "Launch adaptation": the server creates the timeline events right away, from
// the scenario version current at that moment, and answers with invite_link.
export async function apiCreateNewcomer(body: NewcomerCreate): Promise<Newcomer> {
  const r = await http.post<Newcomer>('/newcomers', body)
  return r.data
}

export async function apiGetNewcomer(id: string): Promise<Newcomer> {
  const r = await http.get<Newcomer>(`/newcomers/${encodeURIComponent(id)}`)
  return r.data
}

export async function apiGetTimeline(id: string): Promise<TimelineResponse> {
  const r = await http.get<TimelineResponse>(`/newcomers/${encodeURIComponent(id)}/timeline`)
  return r.data
}

// Sends every not-yet-sent event up to and including `day` (marked [TEST] by the
// server) and answers with the refreshed timeline.
export async function apiTimeTravel(id: string, day: number): Promise<TimelineResponse> {
  const r = await http.post<TimelineResponse>(`/newcomers/${encodeURIComponent(id)}/time-travel`, {
    day,
  })
  return r.data
}

// Moves the start date; the answer is the newcomer with the new day and stage.
export async function apiPatchNewcomer(id: string, body: NewcomerPatch): Promise<Newcomer> {
  const r = await http.patch<Newcomer>(`/newcomers/${encodeURIComponent(id)}`, body)
  return r.data
}

// Decision on the probation period (agent F): recruiter and hr_lead; null —
// back to "not set". Answers with the card.
export async function apiSetIpDecision(id: string, decision: IpDecision | null): Promise<Newcomer> {
  const r = await http.post<Newcomer>(`/newcomers/${encodeURIComponent(id)}/ip-decision`, {
    decision,
  })
  return r.data
}

// Development only: the server answers 404 outside APP_ENV=development.
export async function apiSimulateStart(body: SimulateStartRequest): Promise<Newcomer> {
  const r = await http.post<Newcomer>('/dev/simulate-start', body)
  return r.data
}

// POST /api/newcomers/{id}/flags (contract v4): the recruiter's go-ahead
// (recruiter_enabled) releases the steps held for it.
export async function apiSetNewcomerFlag(
  id: string,
  flag: string,
  value = true,
): Promise<{ flags: Record<string, boolean> }> {
  const r = await http.post<{ flags: Record<string, boolean> }>(
    `/newcomers/${encodeURIComponent(id)}/flags`,
    { flag, value },
  )
  return r.data
}

export interface NewcomerFile {
  name: string
  filename: string
  mime: string
  size: number
  updated_at: string
}

// POST /api/newcomers/{id}/files (api_contract.md v4, "personal files"): the
// newcomer's own adaptation plan, sent by the step with
// media: [{personal: "adaptation_plan"}]. The same name replaces the file.
export async function apiUploadNewcomerFile(
  id: string,
  file: File,
  name = 'adaptation_plan',
): Promise<NewcomerFile> {
  const f = new FormData()
  f.append('file', file)
  f.append('name', name)
  const r = await http.post<NewcomerFile>(`/newcomers/${encodeURIComponent(id)}/files`, f, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return r.data
}
