import type { MockRoute } from '../adapter'
import { healthFixture } from '../fixtures/health'

export const healthRoutes: MockRoute[] = [
  {
    method: 'GET',
    path: '/health',
    handler: () => ({ status: 200, data: { ...healthFixture } }),
  },
]
