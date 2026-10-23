<script setup lang="ts">
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

// Modal on reka-ui Dialog (focus trap, Esc, aria) dressed in the .modal classes
// of components.css. The content sits inside the overlay so a tall dialog
// scrolls with the page instead of being cut off.
withDefaults(
  defineProps<{
    title: string
    description?: string
    width?: 'default' | 'wide'
  }>(),
  { description: undefined, width: 'default' },
)

const open = defineModel<boolean>('open', { required: true })
const { t } = useI18n()
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="modal-backdrop app-dialog__overlay">
        <DialogContent :class="['modal', 'app-dialog', width === 'wide' && 'app-dialog--wide']">
          <div class="modal-head">
            <div>
              <DialogTitle class="modal-title">{{ title }}</DialogTitle>
              <DialogDescription v-if="description" class="app-dialog__desc">
                {{ description }}
              </DialogDescription>
            </div>
            <DialogClose
              class="btn btn-ghost btn-icon btn-sm"
              :aria-label="t('common.actions.close')"
            >
              <X :size="16" />
            </DialogClose>
          </div>
          <div class="modal-body">
            <slot />
          </div>
          <div v-if="$slots.foot" class="modal-foot">
            <slot name="foot" />
          </div>
        </DialogContent>
      </DialogOverlay>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
.app-dialog__overlay {
  overflow-y: auto;
  padding: 32px 16px;
}
.app-dialog {
  max-width: 100%;
}
.app-dialog--wide {
  width: 1040px;
}
.app-dialog__desc {
  margin: 0;
  font-size: var(--text-base);
  color: var(--fg-subtle);
}
</style>
