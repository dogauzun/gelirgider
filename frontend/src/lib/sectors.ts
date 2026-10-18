// The 11 sector keys an admin can pin on an instrument, as the backend's
// sectorKeys (backend/internal/instrument/sector.go). Labels: sector.<key>.
export const SECTOR_KEYS = [
  'energy',
  'materials',
  'industrials',
  'consumer_discretionary',
  'consumer_staples',
  'health_care',
  'financials',
  'information_technology',
  'communication_services',
  'utilities',
  'real_estate',
] as const
