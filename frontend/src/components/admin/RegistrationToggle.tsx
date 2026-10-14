import { useAuthConfig, useSetRegistrationOpen } from '../../api/auth'
import { t } from '../../i18n/strings'
import { useOneAtATime } from '../../lib/useOneAtATime'
import { Checkbox } from '../ui/Checkbox'

// Its own component so the setup wizard's step 2 can reuse it.
export function RegistrationToggle() {
  const { data } = useAuthConfig()
  const mutation = useSetRegistrationOpen()
  const once = useOneAtATime()

  return (
    <div>
      <Checkbox
        label={t('admin.registration.label')}
        description={t('admin.registration.hint')}
        checked={data?.registration_open ?? false}
        disabled={!data}
        pending={mutation.isPending}
        onChange={(checked) => once(() => mutation.mutateAsync(checked))}
      />
      {mutation.isError && (
        <p role="alert" className="text-loss mt-1 text-[13px]">
          {mutation.error.message}
        </p>
      )}
    </div>
  )
}
