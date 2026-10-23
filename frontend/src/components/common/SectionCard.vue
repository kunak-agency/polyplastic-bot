<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string
    desc?: string
    bodyPadding?: 'default' | 'none' | 'tight'
    fillHeight?: boolean
  }>(),
  { title: undefined, desc: undefined, bodyPadding: 'default', fillHeight: false },
)
</script>

<template>
  <div class="card" :style="fillHeight ? { height: '100%' } : undefined">
    <div
      v-if="title || desc || $slots.title || $slots.desc || $slots.headActions"
      class="card-head"
    >
      <div>
        <div v-if="title || $slots.title" class="card-title">
          <slot name="title">{{ title }}</slot>
        </div>
        <div v-if="desc || $slots.desc" class="card-desc">
          <slot name="desc">{{ desc }}</slot>
        </div>
      </div>
      <div v-if="$slots.headActions" class="row gap-2 center">
        <slot name="headActions" />
      </div>
    </div>
    <div
      class="card-body"
      :style="
        bodyPadding === 'none'
          ? { padding: 0 }
          : bodyPadding === 'tight'
            ? { padding: '12px 8px 4px' }
            : undefined
      "
    >
      <slot />
    </div>
    <div v-if="$slots.foot" class="card-foot">
      <slot name="foot" />
    </div>
  </div>
</template>
