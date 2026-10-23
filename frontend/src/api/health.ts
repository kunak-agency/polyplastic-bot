import { http } from '@/api/http'
import type { HealthResponse } from '@/types/api'

export async function apiGetHealth(): Promise<HealthResponse> {
  const r = await http.get<HealthResponse>('/health')
  return r.data
}
