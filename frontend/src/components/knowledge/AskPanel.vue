<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleAlert, Info, LoaderCircle, Quote } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { apiAskDocuments } from '@/api/knowledge'
import { errorMessage } from '@/lib/apiError'
import { isNumberedClause, sourceFromDoc } from '@/lib/knowledge'
import type { KnowledgeAskResponse } from '@/types/api'

// "Check" tab (ux-spec 3.6): the HR asks what a newcomer would ask and sees
// the bot's answer with quotes and clause ids — the same pipeline. The rag
// searches the whole base, so quotes from this document are highlighted and a
// banner says when the answer came from another one.
const props = defineProps<{ doc: string }>()
const { t } = useI18n()

const question = ref('')
const loading = ref(false)
const error = ref('')
const result = ref<KnowledgeAskResponse | null>(null)
const asked = ref('')

async function ask(): Promise<void> {
  const q = question.value.trim()
  if (q.length < 3 || loading.value) return
  loading.value = true
  error.value = ''
  try {
    result.value = await apiAskDocuments({ question: q, doc: props.doc })
    asked.value = q
  } catch (e) {
    result.value = null
    error.value = errorMessage(e, 'knowledge.check.failed')
  } finally {
    loading.value = false
  }
}

function onKey(e: KeyboardEvent): void {
  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
    e.preventDefault()
    void ask()
  }
}
</script>

<template>
  <div class="col gap-4">
    <form class="col gap-2" @submit.prevent="ask">
      <label class="field-label" for="kb-ask">{{ t('knowledge.check.label') }}</label>
      <textarea
        id="kb-ask"
        v-model="question"
        class="textarea kb-ask"
        rows="2"
        maxlength="1000"
        :placeholder="t('knowledge.check.placeholder')"
        @keydown="onKey"
      />
      <div class="row between center gap-3">
        <span class="txt-xs muted">{{ t('knowledge.check.note') }}</span>
        <Button
          type="submit"
          variant="primary"
          size="sm"
          :disabled="loading || question.trim().length < 3"
        >
          <LoaderCircle v-if="loading" :size="14" class="kb-spin" aria-hidden="true" />
          {{ loading ? t('knowledge.actions.checking') : t('knowledge.actions.check') }}
        </Button>
      </div>
    </form>

    <div v-if="error" class="kb-alert kb-alert--danger" role="alert">
      <CircleAlert :size="16" aria-hidden="true" />
      <span>{{ error }}</span>
    </div>

    <section v-if="result" class="kb-result" aria-live="polite" data-testid="kb-answer">
      <div class="txt-xs muted kb-result__q">«{{ asked }}»</div>

      <template v-if="result.needs_escalation">
        <div class="kb-alert kb-alert--warning">
          <Info :size="16" aria-hidden="true" />
          <div>
            <div class="fw-semi">{{ t('knowledge.check.abstainTitle') }}</div>
            <div class="txt-sm">
              {{ t(`knowledge.check.abstain.${result.abstain_reason ?? 'no_source'}`) }}
            </div>
          </div>
        </div>
      </template>

      <template v-else>
        <div class="row between center">
          <span class="field-label">{{ t('knowledge.check.answer') }}</span>
          <Badge
            :variant="
              result.confidence === 'high'
                ? 'success'
                : result.confidence === 'medium'
                  ? 'info'
                  : 'warning'
            "
          >
            {{ t(`knowledge.check.confidence.${result.confidence}`) }}
          </Badge>
        </div>
        <p class="kb-answer">{{ result.answer }}</p>

        <div v-if="result.from_doc === false" class="kb-alert kb-alert--warning">
          <Info :size="16" aria-hidden="true" />
          <span class="txt-sm">{{ t('knowledge.check.fromOther') }}</span>
        </div>

        <div class="field-label">{{ t('knowledge.check.sources') }}</div>
        <ol class="kb-sources">
          <li
            v-for="(s, i) in result.sources"
            :key="i"
            :class="['kb-source', { 'kb-source--this': sourceFromDoc(s, doc) }]"
          >
            <div class="kb-source__head">
              <Quote :size="13" aria-hidden="true" />
              <span class="fw-medium">{{ s.doc }}</span>
              <span class="badge td-mono">{{
                isNumberedClause(s.clause_id)
                  ? t('knowledge.check.clause', { id: s.clause_id })
                  : s.clause_id
              }}</span>
              <span v-if="s.page !== null" class="txt-xs muted">{{
                t('knowledge.check.page', { n: s.page })
              }}</span>
            </div>
            <blockquote class="kb-quote">{{ s.quote }}</blockquote>
          </li>
        </ol>
      </template>
    </section>
  </div>
</template>

<style scoped>
.kb-ask {
  resize: vertical;
  min-height: 64px;
}
.kb-result {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
}
.kb-result__q {
  font-style: italic;
}
.kb-answer {
  margin: 0;
  line-height: 1.5;
}
.kb-sources {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.kb-source {
  padding: 10px 12px;
  border-radius: var(--radius-md);
  border: 1px solid var(--border-subtle);
  background: var(--bg-subtle);
}
.kb-source--this {
  border-color: color-mix(in oklab, var(--accent) 45%, var(--border));
}
.kb-source__head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  color: var(--fg-muted);
}
.kb-quote {
  margin: 6px 0 0;
  padding-left: 10px;
  border-left: 2px solid var(--accent);
  color: var(--fg);
  line-height: 1.5;
}
.kb-alert {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border-radius: var(--radius-md);
}
.kb-alert > svg {
  flex-shrink: 0;
  margin-top: 2px;
}
.kb-alert--warning {
  background: var(--warning-soft);
  color: var(--fg);
}
.kb-alert--warning > svg {
  color: var(--warning);
}
.kb-alert--danger {
  background: var(--danger-soft);
  color: var(--danger);
}
.kb-spin {
  animation: kb-spin 1s linear infinite;
}
@keyframes kb-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
