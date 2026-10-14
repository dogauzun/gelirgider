import { SHARE_FORMAT } from '../../lib/allocation'
import { formatNumber } from '../../lib/format'

// A row's share as a short bar, scaled to the largest share, plus the percent.
export function WeightBar({ share, largest }: { share: number; largest: number }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span aria-hidden="true" className="h-1 w-8 overflow-hidden rounded-full bg-line">
        <span className="block h-full bg-accent" style={{ width: `${(share / largest) * 100}%` }} />
      </span>
      {formatNumber(share, SHARE_FORMAT)}
    </span>
  )
}
