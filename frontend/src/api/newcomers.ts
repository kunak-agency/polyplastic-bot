import { http } from '@/api/http'
import type {
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

// Development only: the server answers 404 outside APP_ENV=development.
export async function apiSimulateStart(body: SimulateStartRequest): Promise<Newcomer> {
  const r = await http.post<Newcomer>('/dev/simulate-start', body)
  return r.data
}
