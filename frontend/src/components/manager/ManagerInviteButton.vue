<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Copy, UserPlus } from 'lucide-vue-next'
import AppDialog from '@/components/common/AppDialog.vue'
import { Button } from '@/components/ui/button'
import { apiManagerInvite } from '@/api/manager'
import { useCopy } from '@/composables/useClipboard'
import { errorMessage } from '@/lib/apiError'
import type { ManagerInvite } from '@/types/api'

// "Invite the head of unit" on the newcomer card (case role 03): a deep link
// ?start=mgr_<token>; the bot binds the head to every newcomer with this
// manager_name and starts sending 1:1 nudges.
const props = defineProps<{ newcomerId: string }>()
const { t, d } = useI18n()
const copy = useCopy()

const open = ref(false)
const invite = ref<ManagerInvite | null>(null)
const failure = ref<string | null>(null)
const busy = ref(false)

async function show(): Promise<void> {
  open.value = true
  busy.value = true
  failure.value = null
  try {
    invite.value = await apiManagerInvite(props.newcomerId)
  } catch (e) {
    failure.value = errorMessage(e, 'manager.invite.failed')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Button @click="show"><UserPlus :size="14" />{{ t('manager.invite.button') }}</Button>
  <AppDialog
    v-model:open="open"
    :title="t('manager.invite.title')"
    :description="t('manager.invite.desc')"
  >
    <p v-if="busy" class="muted">…</p>
    <p v-else-if="failure" class="danger">{{ failure }}</p>
    <div v-else-if="invite" class="inv">
      <div class="fw-semi">{{ t('manager.invite.manager', { name: invite.manager_name }) }}</div>
      <p v-if="invite.connected" class="txt-sm ok">{{ t('manager.invite.connected') }}</p>
      <code class="inv__link">{{ invite.link }}</code>
      <p class="txt-sm muted">
        {{ t('manager.invite.expires', { date: d(new Date(invite.expires_at), 'short') }) }}
      </p>
      <div class="row gap-2">
        <Button variant="primary" size="sm" @click="copy(invite.link)">
          <Copy :size="14" />{{ t('manager.invite.copy') }}
        </Button>
        <Button as-child variant="outline" size="sm">
          <RouterLink :to="{ name: 'manager', query: { name: invite.manager_name } }">
            {{ t('manager.invite.open') }}
          </RouterLink>
        </Button>
      </div>
    </div>
  </AppDialog>
</template>

<style scoped>
.inv {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.inv__link {
  display: block;
  padding: 8px 10px;
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
  word-break: break-all;
  font-size: var(--text-sm);
}
.ok {
  color: var(--success);
}
.danger {
  color: var(--danger);
}
</style>
