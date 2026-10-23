import { http } from '@/api/http'
import type {
  ListResponse,
  PublishPreview,
  PublishRequest,
  ScenarioPatch,
  ScenarioStats,
  SendTestRequest,
  SiteCode,
  Scenario,
  ScenarioCreateRequest,
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

// 201 + the new draft; 409 scenario_name_taken when the name is in use.
export async function apiCreateScenario(body: ScenarioCreateRequest): Promise<ScenarioDetail> {
  const r = await http.post<ScenarioDetail>('/scenarios', body)
  return r.data
}

// 204; only a never-published draft without newcomers (409 otherwise).
export async function apiDeleteScenario(id: string): Promise<void> {
  await http.delete(`/scenarios/${encodeURIComponent(id)}`)
}

// Excel steps replace the draft of an existing scenario (the empty editor's
// "Upload Excel"); the answer is the scenario with steps.
export async function apiImportScenarioSteps(id: string, file: File): Promise<ScenarioDetail> {
  const form = new FormData()
  form.append('file', file)
  const r = await http.post<ScenarioDetail>(`/scenarios/${encodeURIComponent(id)}/import`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return r.data
}

// v4: per-step and per-button numbers; shares only at >= min_answers answers.
export async function apiScenarioStats(id: string, site: SiteCode | null): Promise<ScenarioStats> {
  const r = await http.get<ScenarioStats>(`/scenarios/${encodeURIComponent(id)}/stats`, {
    params: site ? { site } : {},
  })
  return r.data
}

// v4: what publishing the saved draft would do (nothing is written).
export async function apiPublishPreview(id: string): Promise<PublishPreview> {
  const r = await http.post<PublishPreview>(
    `/scenarios/${encodeURIComponent(id)}/publish/preview`,
    {},
  )
  return r.data
}

// v4 (bot side): send the step to the HR's own MAX chat; with a button — walk
// that branch too.
export async function apiSendTest(
  id: string,
  stepId: string,
  body: SendTestRequest,
): Promise<void> {
  await http.post(
    `/scenarios/${encodeURIComponent(id)}/steps/${encodeURIComponent(stepId)}/send-test`,
    body,
  )
}

// v4: scenario commands (/food, /hr) and rules (free question, survey
// reminder); the answer is the scenario with steps.
export async function apiPatchScenario(id: string, body: ScenarioPatch): Promise<ScenarioDetail> {
  const r = await http.patch<ScenarioDetail>(`/scenarios/${encodeURIComponent(id)}`, body)
  return r.data
}
