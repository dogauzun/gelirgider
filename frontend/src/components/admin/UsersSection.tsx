import { useId, useState } from 'react'
import type { ReactNode } from 'react'
import { useNavigate } from 'react-router'
import { useMe } from '../../api/auth'
import {
  useAdminUsers,
  useCreateUser,
  useDeleteUser,
  useResetPassword,
  useToggleAdmin,
  type AdminUser,
} from '../../api/users'
import { formatMarketDate } from '../../lib/date'
import { useIsDesktop } from '../../lib/useIsDesktop'
import { getLocale, locales, t, type Locale } from '../../i18n/strings'
import { toast } from '../../lib/toast'
import { focusIfLost, neighbourOf } from '../../lib/useFocusLater'
import { useOneAtATime } from '../../lib/useOneAtATime'
import { ActionsTh } from '../ui/ActionsTh'
import { Button } from '../ui/Button'
import { ConfirmDialog } from '../ui/ConfirmDialog'
import { useConfirm } from '../ui/useConfirm'
import { Field } from '../ui/Field'
import { inputClass } from '../ui/inputClass'
import { Icon } from '../ui/Icon'
import { Pill } from '../ui/Pill'
import { Select } from '../ui/Select'
import { ROW_44, TABLE, TD, TH } from '../ui/table'
import { useModal } from '../ui/useModal'
import { Modal } from '../ui/Modal'
import { sheetPanel } from '../ui/sheetPanel'
import { RegistrationToggle } from './RegistrationToggle'

// created_at is an instant; show the Istanbul calendar day it fell on.
function createdDay(user: AdminUser): string {
  return formatMarketDate(user.created_at, { timeZone: 'Europe/Istanbul' })
}

export function UsersSection() {
  const navigate = useNavigate()
  const { confirm, dialog: confirmDialog } = useConfirm()
  const { data: me } = useMe()
  const { data: users, isError, error } = useAdminUsers()
  const [adding, setAdding] = useState(false)
  const [resetting, setResetting] = useState<AdminUser | null>(null)
  const [deleting, setDeleting] = useState<AdminUser | null>(null)
  const [typedEmail, setTypedEmail] = useState('')
  const desktop = useIsDesktop()

  function isMe(user: AdminUser): boolean {
    return user.id === me?.id
  }

  // Without the admin role /admin renders NotFound, so leave for home before
  // the /api/me refetch lands.
  const adminMutation = useToggleAdmin(() => {
    void navigate('/', { replace: true })
    toast(t('admin.users.demotedSelf'))
  })
  const once = useOneAtATime()
  const deleteMutation = useDeleteUser((user) => {
    toast(t('admin.users.deleted'))
    // The dialog closed and the refetch removes this row's delete button,
    // dropping a keyboard user to <body>. Move focus to the next
    // user's delete button, else the previous one's, else the heading --
    // unless the user has already moved it elsewhere. The caller's row has
    // no delete button, so it is skipped.
    const deletable = (users ?? []).filter((other) => !isMe(other))
    const neighbour = neighbourOf(
      deletable.map((other) => other.id),
      user.id,
    )
    focusIfLost(
      document.querySelector<HTMLElement>(
        neighbour !== undefined ? `[data-delete-user="${neighbour}"]` : '[data-users-heading]',
      ),
      document.querySelector(`[data-delete-user="${user.id}"]`),
    )
  })
  const actionError = adminMutation.error ?? deleteMutation.error

  // One error line for the row actions; any new action clears it.
  function clearError() {
    adminMutation.reset()
    deleteMutation.reset()
  }

  // The phone list puts the delete button beside the email: at 375 px three
  // buttons overflow one line and the last one wraps.
  function actions(user: AdminUser, withDelete: boolean): ReactNode {
    return (
      <div className="flex flex-wrap items-center justify-end gap-1 whitespace-nowrap md:flex-nowrap">
        <Button
          variant="ghost"
          aria-label={t('admin.users.resetPasswordLabel', { email: user.email })}
          onClick={() => {
            clearError()
            setResetting(user)
          }}
        >
          {t('admin.users.resetPassword')}
        </Button>
        <Button
          variant="ghost"
          pending={adminMutation.isPending}
          aria-label={t(
            user.is_admin ? 'admin.users.removeAdminLabel' : 'admin.users.makeAdminLabel',
            { email: user.email },
          )}
          onClick={() =>
            once(async () => {
              clearError()
              if (
                isMe(user) &&
                user.is_admin &&
                !(await confirm({
                  title: t('admin.users.demoteSelfTitle'),
                  body: t('admin.users.demoteSelfBody'),
                  confirmLabel: t('admin.users.removeAdmin'),
                  danger: true,
                }))
              ) {
                return
              }
              await adminMutation.mutateAsync(user)
            })
          }
        >
          {/* Both labels share one grid cell with the other one hidden, so
              the button is as wide as the longer label on every row and the
              row's buttons line up across rows. */}
          <span className="grid justify-items-center [&>*]:col-start-1 [&>*]:row-start-1">
            <span className={user.is_admin ? '' : 'invisible'}>{t('admin.users.removeAdmin')}</span>
            <span className={user.is_admin ? 'invisible' : ''}>{t('admin.users.makeAdmin')}</span>
          </span>
        </Button>
        {withDelete && deleteSlot(user)}
      </div>
    )
  }

  // The server refuses to delete the caller, so their row gets an empty slot
  // instead, which keeps the columns aligned.
  function deleteSlot(user: AdminUser): ReactNode {
    if (isMe(user)) return <span className="w-9 shrink-0" />
    return (
      <Button
        variant="ghost"
        iconOnly
        aria-label={t('admin.users.delete', { email: user.email })}
        data-delete-user={user.id}
        onClick={() => {
          clearError()
          setTypedEmail('')
          setDeleting(user)
        }}
      >
        <Icon name="trash" />
      </Button>
    )
  }

  const adminPill = <Pill tone="cash">{t('admin.users.adminBadge')}</Pill>

  return (
    <section id="admin-users" className="card scroll-mt-20">
      <div className="border-line flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
        <h2 data-users-heading tabIndex={-1} className="text-ink text-[14px] font-semibold">
          {t('admin.section.users')}
        </h2>
        <Button
          variant="secondary"
          onClick={() => {
            clearError()
            setAdding(true)
          }}
        >
          <Icon name="plus" />
          {t('admin.users.add')}
        </Button>
      </div>

      <div className="px-4 py-3">
        <RegistrationToggle />
        {isError && (
          <p role="alert" className="text-loss mt-3 text-[13px]">
            {error.message}
          </p>
        )}
        {actionError && (
          <p role="alert" className="text-loss mt-3 text-[13px]">
            {actionError.message}
          </p>
        )}
      </div>

      {users && (
        <div className="border-line border-t">
          {desktop ? (
            <div className="relative overflow-x-auto">
              <table className={TABLE}>
                <thead>
                  <tr>
                    <th className={TH}>{t('auth.email')}</th>
                    <th className={TH}>{t('admin.users.created')}</th>
                    <ActionsTh />
                  </tr>
                </thead>
                <tbody>
                  {users.map((user) => (
                    <tr key={user.id} className={ROW_44}>
                      <td className={TD}>
                        {/* The cell does not wrap; a long email does, so it
                        cannot widen the table. */}
                        <span className="flex items-center gap-1.5">
                          <span className="text-ink break-all whitespace-normal">{user.email}</span>
                          {user.is_admin && adminPill}
                        </span>
                      </td>
                      <td className={`${TD} text-ink-2`}>{createdDay(user)}</td>
                      <td className={TD}>{actions(user, true)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <ul>
              {users.map((user) => (
                <li key={user.id} className="border-line border-b px-4 py-2 last:border-0">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <span className="flex flex-wrap items-center gap-1.5">
                        <span className="text-ink text-[14px] break-all">{user.email}</span>
                        {user.is_admin && adminPill}
                      </span>
                      <span className="text-muted mt-0.5 block text-[12px]">
                        {`${t('admin.users.created')} ${createdDay(user)}`}
                      </span>
                    </div>
                    {deleteSlot(user)}
                  </div>
                  <div className="mt-1">{actions(user, false)}</div>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {adding && (
        <AddUserDialog onClose={() => setAdding(false)} onAdded={() => setAdding(false)} />
      )}
      {resetting && (
        <ResetPasswordDialog
          user={resetting}
          onClose={() => setResetting(null)}
          // Resetting your own password ends your session; the refresh of
          // /api/me then sends you to the login page.
          onDone={() => setResetting(null)}
        />
      )}
      {/* Stays mounted: removing an open <dialog> drops focus to <body>,
          close() hands it back to the delete button. */}
      <ConfirmDialog
        open={deleting !== null}
        danger
        title={t('admin.users.deleteTitle', { email: deleting?.email ?? '' })}
        body={t('admin.users.deleteBody')}
        confirmLabel={t('admin.users.deleteConfirm')}
        confirmDisabled={typedEmail.trim().toLowerCase() !== deleting?.email.toLowerCase()}
        onConfirm={() => {
          if (deleting) deleteMutation.mutate(deleting)
          setDeleting(null)
        }}
        onCancel={() => setDeleting(null)}
      >
        <div className="mt-4">
          <Field label={t('auth.email')}>
            <input
              type="email"
              autoComplete="off"
              value={typedEmail}
              onChange={(event) => setTypedEmail(event.target.value)}
              className={inputClass()}
            />
          </Field>
        </div>
      </ConfirmDialog>
      {confirmDialog}
    </section>
  )
}

// The native <dialog> shell shared by the add and reset forms; mounted only
// while open, so fields and server errors reset on every reopen.
function FormDialog({
  title,
  onClose,
  onSubmit,
  children,
}: {
  title: string
  onClose: () => void
  // The mutation's mutateAsync: a second submit while it runs is ignored.
  onSubmit: () => Promise<unknown>
  children: ReactNode
}) {
  const dialogRef = useModal()
  const titleId = useId()
  const once = useOneAtATime()

  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onClose={onClose}>
      <form
        onSubmit={(event) => {
          event.preventDefault()
          once(onSubmit)
        }}
        className={sheetPanel('p-5 md:w-[26rem]')}
      >
        <h2 id={titleId} className="text-ink text-[15px] font-semibold">
          {title}
        </h2>
        {children}
      </form>
    </Modal>
  )
}

function DialogButtons({
  submitLabel,
  pending,
  onClose,
}: {
  submitLabel: string
  pending: boolean
  onClose: () => void
}) {
  return (
    <div className="mt-5 flex justify-end gap-2">
      <Button variant="secondary" onClick={onClose}>
        {t('confirm.cancel')}
      </Button>
      <Button type="submit" pending={pending}>
        {submitLabel}
      </Button>
    </div>
  )
}

function AddUserDialog({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [locale, setLocale] = useState<Locale>(getLocale())
  const mutation = useCreateUser({ email, password, locale }, () => {
    toast(t('admin.users.added'))
    onAdded()
  })

  return (
    <FormDialog
      title={t('admin.users.add')}
      onClose={onClose}
      onSubmit={() => mutation.mutateAsync()}
    >
      <div className="mt-4 flex flex-col gap-4">
        {mutation.isError && (
          <p role="alert" className="text-loss text-[13px]">
            {mutation.error.message}
          </p>
        )}
        <Field label={t('auth.email')}>
          <input
            type="email"
            required
            maxLength={254}
            autoFocus
            autoComplete="off"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            className={inputClass()}
          />
        </Field>
        <Field label={t('auth.password')} hint={t('auth.passwordHint')}>
          <input
            type="password"
            required
            minLength={8}
            autoComplete="new-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            className={inputClass()}
          />
        </Field>
        <Field label={t('account.section.language')}>
          <Select
            value={locale}
            onChange={setLocale}
            options={locales.map((code) => ({ value: code, label: t(`language.${code}`) }))}
          />
        </Field>
      </div>
      <DialogButtons
        submitLabel={t('admin.users.add')}
        pending={mutation.isPending}
        onClose={onClose}
      />
    </FormDialog>
  )
}

function ResetPasswordDialog({
  user,
  onClose,
  onDone,
}: {
  user: AdminUser
  onClose: () => void
  onDone: () => void
}) {
  const [password, setPassword] = useState('')
  const mutation = useResetPassword(user.id, password, () => {
    toast(t('admin.users.passwordReset'))
    onDone()
  })

  return (
    <FormDialog
      title={t('admin.users.resetPassword')}
      onClose={onClose}
      onSubmit={() => mutation.mutateAsync()}
    >
      <p className="text-ink-2 mt-2 text-[13px] wrap-anywhere">
        {t('admin.users.resetPasswordNote', { email: user.email })}
      </p>
      <div className="mt-4">
        <Field
          label={t('account.newPassword')}
          hint={t('auth.passwordHint')}
          error={mutation.isError ? mutation.error.message : undefined}
        >
          <input
            type="password"
            required
            minLength={8}
            autoFocus
            autoComplete="new-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            className={inputClass()}
          />
        </Field>
      </div>
      <DialogButtons
        submitLabel={t('admin.users.resetPassword')}
        pending={mutation.isPending}
        onClose={onClose}
      />
    </FormDialog>
  )
}
