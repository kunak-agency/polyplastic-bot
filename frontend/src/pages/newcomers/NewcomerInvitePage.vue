<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import QRCode from 'qrcode'
import { Copy, Download } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { apiGetNewcomer } from '@/api/newcomers'
import { useBreadcrumbTail } from '@/composables/useBreadcrumbs'
import { useCopy } from '@/composables/useClipboard'
import { errorMessage } from '@/lib/apiError'
import { isoToLocalDate } from '@/lib/calendar'
import type { Newcomer } from '@/types/api'

const { t, d } = useI18n()
const route = useRoute()
const copy = useCopy()
const { setBreadcrumbTail } = useBreadcrumbTail()

const newcomer = ref<Newcomer | null>(null)
const error = ref<unknown>(null)
const qr = ref<string | null>(null)
const qrFailed = ref(false)

const id = computed(() => String(route.params.id))

async function load(): Promise<void> {
  error.value = null
  newcomer.value = null
  try {
    newcomer.value = await apiGetNewcomer(id.value)
    setBreadcrumbTail(newcomer.value.full_name)
  } catch (e) {
    error.value = e
  }
}
watch(id, load, { immediate: true })

// The QR is drawn in the browser from the link: no image service, nothing
// leaves the page, and the CSP needs only the data: images it already allows.
watch(
  () => newcomer.value?.invite_link,
  async (link) => {
    qr.value = null
    qrFailed.value = false
    if (!link) return
    try {
      qr.value = await QRCode.toDataURL(link, { width: 480, margin: 1, errorCorrectionLevel: 'M' })
    } catch {
      qrFailed.value = true
    }
  },
)

// The name the letter greets: the first word after the surname when there is
// one ("Ivanov Petr" → "Petr"), otherwise the whole field.
const firstName = computed(() => {
  const parts = (newcomer.value?.full_name ?? '').split(/\s+/).filter(Boolean)
  return parts.length > 1 ? parts[1] : (parts[0] ?? '')
})

const letter = computed(() =>
  newcomer.value
    ? t('newcomers.invite.letterText', {
        name: firstName.value,
        date: d(isoToLocalDate(newcomer.value.start_date), 'day'),
        link: newcomer.value.invite_link,
      })
    : '',
)

const STATUS_VARIANT = { invited: 'warning', connected: 'success', stopped: 'default' } as const
</script>

<template>
  <PageContainer width="narrow">
    <LoadingState v-if="!newcomer && !error" variant="card" :rows="2" />
    <ErrorState v-else-if="error" :message="errorMessage(error, 'newcomers.invite.loadFailed')">
      <template #retry>
        <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
      </template>
    </ErrorState>
    <template v-else-if="newcomer">
      <PageHead :title="t('newcomers.invite.title')">
        <template #subtitle>{{ t('newcomers.invite.subtitle') }}</template>
        <template #actions>
          <Button as-child>
            <RouterLink :to="{ name: 'newcomer', params: { id: newcomer.id } }">
              {{ t('newcomers.actions.toCard') }}
            </RouterLink>
          </Button>
        </template>
      </PageHead>

      <SectionCard>
        <template #title>{{ newcomer.full_name }}</template>
        <template #desc>
          {{ newcomer.position }} · {{ t(`common.site.${newcomer.site}`) }} ·
          {{ d(isoToLocalDate(newcomer.start_date), 'short') }}
        </template>
        <template #headActions>
          <Badge :variant="STATUS_VARIANT[newcomer.connection]">
            {{ t(`newcomers.invite.status.${newcomer.connection}`) }}
          </Badge>
        </template>

        <div class="invite">
          <div class="invite__qr">
            <div class="field-label">{{ t('newcomers.invite.qr') }}</div>
            <img v-if="qr" :src="qr" :alt="t('newcomers.invite.qrAlt')" width="200" height="200" />
            <div v-else-if="qrFailed" class="muted txt-sm">
              {{ t('newcomers.invite.qrFailed') }}
            </div>
            <div v-else class="invite__qr-placeholder shimmer" />
            <Button v-if="qr" as-child size="sm" variant="ghost">
              <a :href="qr" download="invite-qr.png">
                <Download :size="14" />{{ t('newcomers.invite.qrDownload') }}
              </a>
            </Button>
          </div>

          <div class="invite__main">
            <div>
              <label class="field-label" for="invite-link">{{ t('newcomers.invite.link') }}</label>
              <div class="row gap-2">
                <input
                  id="invite-link"
                  class="input mono"
                  readonly
                  :value="newcomer.invite_link"
                  @focus="($event.target as HTMLInputElement).select()"
                />
                <Button @click="copy(newcomer.invite_link, 'newcomers.invite.linkCopied')">
                  <Copy :size="14" />{{ t('common.actions.copy') }}
                </Button>
              </div>
            </div>
            <div>
              <div class="row between center">
                <label class="field-label" for="invite-letter">{{
                  t('newcomers.invite.letter')
                }}</label>
                <Button size="sm" @click="copy(letter, 'newcomers.invite.letterCopied')">
                  <Copy :size="14" />{{ t('common.actions.copy') }}
                </Button>
              </div>
              <textarea
                id="invite-letter"
                class="textarea invite__letter"
                readonly
                :value="letter"
              />
            </div>
          </div>
        </div>
      </SectionCard>
    </template>
  </PageContainer>
</template>

<style scoped>
.invite {
  display: grid;
  grid-template-columns: 200px 1fr;
  gap: 24px;
}
.invite__qr {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
.invite__qr img {
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: #fff;
}
.invite__qr-placeholder {
  width: 200px;
  height: 200px;
  border-radius: var(--radius-md);
}
.invite__main {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}
.invite__letter {
  height: 220px;
  padding: 10px;
  margin-top: 6px;
  line-height: 1.5;
  resize: vertical;
}
@media (max-width: 720px) {
  .invite {
    grid-template-columns: 1fr;
  }
}
</style>
