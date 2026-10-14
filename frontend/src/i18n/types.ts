// An English area file, typed against its Turkish counterpart so `tsc -b`
// fails on a missing or extra key. A `<key>.one` entry is the singular form,
// picked by t() when `count` is 1; Turkish does not inflect a noun after a
// number, so the tr files have none.
export type AreaStrings<T> = Record<keyof T, string> &
  Partial<Record<`${keyof T & string}.one`, string>>
