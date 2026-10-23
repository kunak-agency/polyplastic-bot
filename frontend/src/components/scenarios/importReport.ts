import { computed, ref, type Ref } from 'vue'

// import_report of POST /api/scenarios/import and /scenarios/{id}/import
// (api_contract.md, v4). Only the contract fields the screen reads are required.
export interface ImportWarning {
  row: number
  code: string
  message: string
  step_key: string
  step_id?: string
}

export interface ImportReport {
  blocks: number
  messages: number
  questions: number
  branches: number
  rules: number
  media_names: number
  media_name_list?: string[]
  surveys?: number
  survey_questions?: number
  outcomes?: number
  commands?: number
  implicit_yes_no?: number
  next_button?: number
  question_first?: number
  hr_tasks?: number
  warnings: ImportWarning[]
}

export interface WithImportReport {
  import_report?: ImportReport
}

// The report outlives the jump from the scenario list to the editor: kept per
// scenario id for this tab (sessionStorage may be unavailable — then memory only).
const STORAGE_KEY = 'scenarios.importReports'
const reports = ref<Record<string, ImportReport>>(readStored())

function readStored(): Record<string, ImportReport> {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as Record<string, ImportReport>) : {}
  } catch {
    return {}
  }
}

function store(): void {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(reports.value))
  } catch {
    // private mode or quota: the report stays in memory
  }
}

export function rememberImportReport(id: string, data: unknown): void {
  const r = (data as WithImportReport | null)?.import_report
  if (!r) return
  reports.value = { ...reports.value, [id]: { ...r, warnings: r.warnings ?? [] } }
  store()
}

export function useImportReport(id: Ref<string>) {
  const report = computed(() => reports.value[id.value] ?? null)
  function clear(): void {
    const next = { ...reports.value }
    delete next[id.value]
    reports.value = next
    store()
  }
  return { report, clear }
}
