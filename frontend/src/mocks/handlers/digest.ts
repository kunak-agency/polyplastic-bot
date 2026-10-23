import type { MockRoute } from '../adapter'
import { digestFixture, managersFixture, managerViewFixture } from '../fixtures/digest'

export const digestRoutes: MockRoute[] = [
  {
    method: 'GET',
    path: '/digest',
    handler: (req) => ({
      status: 200,
      data: { ...structuredClone(digestFixture), week: req.query.week || digestFixture.week },
    }),
  },
  {
    method: 'POST',
    path: '/dev/digest/send',
    handler: () => ({
      status: 200,
      data: {
        week: digestFixture.week,
        sent_to: 0,
        logged: true,
        text: digestFixture.max_message.text,
      },
    }),
  },
  {
    method: 'GET',
    path: '/manager/managers',
    handler: () => ({
      status: 200,
      data: { items: managersFixture, total: managersFixture.length },
    }),
  },
  {
    method: 'GET',
    path: '/manager/newcomers',
    handler: (req) => ({
      status: 200,
      data: { ...structuredClone(managerViewFixture), manager_name: req.query.name ?? '' },
    }),
  },
  {
    method: 'POST',
    path: '/newcomers/:id/manager-invite',
    handler: () => ({
      status: 200,
      data: {
        manager_name: managerViewFixture.manager_name,
        link: 'https://max.ru/your_bot?start=mgr_mocktoken',
        expires_at: '2026-11-23T10:00:00+03:00',
        connected: false,
      },
    }),
  },
]
