import { http } from '@/api/http'
import type {
  ListResponse,
  PublishRequest,
  Scenario,
  ScenarioDetail,
  ScenarioStep,
} from '@/types/api'

export async function apiListScenarios(): Promise<ListResponse<Scenario>> {
  const r = await http.get<ListResponse<Scenario>>('/scenarios')
  return r.data
}

export async function apiGetScenario(id: string): Promise<ScenarioDetail> {
  const r = await http.get<ScenarioDetail>(`/scenarios/${encodeURIComponent(id)}`)
  return r.data
}

// The whole step list replaces the draft; the answer is the draft itself.
// Body is { steps } (v1 clarifications in api_contract.md).
export async function apiPutScenarioSteps(
  id: string,
  steps: ScenarioStep[],
): Promise<ScenarioDetail> {
  const r = await http.put<ScenarioDetail>(`/scenarios/${encodeURIComponent(id)}/steps`, {
    steps,
  })
  return r.data
}

export async function apiPublishScenario(
  id: string,
  body: PublishRequest,
): Promise<ScenarioDetail> {
  const r = await http.post<ScenarioDetail>(`/scenarios/${encodeURIComponent(id)}/publish`, body)
  return r.data
}

// multipart: the instance's default JSON content type would make axios turn the
// FormData into JSON, so it is overridden; the browser then sets the boundary.
export async function apiImportScenario(file: File): Promise<ScenarioDetail> {
  const form = new FormData()
  form.append('file', file)
  const r = await http.post<ScenarioDetail>('/scenarios/import', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return r.data
}
