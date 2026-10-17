import type { trAccount } from '../tr/account'
import type { AreaStrings } from '../types'

export const enAccount: AreaStrings<typeof trAccount> = {
  'account.link': 'Account',
  'account.export': 'Export transactions',
  'account.menu': 'Account menu',
  'account.heading': 'Account',
  'account.section.preferences': 'Preferences',
  'account.reportingHelp': 'The summary and value history are shown in this currency',
  'account.section.password': 'Password',
  'account.passwordHelp': 'Your other sessions are closed after the change',
  'account.section.language': 'Language',
  'account.languageHelp': 'The interface language is remembered in this browser',
  'account.section.appearance': 'Appearance',
  'account.appearanceHelp': 'System follows your operating system setting',
  'account.currentPassword': 'Current password',
  'account.newPassword': 'New password',
  'account.newPasswordAgain': 'New password (again)',
  'account.passwordMismatch': 'The new passwords do not match.',
  'account.submit': 'Change password',
  'account.success': 'Your password has been changed.',
  'account.reportingCurrency': 'Reporting currency',
  'account.reportingCurrency.saving': 'Saving...',
  'account.reportingCurrency.saved': 'Saved',
}
