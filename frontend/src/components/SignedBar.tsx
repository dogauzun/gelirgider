// A signed bar on a shared zero line: a gain to the right and a loss to the
// left, `fraction` (0 to 1) of the half track. Decoration only, the amount
// next to it is the content; the track is a grid cell, so the zero lines of
// a list line up.
export function SignedBar({ negative, fraction }: { negative: boolean; fraction: number }) {
  return (
    <span aria-hidden="true" className="relative h-3.5">
      <span className="absolute -inset-y-0.5 left-1/2 w-px bg-line-strong" />
      <span
        data-bar
        className={`absolute inset-y-0 rounded-[3px] ${
          negative ? 'right-1/2 bg-loss' : 'left-1/2 bg-gain'
        }`}
        style={{ width: `${fraction * 50}%` }}
      />
    </span>
  )
}
