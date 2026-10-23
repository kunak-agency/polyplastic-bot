import { defineStore } from 'pinia'
import { ref } from 'vue'

// Sites of the case. The codes are data (they go to the API as `site`, see
// api_contract.md), so they stay Latin; display labels live in the catalogue.
export const SITES = ['GrPP', 'Luzhniki', 'GTP'] as const
export type Site = (typeof SITES)[number]

// 'all' is the UI value of "no filter": the API receives `site: null`.
export type SiteFilter = 'all' | Site

export const SITE_FILTERS: readonly SiteFilter[] = ['all', ...SITES]

// Global site filter from the topbar (ux-spec §3.0: applies to every section).
// UI only for now: no section reads it until the domain endpoints exist. Not
// persisted on purpose — a pasted link must not open pre-filtered for someone
// else; when filtering becomes real it moves into the route query.
export const useSiteStore = defineStore('site', () => {
  const current = ref<SiteFilter>('all')

  function setSite(value: SiteFilter): void {
    current.value = value
  }

  // Value for the API's `site` field.
  function apiSite(): Site | null {
    return current.value === 'all' ? null : current.value
  }

  return { current, setSite, apiSite }
})
