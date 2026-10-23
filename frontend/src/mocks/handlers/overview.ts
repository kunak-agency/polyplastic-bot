import type { MockRoute } from '../adapter'
import { overviewFixtures } from '../fixtures/overview'
import type { SiteCode } from '@/types/api'

const SITES: readonly string[] = ['GrPP', 'Luzhniki', 'GTP']

export const overviewRoutes: MockRoute[] = [
  {
    method: 'GET',
    path: '/overview',
    handler: (req) => {
      const site = req.query.site
      if (site && !SITES.includes(site)) {
        return {
          status: 422,
          data: {
            error: {
              code: 'VALIDATION_FAILED',
              message: 'Проверьте поля',
              traceId: 'mock',
              details: [
                {
                  field: 'site',
                  code: 'OUT_OF_RANGE',
                  message: 'Площадка: GrPP, Luzhniki или GTP',
                },
              ],
            },
          },
        }
      }
      const base = overviewFixtures[(site as SiteCode) || 'all']
      return {
        status: 200,
        data: { ...structuredClone(base), generated_at: new Date().toISOString() },
      }
    },
  },
]
