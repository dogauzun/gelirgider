import { useState, type ChangeEvent, type FormEvent } from 'react'
import { t, type StringKey } from '../i18n/strings'
import {
  compareDecimal,
  isAmbiguousThousands,
  mulDecimal,
  parseDecimalInput,
  tooManyDigits,
} from '../lib/decimal'
import { toast } from '../lib/toast'
import { useOneAtATime } from '../lib/useOneAtATime'

// What the open and renew dialogs share: the decimal fields and the submit.

// How a decimal field is checked. `fraction` is the digits after the point it
// may have: percent fields keep 7, so the fraction sent has the server's 9.
type Rule = {
  label: StringKey
  fraction: number
  ok: (parsed: string) => boolean
  errorKey: StringKey
}

const positive = (parsed: string) => compareDecimal(parsed, '0') > 0

export const RULES: Record<'principal' | 'rate' | 'stopaj', Rule> = {
  principal: {
    label: 'deposits.principal',
    fraction: 9,
    ok: positive,
    errorKey: 'deposits.error.principal',
  },
  rate: {
    label: 'deposits.rate',
    fraction: 7,
    // The server takes a fraction of at most 10, which is 1000 %.
    ok: (parsed) => positive(parsed) && compareDecimal(parsed, '1000') <= 0,
    errorKey: 'deposits.error.rate',
  },
  stopaj: {
    label: 'deposits.stopaj',
    fraction: 7,
    ok: (parsed) => compareDecimal(parsed, '100') < 0,
    errorKey: 'deposits.error.stopaj',
  },
}

// The message for a typed decimal that is not an allowed value, else null.
export function decimalError(text: string, rule: Rule): string | null {
  if (isAmbiguousThousands(text)) return t('decimal.error.ambiguousThousands')
  const parsed = parseDecimalInput(text)
  if (parsed === null || !rule.ok(parsed)) return t(rule.errorKey)
  if (tooManyDigits(parsed, rule.fraction)) {
    return t('decimal.error.tooManyDigits', { field: t(rule.label), fraction: rule.fraction })
  }
  return null
}

// Rate and stopaj are typed as percents and sent as fractions: "45" -> "0.45",
// a decimal shift on the string, never through a float.
export const fraction = (percent: string) => mulDecimal(parseDecimalInput(percent) ?? '0', '0.01')

// `field` null is a server error, shown under the form.
export type FormError<F extends string> = { field: F | null; message: string }

// The first failed check of a form, or null.
export function firstError<F extends string>(checks: [F, string | null][]): FormError<F> | null {
  const failed = checks.find(([, message]) => message !== null)
  return failed ? { field: failed[0], message: failed[1] as string } : null
}

// The value and onChange of a text field of `form`.
export function textBinding<T extends Record<string, string | boolean>>(
  form: T,
  setForm: (next: T) => void,
) {
  return (key: keyof T & string) => ({
    value: form[key] as string,
    onChange: (event: ChangeEvent<HTMLInputElement>) =>
      setForm({ ...form, [key]: event.target.value }),
  })
}

// The submit of the open and renew dialogs: the form's error, and a submit that
// validates, posts once and closes with a toast.
export function useDepositSubmit<F extends string, Input>({
  mutation,
  validate,
  toInput,
  doneKey,
  onClose,
}: {
  mutation: {
    isPending: boolean
    mutateAsync: (
      input: Input,
      options: { onSuccess: () => void; onError: (error: Error) => void },
    ) => Promise<unknown>
  }
  validate: () => FormError<F> | null
  toInput: () => Input
  doneKey: StringKey
  onClose: () => void
}) {
  const [error, setError] = useState<FormError<F> | null>(null)
  const once = useOneAtATime()
  const fieldError = (field: F) => (error?.field === field ? error.message : undefined)

  function submit(event: FormEvent) {
    event.preventDefault()
    // Enter in a field comes straight here: a second press while the save
    // runs would post twice.
    if (mutation.isPending) return
    const invalid = validate()
    setError(invalid)
    if (invalid) return
    once(() =>
      mutation.mutateAsync(toInput(), {
        onSuccess: () => {
          toast(t(doneKey))
          onClose()
        },
        onError: (mutationError) => setError({ field: null, message: mutationError.message }),
      }),
    )
  }

  return { error, setError, fieldError, submit }
}
