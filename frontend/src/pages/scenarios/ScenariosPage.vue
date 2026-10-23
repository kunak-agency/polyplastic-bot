<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { Route, FileSpreadsheet } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import ScenarioStatus from '@/components/scenarios/ScenarioStatus.vue'
import { Button } from '@/components/ui/button'
import { apiImportScenario } from '@/api/scenarios'
import { useScenariosStore } from '@/stores/scenarios'
import { useSiteStore } from '@/stores/site'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'

const { t, d } = useI18n()
const router = useRouter()
const toast = useToast()
const store = useScenariosStore()
const { items, loaded, loading, error } = storeToRefs(store)
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)

onMounted(() => void store.load())

// A scenario without sites applies everywhere, so it stays under any filter.
const rows = computed(() =>
  items.value.filter(
    (s) =>
      currentSite.value === 'all' || s.sites.length === 0 || s.sites.includes(currentSite.value),
  ),
)

const fileInput = ref<HTMLInputElement | null>(null)
const importing = ref(false)

async function onFile(e: Event): Promise<void> {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  importing.value = true
  try {
    const created = await apiImportScenario(file)
    toast.success(t('scenarios.import.done'))
    void router.push({ name: 'scenario', params: { id: created.id } })
  } catch (err) {
    toast.error(errorMessage(err, 'scenarios.import.failed'))
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <PageContainer>
    <PageHead :title="t('scenarios.title')">
      <template #subtitle>{{ t('scenarios.subtitle') }}</template>
      <template #actions>
        <input
          ref="fileInput"
          type="file"
          class="sr-only"
          accept=".xlsx,.xls,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.ms-excel"
          tabindex="-1"
          aria-hidden="true"
          @change="onFile"
        />
        <Button
          :disabled="importing"
          :title="t('scenarios.import.hint')"
          @click="fileInput?.click()"
        >
          <FileSpreadsheet :size="14" />
          {{ importing ? t('scenarios.import.uploading') : t('scenarios.import.button') }}
        </Button>
      </template>
    </PageHead>

    <SectionCard body-padding="none">
      <LoadingState v-if="loading && !loaded" variant="table" :rows="3" />
      <ErrorState
        v-else-if="error && !loaded"
        :message="errorMessage(error, 'scenarios.loadFailed')"
      >
        <template #retry>
          <Button size="sm" @click="store.load()">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
      <EmptyState
        v-else-if="items.length === 0"
        :icon="Route"
        :title="t('scenarios.empty.title')"
        :subtitle="t('scenarios.empty.description')"
      />
      <EmptyState v-else-if="rows.length === 0" :icon="Route" :title="t('scenarios.noMatches')" />
      <table v-else class="tbl">
        <thead>
          <tr>
            <th>{{ t('scenarios.columns.name') }}</th>
            <th>{{ t('scenarios.columns.sites') }}</th>
            <th>{{ t('scenarios.columns.status') }}</th>
            <th class="td-num">{{ t('scenarios.columns.active') }}</th>
            <th>{{ t('scenarios.columns.updated') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in rows" :key="s.id">
            <td>
              <RouterLink class="cell-link" :to="{ name: 'scenario', params: { id: s.id } }">
                {{ s.name }}
              </RouterLink>
            </td>
            <td>
              <div class="row gap-1">
                <span v-for="code in s.sites" :key="code" class="badge">
                  {{ t(`common.site.${code}`) }}
                </span>
                <span v-if="s.sites.length === 0" class="badge">{{ t('scenarios.allSites') }}</span>
              </div>
            </td>
            <td><ScenarioStatus :scenario="s" /></td>
            <td class="td-num tabular">{{ s.active_newcomers }}</td>
            <td class="tabular muted">{{ d(new Date(s.updated_at), 'long') }}</td>
          </tr>
        </tbody>
      </table>
    </SectionCard>
  </PageContainer>
</template>

<style scoped>
.cell-link {
  color: var(--fg);
  font-weight: 500;
}
.cell-link:hover {
  text-decoration: underline;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
</style>
