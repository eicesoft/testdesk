import {useState} from 'react'

import {AppSidebar, type Page} from '@/components/AppSidebar'
import {RecorderPage} from '@/pages/RecorderPage'
import {ScriptsPage} from '@/pages/ScriptsPage'
import {SettingsPage} from '@/pages/SettingsPage'

export default function App() {
  const [page, setPage] = useState<Page>('recorder')
  const [openScriptId, setOpenScriptId] = useState<string | null>(null)

  function handleSaved(id: string) {
    setOpenScriptId(id)
    setPage('scripts')
  }

  return (
    <div className="bg-background text-foreground flex h-screen">
      <AppSidebar page={page} onNavigate={setPage} />
      <main className="flex min-w-0 flex-1 flex-col">
        {page === 'recorder' && <RecorderPage onSaved={handleSaved} onOpenSettings={() => setPage('settings')} />}
        {page === 'scripts' && <ScriptsPage openId={openScriptId} />}
        {page === 'settings' && <SettingsPage />}
      </main>
    </div>
  )
}
