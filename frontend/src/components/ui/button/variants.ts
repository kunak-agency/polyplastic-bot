import { cva, type VariantProps } from 'class-variance-authority'

// Maps to components.css .btn classes. We keep the design's look (defined in
// components.css) and use cva only to map variant/size props to class names.
export const buttonVariants = cva('btn', {
  variants: {
    variant: {
      primary: 'btn-primary',
      secondary: 'btn-secondary',
      ghost: 'btn-ghost',
      outline: 'btn-outline',
      danger: 'btn-danger',
    },
    size: {
      default: '',
      sm: 'btn-sm',
      lg: 'btn-lg',
      icon: 'btn-icon',
      'icon-sm': 'btn-icon btn-sm',
    },
  },
  defaultVariants: {
    variant: 'secondary',
    size: 'default',
  },
})

export type ButtonVariants = VariantProps<typeof buttonVariants>
