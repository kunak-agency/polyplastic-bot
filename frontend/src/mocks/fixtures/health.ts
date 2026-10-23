import type { HealthResponse } from '@/types/api'

// Synthetic: the mock backend is always healthy. Flip a field to "down" here to
// see the degraded state of the API badge without stopping the real services.
export const healthFixture: HealthResponse = {
  status: 'ok',
  db: 'ok',
  rag: 'ok',
}
