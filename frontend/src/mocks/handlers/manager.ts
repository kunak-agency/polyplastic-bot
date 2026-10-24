import type { MockRequest, MockResult, MockRoute } from '../adapter'
import { ensureSeeded, newcomers, view } from '../state'
import { currentUser } from './auth'
import type { ManagerCard } from '@/types/api'
import { addDays } from '@/lib/calendar'

// Head of unit (api_contract.md v4 + api_contract_auth.md): no tickets, no
// answer texts, no flag categories. Signed in as manager, the name comes from
// the session and `name` is ignored.

// 1:1 tasks of the mock, keyed by newcomer: closed ones disappear.
const closed = new Set<string>()
const planned = new Set<string>()

function active() {
  return [...newcomers.values()].map(view).filter((n) => n.stage !== 'done')
}

function managers(): MockResult {
  const by = new Map<string, number>()
  for (const n of active()) by.set(n.manager_name, (by.get(n.manager_name) ?? 0) + 1)
  const items = [...by].map(([manager_name, count]) => ({
    manager_name,
    newcomers: count,
    connected: false,
  }))
  return { status: 200, data: { items, total: items.length } }
}

function managerView(req: MockRequest): MockResult {
  const me = currentUser()
  const name = me?.role === 'manager' ? (me.manager_name ?? me.full_name) : (req.query.name ?? '')
  if (!name)
    return {
      status: 422,
      data: { error: { code: 'VALIDATION_FAILED', message: 'Укажите руководителя' } },
    }
  const items: ManagerCard[] = active()
    .filter((n) => n.manager_name === name)
    .map((n) => ({
      newcomer_id: n.id,
      full_name: n.full_name,
      site: n.site,
      department: n.department,
      position: n.position,
      start_date: n.start_date,
      day: n.day,
      stage: n.stage,
      connection: n.connection,
      last_pulse: n.last_pulse,
      probation_date: addDays(n.start_date, 90),
      ip_decision: n.ip_decision ?? null,
      attention: n.last_pulse !== null && n.last_pulse <= 3 ? ['второй низкий пульс подряд'] : [],
      meetings:
        n.last_pulse !== null && n.last_pulse <= 3 && !closed.has(`t-${n.id}`)
          ? [
              {
                id: `t-${n.id}`,
                kind: 'low_pulse_twice' as const,
                status: planned.has(`t-${n.id}`) ? ('planned' as const) : ('open' as const),
                text: 'Встретьтесь 1:1: второй низкий пульс подряд',
                created_at: new Date().toISOString(),
              },
            ]
          : [],
    }))
  return {
    status: 200,
    data: { manager_name: name, connected: false, items, total: items.length },
  }
}

function task(req: MockRequest): MockResult {
  if (req.body.status === 'done') closed.add(req.params.id)
  else planned.add(req.params.id)
  return { status: 204 }
}

const routes: MockRoute[] = [
  { method: 'POST', path: '/manager/tasks/:id', handler: task },
  { method: 'GET', path: '/manager/managers', handler: managers },
  { method: 'GET', path: '/manager/newcomers', handler: managerView },
]

export const managerRoutes: MockRoute[] = routes.map((r) => ({
  ...r,
  handler: (req) => {
    ensureSeeded()
    return r.handler(req)
  },
}))
