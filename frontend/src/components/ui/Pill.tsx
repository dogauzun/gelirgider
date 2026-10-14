import type { ReactNode } from 'react'

const tones = {
  buy: 'bg-accent-soft text-accent-ink',
  sell: 'bg-plum-soft text-plum',
  dividend: 'bg-gain-soft text-gain',
  cash: 'bg-chip text-ink-2',
  gain: 'bg-gain-soft text-gain',
  loss: 'bg-loss-soft text-loss',
  warn: 'bg-warn-soft text-warn',
}

type PillProps = {
  tone: keyof typeof tones
  children: ReactNode
}

export function Pill({ tone, children }: PillProps) {
  return (
    <span
      className={`inline-flex h-[22px] items-center rounded-full px-2 text-[12px] font-medium whitespace-nowrap ${tones[tone]}`}
    >
      {children}
    </span>
  )
}
