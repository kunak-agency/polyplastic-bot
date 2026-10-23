import { http } from '@/api/http'
import type { Overview, SiteCode } from '@/types/api'

// api_contract.md v3: every number on the Overview screen comes from one call,
// computed by the server's code (no LLM). `site: null` means all sites.
export async function apiGetOverview(site: SiteCode | null): Promise<Overview> {
  const r = await http.get<Overview>('/overview', { params: { site: site || undefined } })
  return r.data
}
