import { useNavigate } from 'react-router'
import { useLogout, useMe } from '../api/auth'
import { t } from '../i18n/strings'
import { Icon } from './ui/Icon'
import { Menu, MenuItem, MenuLink } from './ui/Menu'

// The signed-in user's menu, at the foot of the sidebar and in the small-screen
// top bar: the account page and the logout. Yönetim is the
// sidebar's own row, in the drawer below `lg`. `wide` is the
// sidebar's: the trigger fills its row and always shows the email, truncated,
// the drawer included; the top bar has room for the initial only below `sm`,
// and there the trigger keeps its size: the page title is the bar's only
// item that shrinks.
export function AccountMenu({ wide = false }: { wide?: boolean }) {
  const { data: me } = useMe()
  const navigate = useNavigate()
  const mutation = useLogout(() => navigate('/login', { replace: true }))

  if (!me) return null

  return (
    <Menu
      trigger={
        <button
          type="button"
          aria-label={t('account.menu')}
          className={`flex h-9 min-w-0 items-center gap-2 rounded-ctl px-1.5 text-[13px] text-ink-2 hover:bg-chip ${wide ? 'w-full' : 'shrink-0'}`}
        >
          <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[11px] font-semibold text-accent-ink">
            {me.email.slice(0, 1).toUpperCase()}
          </span>
          <span title={me.email} className={`min-w-0 truncate ${wide ? '' : 'hidden sm:inline'}`}>
            {me.email}
          </span>
          <Icon name="chevronDown" className="ml-auto shrink-0 text-muted" />
        </button>
      }
    >
      <MenuLink to="/account">{t('account.link')}</MenuLink>
      <MenuItem onSelect={() => mutation.mutate()}>{t('auth.logout')}</MenuItem>
    </Menu>
  )
}
