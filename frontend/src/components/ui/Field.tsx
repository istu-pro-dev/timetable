import {
  cloneElement,
  isValidElement,
  useId,
  type InputHTMLAttributes,
  type ReactElement,
  type ReactNode,
  type SelectHTMLAttributes,
} from 'react'
import { cx } from './cx.ts'

const control =
  'block w-full rounded-md border border-border bg-surface px-3 py-1.5 text-sm text-fg placeholder:text-muted disabled:opacity-60 aria-invalid:border-danger'

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cx(control, 'h-9', className)} {...props} />
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cx(control, 'h-9', className)} {...props} />
}

interface ControlProps {
  id?: string
  'aria-invalid'?: boolean
  'aria-describedby'?: string
}

export interface FieldProps {
  label: ReactNode
  error?: string | undefined
  hint?: ReactNode
  className?: string
  /** A single form control; it receives id, aria-invalid and aria-describedby. */
  children: ReactElement<ControlProps>
}

/** A labelled form control with an optional hint and validation error. */
export function Field({ label, error, hint, className, children }: FieldProps) {
  const autoId = useId()
  const id = (isValidElement(children) && children.props.id) || autoId
  const hintId = hint ? `${id}-hint` : undefined
  const errorId = error ? `${id}-error` : undefined
  const describedBy = [hintId, errorId].filter(Boolean).join(' ') || undefined

  return (
    <div className={cx('flex flex-col gap-1', className)}>
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      {cloneElement(children, {
        id,
        'aria-invalid': error ? true : undefined,
        'aria-describedby': describedBy,
      })}
      {hint && (
        <p id={hintId} className="text-xs text-muted">
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} className="text-xs text-danger">
          {error}
        </p>
      )}
    </div>
  )
}
