// The class string of an <input> or a Select trigger. It lives apart from
// Field so the component file exports only components (react-refresh).

// Height, width, text colour and padding are picked here, not appended by the
// caller: two classes for one property conflict, and the generated stylesheet
// picks the winner, not the class order.
const sizes = {
  sm: 'h-9', // a filter in a toolbar
  md: 'h-10',
  lg: 'h-11', // the sign-in and setup forms
}

const widths = {
  full: 'w-full',
  auto: 'w-auto', // sized by its text
  number: 'w-24', // a short number: the browser's default text-input width is 214 px
}

// For <input> and Select call sites. The error look rides on `aria-invalid`,
// which Field sets for them, so there is one class list and no error variant.
// The aria-invalid focus pair outranks the plain aria-invalid ring, so a focused
// invalid field still shows one 2px edge, in loss instead of accent.
// The resting border is `faint`, the 3:1 a control's boundary needs (WCAG
// 1.4.11); `line-strong` drew it at 1.5:1.
// `readOnly` mutes the text of a field the user cannot edit; `trailingIcon`
// leaves room on the right for an icon button laid over the field.
export function inputClass({
  size = 'md',
  width = 'full',
  readOnly = false,
  trailingIcon = false,
}: {
  size?: keyof typeof sizes
  width?: keyof typeof widths
  readOnly?: boolean
  trailingIcon?: boolean
} = {}) {
  return `${sizes[size]} ${widths[width]} rounded-ctl border border-faint bg-surface ${trailingIcon ? 'pr-11 pl-3' : 'px-3'} ${readOnly ? 'text-muted' : 'text-ink'} placeholder:text-faint focus-within:border-accent focus-within:ring-1 focus-within:ring-accent focus:outline-none aria-invalid:border-loss aria-invalid:ring-3 aria-invalid:ring-loss-soft aria-invalid:focus-within:ring-1 aria-invalid:focus-within:ring-loss`
}
