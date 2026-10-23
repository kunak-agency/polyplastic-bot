<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppDialog from '@/components/common/AppDialog.vue'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import { apiCreateScenario } from '@/api/scenarios'
import { SITES } from '@/stores/site'
import { errorMessage, isApiError } from '@/lib/apiError'
import { useToast } from '@/composables/useToast'
import type { Scenario, ScenarioDetail, SiteCode } from '@/types/api'

// "Create scenario" (ux-spec 3.5): a draft from scratch or a copy of an
// existing scenario's last published version. The server answers 409
// scenario_name_taken for a name in use; it is shown under the name field.
const props = defineProps<{ scenarios: Scenario[] }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ created: [scenario: ScenarioDetail] }>()
const { t } = useI18n()
const toast = useToast()

type Start = 'blank' | 'copy'

const name = ref('')
const sites = ref<SiteCode[]>([])
const start = ref<Start>('blank')
const sourceId = ref('')
const touched = ref(false)
const busy = ref(false)
const nameTaken = ref<string | null>(null)

const source = computed(() => props.scenarios.find((s) => s.id === sourceId.value) ?? null)

watch(open, (v) => {
  if (!v) return
  name.value = ''
  sites.value = []
  start.value = 'blank'
  sourceId.value = props.scenarios.find((s) => s.version > 0)?.id ?? props.scenarios[0]?.id ?? ''
  touched.value = false
  nameTaken.value = null
})

// A copy usually serves the same sites: take them over, HR can change them.
watch([start, sourceId], () => {
  if (start.value === 'copy' && source.value) sites.value = [...source.value.sites]
})

watch(name, () => {
  nameTaken.value = null
})

const nameError = computed(() => {
  if (nameTaken.value) return nameTaken.value
  if (touched.value && !name.value.trim()) return t('scenarios.create.nameRequired')
  return undefined
})

function sourceLabel(s: Scenario): string {
  return s.version > 0
    ? t('scenarios.create.sourcePublished', { name: s.name, version: s.version })
    : t('scenarios.create.sourceDraft', { name: s.name })
}

async function submit(): Promise<void> {
  touched.value = true
  if (!name.value.trim()) return
  busy.value = true
  try {
    const created = await apiCreateScenario({
      name: name.value.trim(),
      sites: sites.value,
      from_scenario_id: start.value === 'copy' && sourceId.value ? sourceId.value : null,
    })
    toast.success(t('scenarios.create.done'))
    open.value = false
    emit('created', created)
  } catch (e) {
    if (isApiError(e) && e.code === 'scenario_name_taken') {
      nameTaken.value = errorMessage(e)
    } else {
      toast.error(errorMessage(e, 'scenarios.create.failed'))
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppDialog
    v-model:open="open"
    :title="t('scenarios.create.title')"
    :description="t('scenarios.create.description')"
  >
    <form id="create-scenario-form" class="cs" novalidate @submit.prevent="submit">
      <FormField :label="t('scenarios.create.name')" html-for="cs-name" required :error="nameError">
        <input
          id="cs-name"
          v-model="name"
          class="input"
          :class="{ err: nameError }"
          autocomplete="off"
          :placeholder="t('scenarios.create.namePlaceholder')"
        />
      </FormField>
      <FormField :label="t('scenarios.create.sites')" :hint="t('scenarios.create.sitesHint')">
        <div class="cs__row" role="group" :aria-label="t('scenarios.create.sites')">
          <label v-for="s in SITES" :key="s" class="cs__check">
            <input v-model="sites" type="checkbox" :value="s" />
            {{ t(`common.site.${s}`) }}
          </label>
        </div>
      </FormField>
      <FormField :label="t('scenarios.create.start')">
        <div class="cs__col" role="radiogroup" :aria-label="t('scenarios.create.start')">
          <label class="cs__check">
            <input v-model="start" type="radio" name="cs-start" value="blank" />
            {{ t('scenarios.create.startBlank') }}
          </label>
          <label class="cs__check">
            <input
              v-model="start"
              type="radio"
              name="cs-start"
              value="copy"
              :disabled="scenarios.length === 0"
            />
            {{ t('scenarios.create.startCopy') }}
          </label>
          <select
            v-if="start === 'copy'"
            id="cs-source"
            v-model="sourceId"
            class="input cs__source"
            :aria-label="t('scenarios.create.source')"
          >
            <option v-for="s in scenarios" :key="s.id" :value="s.id">{{ sourceLabel(s) }}</option>
          </select>
          <span v-if="start === 'copy'" class="field-hint">{{
            t('scenarios.create.copyHint')
          }}</span>
        </div>
      </FormField>
    </form>
    <template #foot>
      <Button type="button" @click="open = false">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" form="create-scenario-form" variant="primary" :disabled="busy">
        {{ busy ? t('scenarios.create.submitting') : t('scenarios.create.submit') }}
      </Button>
    </template>
  </AppDialog>
</template>

<style scoped>
.cs {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.cs__row {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  min-height: var(--h-input);
  align-items: center;
}
.cs__col {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.cs__check {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
.cs__source {
  margin-left: 22px;
  width: calc(100% - 22px);
}
.cs__col .field-hint {
  margin-left: 22px;
}
</style>
