<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { MessageCircleQuestion, ShieldCheck, Upload } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import { Button } from '@/components/ui/button'
import { useKnowledgeStore } from '@/stores/knowledge'
import { useSiteStore } from '@/stores/site'
import { errorMessage } from '@/lib/apiError'
import type { KnowledgeUnansweredCluster } from '@/types/api'

// "Unanswered questions" (ux-spec 3.6, Leena Insights): the rag's refusals
// grouped by similarity on the server (no LLM). Each group offers "Upload a
// document" right from the cluster.
const emit = defineEmits<{ upload: [cluster: KnowledgeUnansweredCluster] }>()
const { t, d } = useI18n()
const store = useKnowledgeStore()
const { clusters, questions, unansweredLoaded, unansweredLoading, unansweredError } =
  storeToRefs(store)
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)

const PERIODS = [7, 30, 90] as const
const days = ref<(typeof PERIODS)[number]>(30)
const includeTest = ref(false)

function reload(): void {
  void store.loadUnanswered({
    site: site.apiSite(),
    days: days.value,
    include_test: includeTest.value,
  })
}
watch([currentSite, days, includeTest], reload)
onMounted(reload)

const REASONS = ['no_source', 'below_threshold'] as const
</script>

<template>
  <div class="col gap-4">
    <div class="row between center wrap gap-3">
      <div class="row center gap-3 wrap">
        <div class="tabs" role="group" :aria-label="t('knowledge.unanswered.period')">
          <button
            v-for="p in PERIODS"
            :key="p"
            type="button"
            :class="['tab', { active: days === p }]"
            :aria-pressed="days === p"
            @click="days = p"
          >
            {{ t('knowledge.unanswered.days', p) }}
          </button>
        </div>
        <button
          type="button"
          :class="['switch', { on: includeTest }]"
          role="switch"
          :aria-checked="includeTest"
          :aria-label="t('knowledge.unanswered.includeTest')"
          @click="includeTest = !includeTest"
        />
        <span class="txt-sm muted">{{ t('knowledge.unanswered.includeTest') }}</span>
      </div>
      <span v-if="unansweredLoaded" class="txt-sm muted">
        {{
          t('knowledge.unanswered.total', {
            questions: t('knowledge.unanswered.count', questions),
            clusters: t('knowledge.unanswered.clusters', clusters.length),
          })
        }}
      </span>
    </div>

    <div class="hint-banner txt-sm kb-privacy">
      <ShieldCheck :size="15" aria-hidden="true" />{{ t('knowledge.unanswered.privacy') }}
    </div>

    <SectionCard v-if="!unansweredLoaded">
      <LoadingState v-if="unansweredLoading || !unansweredError" :rows="3" variant="card" />
      <ErrorState v-else :message="errorMessage(unansweredError, 'knowledge.unanswered.failed')">
        <template #retry>
          <Button size="sm" @click="reload">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
    </SectionCard>

    <SectionCard v-else-if="clusters.length === 0">
      <EmptyState
        :icon="MessageCircleQuestion"
        :title="t('knowledge.unanswered.empty.title')"
        :subtitle="t('knowledge.unanswered.empty.description')"
      />
    </SectionCard>

    <ul v-else :class="['kb-clusters', { 'is-stale': unansweredLoading }]">
      <li v-for="c in clusters" :key="c.key" class="card kb-cluster">
        <div class="kb-cluster__main">
          <div class="row center gap-2 wrap">
            <span class="kb-count tabular">{{ c.count }}</span>
            <span class="fw-semi kb-cluster__title">«{{ c.title }}»</span>
          </div>
          <div class="row center gap-2 wrap txt-xs muted">
            <span>{{ t('knowledge.unanswered.newcomers', c.newcomers) }}</span>
            <span class="dot-sep" />
            <span>{{
              t('knowledge.unanswered.last', { date: d(new Date(c.last_at), 'short') })
            }}</span>
            <template v-for="r in REASONS" :key="r">
              <template v-if="c.reasons[r]">
                <span class="dot-sep" />
                <span>{{ t(`knowledge.unanswered.reasons.${r}`) }}: {{ c.reasons[r] }}</span>
              </template>
            </template>
          </div>
          <div v-if="c.keywords.length > 0" class="row gap-1 wrap">
            <span v-for="k in c.keywords" :key="k" class="badge">{{ k }}</span>
          </div>
          <div v-if="c.examples.length > 0" class="kb-examples">
            <div class="txt-xs muted">{{ t('knowledge.unanswered.examples') }}</div>
            <ul>
              <li v-for="(ex, i) in c.examples" :key="i" class="txt-sm">«{{ ex }}»</li>
            </ul>
          </div>
        </div>
        <div class="kb-cluster__act">
          <Button size="sm" @click="emit('upload', c)">
            <Upload :size="14" aria-hidden="true" />{{ t('knowledge.unanswered.upload') }}
          </Button>
        </div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.kb-privacy {
  display: flex;
  align-items: center;
  gap: 8px;
}
.kb-clusters {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.kb-clusters.is-stale {
  opacity: 0.6;
}
.kb-cluster {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  padding: 14px 16px;
}
.kb-cluster__main {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.kb-cluster__title {
  overflow-wrap: anywhere;
}
.kb-cluster__act {
  flex-shrink: 0;
}
.kb-count {
  display: inline-grid;
  place-items: center;
  min-width: 28px;
  height: 24px;
  padding: 0 6px;
  border-radius: 999px;
  background: var(--warning-soft);
  color: var(--warning);
  font-weight: 700;
  font-size: var(--text-sm);
}
.kb-examples ul {
  margin: 2px 0 0;
  padding-left: 16px;
  color: var(--fg-muted);
}
@media (max-width: 720px) {
  .kb-cluster {
    flex-direction: column;
  }
}
</style>
