import { http } from '@/api/http'
import type { IndicatorPeriodKey, Indicators, SiteCode } from '@/types/api'

// api_contract.md, section "indicators": every number of the Indicators screen comes
// from one call, computed by the server (no LLM). `site: null` means all sites.
export async function apiGetIndicators(
  site: SiteCode | null,
  period: IndicatorPeriodKey,
): Promise<Indicators> {
  const r = await http.get<Indicators>('/indicators', {
    params: { site: site || undefined, period },
  })
  return r.data
}
