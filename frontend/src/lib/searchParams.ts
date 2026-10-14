// A copy of `prev` with each key set to its value, or removed when the value
// is undefined or empty.
export function patchSearchParams(
  prev: URLSearchParams,
  changes: Record<string, string | undefined>,
): URLSearchParams {
  const next = new URLSearchParams(prev)
  for (const [key, value] of Object.entries(changes)) {
    if (value) next.set(key, value)
    else next.delete(key)
  }
  return next
}
