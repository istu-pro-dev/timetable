import type { ButtonHTMLAttributes } from 'react'
import { cx } from './cx.ts'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md'

const variants: Record<ButtonVariant, string> = {
  primary: 'bg-primary text-primary-fg hover:opacity-90',
  secondary: 'border border-border bg-surface hover:bg-surface-2',
  ghost: 'hover:bg-surface-2',
  danger: 'bg-danger text-white hover:opacity-90',
}

const sizes: Record<ButtonSize, string> = {
  sm: 'h-8 px-2.5 text-sm gap-1.5',
  md: 'h-9 px-4 text-sm gap-2',
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
}

export function Button({
  variant = 'secondary',
  size = 'md',
  type = 'button',
  className,
  ...props
}: ButtonProps) {
  return (
    <button
      type={type}
      className={cx(
        'inline-flex cursor-pointer items-center justify-center rounded-md font-medium whitespace-nowrap transition-colors disabled:cursor-not-allowed disabled:opacity-50',
        variants[variant],
        sizes[size],
        className,
      )}
      {...props}
    />
  )
}
