import { useMe } from '../api/auth'
import { InstrumentsSection } from '../components/admin/InstrumentsSection'
import { ProvidersSection } from '../components/admin/ProvidersSection'
import { SourcesSection } from '../components/admin/SourcesSection'
import { UsersSection } from '../components/admin/UsersSection'
import { PageHeader } from '../components/PageHeader'
import { t } from '../i18n/strings'
import { useTitle } from '../lib/useTitle'
import { NotFoundPage } from './NotFoundPage'

// Anchors match the id on each section's root element.
const sections = [
  { id: 'admin-providers', label: 'admin.section.providers' },
  { id: 'admin-sources', label: 'admin.section.sources' },
  { id: 'admin-instruments', label: 'admin.instruments.title' },
  { id: 'admin-users', label: 'admin.section.users' },
] as const

// The server guards every admin endpoint; this only hides the page from
// non-admins, who get the same NotFound as any unknown URL.
export function AdminPage() {
  const { data: me } = useMe()
  useTitle(me?.is_admin ? t('admin.heading') : t('notFound.title'))

  if (!me?.is_admin) {
    return <NotFoundPage />
  }

  return (
    <main id="main">
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        <PageHeader title={t('admin.heading')} />
        {/* One row that scrolls sideways when it does not fit; 44 px targets. */}
        <nav aria-label={t('admin.sectionsNav')} className="flex gap-4 overflow-x-auto">
          {sections.map((section) => (
            <a
              key={section.id}
              href={`#${section.id}`}
              className="text-muted hover:text-accent flex h-11 shrink-0 items-center text-[13px] whitespace-nowrap"
            >
              {t(section.label)}
            </a>
          ))}
        </nav>
        <div className="mt-2 flex flex-col gap-4">
          <ProvidersSection />
          <SourcesSection />
          <InstrumentsSection />
          <UsersSection />
        </div>
      </div>
    </main>
  )
}
