import type { VariantProps } from 'class-variance-authority'
import { cva } from 'class-variance-authority'

export { default as Badge } from './Badge.vue'

// Maps to components.css .badge classes (solid-success/warning/danger/info/accent).
export const badgeVariants = cva('badge', {
  variants: {
    variant: {
      default: '',
      success: 'solid-success',
      warning: 'solid-warning',
      danger: 'solid-danger',
      info: 'solid-info',
      accent: 'solid-accent',
    },
  },
  defaultVariants: {
    variant: 'default',
  },
})

export type BadgeVariants = VariantProps<typeof badgeVariants>
