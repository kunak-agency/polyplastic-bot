import { http } from '@/api/http'
import type { ManagerInvite, ManagerSummary, ManagerView } from '@/types/api'

// api_contract.md v4: the head of unit. No roles yet: the view is filtered by
// manager_name from the newcomer card.
export async function apiGetManagers(): Promise<ManagerSummary[]> {
  const r = await http.get<{ items: ManagerSummary[] }>('/manager/managers')
  return r.data.items
}

export async function apiGetManagerView(name: string): Promise<ManagerView> {
  const r = await http.get<ManagerView>('/manager/newcomers', { params: { name } })
  return r.data
}

export async function apiManagerInvite(newcomerId: string): Promise<ManagerInvite> {
  const r = await http.post<ManagerInvite>(`/newcomers/${newcomerId}/manager-invite`)
  return r.data
}

// The head of unit closes a 1:1 task from the cabinet: "Met" (done) or
// "Planned" — the same as the mg:met / mg:plan buttons in MAX
// (api_contract_auth.md, proposed route).
export async function apiManagerTask(taskId: string, status: 'done' | 'planned'): Promise<void> {
  await http.post(`/manager/tasks/${encodeURIComponent(taskId)}`, { status })
}
