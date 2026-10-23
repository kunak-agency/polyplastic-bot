import { http } from '@/api/http'
import type { Digest, DigestSendResult, SiteCode } from '@/types/api'

// api_contract.md v4: the weekly digest. Numbers come from code; only the
// connected text is written by the LLM over aggregates.
export async function apiGetDigest(week: string | null, site: SiteCode | null): Promise<Digest> {
  const r = await http.get<Digest>('/digest', {
    params: { week: week || undefined, site: site || undefined },
    // The first open of a week waits for the model (cached afterwards).
    timeout: 40_000,
  })
  return r.data
}

// Dev only: send the digest to MAX now (APP_ENV=development).
export async function apiSendDigest(week: string | null): Promise<DigestSendResult> {
  const r = await http.post<DigestSendResult>(
    '/dev/digest/send',
    { week: week || '' },
    { timeout: 40_000 },
  )
  return r.data
}
