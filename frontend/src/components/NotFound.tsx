import { Link } from 'react-router'
import { buttonClass } from './ui/buttonClass'

// The 404 page's heading, explanation and way back, also used where a
// portfolio or position in the URL does not exist (GLG-814).
export function NotFound({
  heading,
  explanation,
  to,
  linkLabel,
}: {
  heading: string
  explanation: string
  to: string
  linkLabel: string
}) {
  return (
    <div className="flex flex-col items-center gap-3 px-6 py-16 text-center">
      <h1 className="text-[24px] font-semibold text-ink">{heading}</h1>
      <p className="max-w-sm text-[14px] text-muted">{explanation}</p>
      <Link to={to} className={buttonClass({ className: 'mt-2' })}>
        {linkLabel}
      </Link>
    </div>
  )
}
