import type { MockRoute } from '../adapter'
import { overviewFixtures } from '../fixtures/overview'
import { indicatorsFixture } from '../fixtures/indicators'
import type { IndicatorPeriodKey, SiteCode } from '@/types/api'

const SITES: readonly string[] = ['GrPP', 'Luzhniki', 'GTP']
const PERIODS: readonly string[] = ['30d', '90d', 'q']

function invalid(field: string, message: string) {
  return {
    status: 422,
    data: {
      error: {
        code: 'VALIDATION_FAILED',
        message: 'Проверьте поля',
        traceId: 'mock',
        details: [{ field, code: 'OUT_OF_RANGE', message }],
      },
    },
  }
}

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
  {
    method: 'GET',
    path: '/indicators',
    handler: (req) => {
      const site = req.query.site
      const period = req.query.period || '30d'
      if (site && !SITES.includes(site)) return invalid('site', 'Площадка: GrPP, Luzhniki или GTP')
      if (!PERIODS.includes(period)) return invalid('period', 'Период: 30d, 90d или q')
      return {
        status: 200,
        data: indicatorsFixture((site as SiteCode) || null, period as IndicatorPeriodKey),
      }
    },
  },
]
