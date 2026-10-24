import { http } from '@/api/http'
import type { ListResponse, Person, PersonCreate, PersonRole } from '@/types/api'

// People directory (api_contract_auth.md, "People"): recruiter and hr_lead.

export async function apiListPeople(role: PersonRole, q = ''): Promise<Person[]> {
  const r = await http.get<ListResponse<Person>>('/people', {
    params: { role, q: q.trim() || undefined },
  })
  return r.data.items
}

// 201 — created, 200 — a person with the same role and name already existed.
export async function apiCreatePerson(body: PersonCreate): Promise<Person> {
  const r = await http.post<Person>('/people', body)
  return r.data
}
