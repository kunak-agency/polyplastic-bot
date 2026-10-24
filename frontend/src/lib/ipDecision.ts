// Decision on the probation period (api_contract.md, agent F). Pure, types
// only: the node tests load it directly.
import type { IpDecision } from '@/types/api'

export const IP_DECISIONS: readonly IpDecision[] = ['keep', 'extend', 'part']

// The manager's card shows a tone per decision; null — not set yet.
export function ipTone(d: IpDecision | null | undefined): 'success' | 'warning' | 'danger' | null {
  if (d === 'keep') return 'success'
  if (d === 'extend') return 'warning'
  if (d === 'part') return 'danger'
  return null
}
