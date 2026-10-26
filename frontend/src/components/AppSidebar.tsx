import {FileCode, Settings, Video, type LucideIcon} from 'lucide-react'

import {cn} from '@/lib/utils'

export type Page = 'recorder' | 'scripts' | 'settings'

const ITEMS: {id: Page; label: string; icon: LucideIcon}[] = [
  {id: 'recorder', label: '录制', icon: Video},
  {id: 'scripts', label: '脚本库', icon: FileCode},
  {id: 'settings', label: '设置', icon: Settings},
]

type Props = {
  page: Page
  onNavigate: (page: Page) => void
}

export function AppSidebar({page, onNavigate}: Props) {
  return (
    <aside className="bg-sidebar text-sidebar-foreground flex w-56 shrink-0 flex-col border-r">
      <div className="px-5 py-4">
        <div className="text-base font-semibold">TestDesk</div>
        <div className="text-muted-foreground text-xs">AI 浏览器测试录制</div>
      </div>
      <nav className="flex flex-col gap-1 px-3">
        {ITEMS.map(({id, label, icon: Icon}) => (
          <button
            key={id}
            type="button"
            onClick={() => onNavigate(id)}
            className={cn(
              'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground',
              page === id ? 'bg-sidebar-accent text-sidebar-accent-foreground font-medium' : 'text-muted-foreground',
            )}
          >
            <Icon className="size-4" />
            {label}
          </button>
        ))}
      </nav>
    </aside>
  )
}
