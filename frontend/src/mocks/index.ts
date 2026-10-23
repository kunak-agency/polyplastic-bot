import type { AxiosInstance } from 'axios'
import { createMockAdapter, type MockRoute } from './adapter'
import { healthRoutes } from './handlers/health'
import { newcomerRoutes } from './handlers/newcomers'
import { scenarioRoutes } from './handlers/scenarios'
import { ticketRoutes } from './handlers/tickets'

export const MOCKS_ENABLED = import.meta.env.VITE_USE_MOCKS === 'true'

// Swaps the instance's transport for the in-bundle handlers. A no-op unless
// VITE_USE_MOCKS is exactly "true", so a production build behaves identically
// whether or not this is called.
export function installMocks(instance: AxiosInstance): void {
  if (!MOCKS_ENABLED) return
  // One file per domain under ./handlers, its fixtures under ./fixtures, both
  // registered here; a path without a handler answers 404 in the contract's
  // error shape. Fixtures are synthetic only — no real personal data.
  const routes: MockRoute[] = [
    ...healthRoutes,
    ...newcomerRoutes,
    ...scenarioRoutes,
    ...ticketRoutes,
  ]
  instance.defaults.adapter = createMockAdapter(routes)
  console.info(`[mocks] axios adapter installed — ${routes.length} route(s) served locally`)
}
