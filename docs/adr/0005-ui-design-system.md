# ADR 0005: UI design system ("Defter") — semantic tokens, fonts, theming, native dialogs

Status: superseded by [ADR 0023](0023-ui-design-system-v2.md) (2026-10-04).

Defter was the first UI design system: a flat look with a warm paper ground,
white cards with hairline borders, an ink primary, one blue accent, IBM Plex
Sans and Mono, semantic color tokens with a light and a dark value, and native
`<dialog>` elements for dialogs. It had no component library and no charting
library.

[ADR 0023](0023-ui-design-system-v2.md) replaced the look with UI v2 ("soft
depth") and lists, under "Rules that survive from ADR 0005", the rules that
still hold: semantic tokens only, the explicit P/L sign, locale number
formats, the cash-flow sign, native dialogs, no sideways page scroll, the
theme choice in `localStorage` and inline SVG icons. Read that ADR for the
current design system.
