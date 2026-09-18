// Shared design-system primitives for VoltPanel's control-plane UI.
// Every page builds its layout out of these instead of hand-rolled
// `border p-3` divs, so spacing/color/radius stay consistent across the
// app without a heavier component library.
import React from 'react'

export function cn(...parts: Array<string | false | null | undefined>) {
  return parts.filter(Boolean).join(' ')
}

// ---------------------------------------------------------------------------
// Buttons

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
type ButtonSize = 'sm' | 'md'

const buttonVariants: Record<ButtonVariant, string> = {
  primary: 'bg-slate-900 text-white hover:bg-slate-800 active:bg-slate-950 disabled:bg-slate-300',
  secondary: 'bg-white text-slate-700 border border-slate-200 hover:bg-slate-50 active:bg-slate-100 disabled:text-slate-400',
  ghost: 'bg-transparent text-slate-600 hover:bg-slate-100 active:bg-slate-200 disabled:text-slate-300',
  danger: 'bg-white text-red-600 border border-red-200 hover:bg-red-50 active:bg-red-100 disabled:text-red-300',
}

const buttonSizes: Record<ButtonSize, string> = {
  sm: 'px-2.5 py-1 text-xs gap-1.5',
  md: 'px-3.5 py-1.5 text-sm gap-2',
}

export function Button({
  variant = 'secondary',
  size = 'md',
  className,
  children,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: ButtonSize }) {
  return (
    <button
      className={cn(
        'inline-flex items-center justify-center rounded-md font-medium transition-colors',
        'disabled:cursor-not-allowed disabled:opacity-70',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-volt-500 focus-visible:ring-offset-1',
        buttonVariants[variant],
        buttonSizes[size],
        className,
      )}
      {...props}
    >
      {children}
    </button>
  )
}

export function IconButton({
  className,
  title,
  children,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      title={title}
      className={cn(
        'inline-flex items-center justify-center rounded-md p-1.5 text-slate-500',
        'hover:bg-slate-100 hover:text-slate-700 transition-colors',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-volt-500',
        'disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    >
      {children}
    </button>
  )
}

// ---------------------------------------------------------------------------
// Layout

export function Card({
  title,
  description,
  actions,
  className,
  bodyClassName,
  children,
}: {
  title?: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  className?: string
  bodyClassName?: string
  children?: React.ReactNode
}) {
  return (
    <div className={cn('rounded-xl border border-slate-200 bg-white shadow-card', className)}>
      {(title || actions) && (
        <div className="flex items-start justify-between gap-3 border-b border-slate-100 px-4 py-3">
          <div>
            {title && <div className="text-sm font-semibold text-slate-900">{title}</div>}
            {description && <div className="mt-0.5 text-xs text-slate-500">{description}</div>}
          </div>
          {actions && <div className="flex items-center gap-2">{actions}</div>}
        </div>
      )}
      <div className={cn('p-4', bodyClassName)}>{children}</div>
    </div>
  )
}

export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: string
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
}) {
  return (
    <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
      <div>
        {eyebrow && (
          <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-volt-600">{eyebrow}</div>
        )}
        <h1 className="text-xl font-semibold text-slate-900">{title}</h1>
        {description && <p className="mt-1 max-w-2xl text-sm text-slate-500">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Status system
//
// Every domain in the backend speaks its own status vocabulary (service:
// running/stopped/failed, runtime: installed/installing/missing, docker:
// running/exited/..., pipeline: success/failed, ...). Rather than one
// brittle string-sniffing mapper, each call site picks the semantic
// `tone` explicitly and this renders it consistently.

export type Tone = 'success' | 'error' | 'warning' | 'neutral' | 'info'

const toneDot: Record<Tone, string> = {
  success: 'bg-emerald-500',
  error: 'bg-red-500',
  warning: 'bg-amber-500',
  neutral: 'bg-slate-300',
  info: 'bg-blue-500',
}

const toneText: Record<Tone, string> = {
  success: 'text-emerald-700',
  error: 'text-red-700',
  warning: 'text-amber-700',
  neutral: 'text-slate-500',
  info: 'text-blue-700',
}

const toneBg: Record<Tone, string> = {
  success: 'bg-emerald-50 text-emerald-700 ring-emerald-600/20',
  error: 'bg-red-50 text-red-700 ring-red-600/20',
  warning: 'bg-amber-50 text-amber-700 ring-amber-600/20',
  neutral: 'bg-slate-100 text-slate-600 ring-slate-500/20',
  info: 'bg-blue-50 text-blue-700 ring-blue-600/20',
}

/** Inline "● label" status indicator -- for table cells and card meta rows. */
export function StatusDot({ tone, label }: { tone: Tone; label: React.ReactNode }) {
  return (
    <span className={cn('inline-flex items-center gap-1.5 text-sm font-medium', toneText[tone])}>
      <span className={cn('h-1.5 w-1.5 rounded-full', toneDot[tone])} />
      {label}
    </span>
  )
}

/** Pill badge -- for compact table columns and card corners. */
export function Badge({ tone = 'neutral', children }: { tone?: Tone; children: React.ReactNode }) {
  return (
    <span className={cn('inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset', toneBg[tone])}>
      {children}
    </span>
  )
}

// ---------------------------------------------------------------------------
// Empty / loading / error states

export function EmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon?: React.ReactNode
  title: React.ReactNode
  description?: React.ReactNode
  action?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-slate-200 px-6 py-12 text-center">
      {icon && <div className="mb-1 text-slate-300">{icon}</div>}
      <div className="text-sm font-medium text-slate-700">{title}</div>
      {description && <div className="max-w-sm text-sm text-slate-500">{description}</div>}
      {action && <div className="mt-3">{action}</div>}
    </div>
  )
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn('animate-pulse rounded-md bg-slate-100', className)} />
}

export function ErrorNote({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 ring-1 ring-inset ring-red-600/20">
      {children}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Meters (CPU/mem/disk-style progress bars)

export function Meter({ percent, tone = 'info' }: { percent: number; tone?: Tone }) {
  const clamped = Math.max(0, Math.min(100, percent))
  const barTone: Record<Tone, string> = {
    success: 'bg-emerald-500',
    error: 'bg-red-500',
    warning: 'bg-amber-500',
    neutral: 'bg-slate-400',
    info: 'bg-slate-900',
  }
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-100">
      <div className={cn('h-full rounded-full transition-all', barTone[tone])} style={{ width: `${clamped}%` }} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Tables -- a horizontally-scrollable wrapper plus consistent cell padding.

export function Table({ children }: { children: React.ReactNode }) {
  return (
    <div className="volt-scroll -mx-4 overflow-x-auto px-4">
      <table className="w-full min-w-[640px] text-sm">{children}</table>
    </div>
  )
}

export function Th({ children, className, ...props }: React.ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th
      className={cn('border-b border-slate-100 px-3 py-2 text-left text-xs font-medium uppercase tracking-wide text-slate-400', className)}
      {...props}
    >
      {children}
    </th>
  )
}

export function Td({ children, className, ...props }: React.TdHTMLAttributes<HTMLTableCellElement>) {
  return (
    <td className={cn('border-b border-slate-50 px-3 py-2.5 align-middle text-slate-700', className)} {...props}>
      {children}
    </td>
  )
}

// ---------------------------------------------------------------------------
// Form controls

export function Input({ className, ...props }: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        'rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-sm text-slate-800 placeholder:text-slate-400',
        'focus:outline-none focus:ring-2 focus:ring-volt-500/40 focus:border-volt-400',
        className,
      )}
      {...props}
    />
  )
}

export function Select({ className, children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn(
        'rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-sm text-slate-800',
        'focus:outline-none focus:ring-2 focus:ring-volt-500/40 focus:border-volt-400',
        className,
      )}
      {...props}
    >
      {children}
    </select>
  )
}

export function Textarea({ className, ...props }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={cn(
        'rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-sm text-slate-800 placeholder:text-slate-400',
        'focus:outline-none focus:ring-2 focus:ring-volt-500/40 focus:border-volt-400',
        className,
      )}
      {...props}
    />
  )
}

export function Label({ children }: { children: React.ReactNode }) {
  return <label className="mb-1 block text-xs font-medium text-slate-500">{children}</label>
}
