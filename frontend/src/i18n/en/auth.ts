import type { trAuth } from '../tr/auth'
import type { AreaStrings } from '../types'

export const enAuth: AreaStrings<typeof trAuth> = {
  'auth.email': 'Email',
  'auth.password': 'Password',
  'auth.showPassword': 'Show password',
  'auth.hidePassword': 'Hide password',
  'auth.passwordHint': 'At least 8 characters',
  'auth.tagline': 'Portfolio tracking for BIST, US stocks, TEFAS funds, crypto and gold',
  'auth.login.title': 'Log in',
  'auth.login.submit': 'Log in',
  'auth.login.noAccount': 'No account? Sign up',
  'auth.login.sessionExpired': 'Your session expired, log in again',
  'auth.login.forgotPassword':
    'If you forgot your password, you cannot reset it yourself. Contact the person running this app; they can reset it for you.',
  'auth.register.title': 'Sign up',
  'auth.register.submit': 'Sign up',
  'auth.register.hasAccount': 'Already have an account? Log in',
  'auth.register.done': 'Registration complete, log in',
  'auth.register.closed': 'Registration is closed, contact your administrator.',
  'setup.title': 'Setup',
  'setup.token': 'Setup token',
  'setup.tokenHint': 'The setup token is printed in the server log',
  'setup.existingHint':
    'If this email is already registered, that account becomes admin when its password is correct',
  'setup.passwordAgain': 'Password (again)',
  'setup.passwordMismatch': 'The passwords do not match.',
  'setup.submit': 'Create admin account',
  'setup.step': 'Step {step} / 2',
  'setup.configure.intro':
    'This step is optional, and every change is saved right away. US stock prices need the Finnhub and Tiingo keys. You can also add them later under Administration.',
  'setup.configure.skip': 'Skip for now',
  'setup.configure.finish': 'Finish',
  'auth.logout': 'Log out',
  'session.loading': 'Loading session...',
}
