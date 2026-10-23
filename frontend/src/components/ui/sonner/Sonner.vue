<script lang="ts" setup>
import type { ToasterProps } from 'vue-sonner'
import { reactiveOmit } from '@vueuse/core'
import {
  CircleCheckIcon,
  InfoIcon,
  Loader2Icon,
  OctagonXIcon,
  TriangleAlertIcon,
  XIcon,
} from 'lucide-vue-next'
import { Toaster as Sonner } from 'vue-sonner'
// The library's own stylesheet, which is what fixes the toaster to a corner of
// the viewport. Without it the wrapper stays `position: static`, so every toast
// is laid out at the end of the document — below the fold, measured at y=900 in
// a 900 px window — and no message the app raises is ever seen. It was missing
// since the component was vendored: the classes above only theme a toast, they
// do not place it.
import 'vue-sonner/style.css'

const props = defineProps<ToasterProps>()
const delegatedProps = reactiveOmit(props, 'toastOptions')
</script>

<template>
  <Sonner
    class="toaster group"
    :toast-options="{
      classes: {
        toast:
          'group toast group-[.toaster]:bg-background group-[.toaster]:text-foreground group-[.toaster]:border-border group-[.toaster]:shadow-lg',
        description: 'group-[.toast]:text-muted-foreground',
        actionButton: 'group-[.toast]:bg-primary group-[.toast]:text-primary-foreground',
        cancelButton: 'group-[.toast]:bg-muted group-[.toast]:text-muted-foreground',
      },
    }"
    v-bind="delegatedProps"
  >
    <template #success-icon>
      <CircleCheckIcon class="size-4" />
    </template>
    <template #info-icon>
      <InfoIcon class="size-4" />
    </template>
    <template #warning-icon>
      <TriangleAlertIcon class="size-4" />
    </template>
    <template #error-icon>
      <OctagonXIcon class="size-4" />
    </template>
    <template #loading-icon>
      <div>
        <Loader2Icon class="size-4 animate-spin" />
      </div>
    </template>
    <template #close-icon>
      <XIcon class="size-4" />
    </template>
  </Sonner>
</template>
