import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiListDocuments, apiUnanswered } from '@/api/knowledge'
import type {
  KnowledgeDocument,
  KnowledgeUnansweredCluster,
  KnowledgeUnansweredQuery,
} from '@/types/api'

// The documents table and the unanswered-questions clusters. Only the answer
// to the latest request lands (a slow list must not overwrite a fresh one
// after an upload).
export const useKnowledgeStore = defineStore('knowledge', () => {
  const documents = ref<KnowledgeDocument[]>([])
  const ragUp = ref(true)
  const loaded = ref(false)
  const loading = ref(false)
  const error = ref<unknown>(null)
  let docsTicket = 0

  async function loadDocuments(): Promise<void> {
    const mine = ++docsTicket
    loading.value = true
    error.value = null
    try {
      const r = await apiListDocuments()
      if (mine !== docsTicket) return
      documents.value = r.items
      ragUp.value = r.rag === 'ok'
      loaded.value = true
    } catch (e) {
      if (mine === docsTicket) error.value = e
    } finally {
      if (mine === docsTicket) loading.value = false
    }
  }

  // Put a just-uploaded document in place without waiting for the list.
  function upsert(doc: KnowledgeDocument): void {
    const i = documents.value.findIndex((d) => d.id === doc.id || d.doc === doc.doc)
    if (i >= 0) documents.value.splice(i, 1, doc)
    else documents.value.unshift(doc)
  }

  const clusters = ref<KnowledgeUnansweredCluster[]>([])
  const questions = ref(0)
  const unansweredLoaded = ref(false)
  const unansweredLoading = ref(false)
  const unansweredError = ref<unknown>(null)
  let unansweredTicket = 0

  async function loadUnanswered(query: KnowledgeUnansweredQuery): Promise<void> {
    const mine = ++unansweredTicket
    unansweredLoading.value = true
    unansweredError.value = null
    try {
      const r = await apiUnanswered(query)
      if (mine !== unansweredTicket) return
      clusters.value = r.items
      questions.value = r.questions
      unansweredLoaded.value = true
    } catch (e) {
      if (mine === unansweredTicket) unansweredError.value = e
    } finally {
      if (mine === unansweredTicket) unansweredLoading.value = false
    }
  }

  return {
    documents,
    ragUp,
    loaded,
    loading,
    error,
    loadDocuments,
    upsert,
    clusters,
    questions,
    unansweredLoaded,
    unansweredLoading,
    unansweredError,
    loadUnanswered,
  }
})
