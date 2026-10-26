import {useCallback, useEffect, useState} from 'react'
import {Play, Plus, Save, Square, Trash2} from 'lucide-react'

import {Badge} from '@/components/ui/badge'
import {Button} from '@/components/ui/button'
import {Input} from '@/components/ui/input'
import {NativeSelect} from '@/components/ui/native-select'
import {Textarea} from '@/components/ui/textarea'
import {
  DeleteScript,
  EVENT,
  GetScript,
  LANGUAGES,
  ListScripts,
  RunScript,
  SaveScript,
  StopRun,
  formatTime,
  onEvent,
  scripts,
  type Language,
  type RunDone,
  type RunOutput,
  type RunResult,
  type Script,
} from '@/lib/api'
import {cn} from '@/lib/utils'

type Draft = {id: string; name: string; language: Language; code: string}

const EMPTY: Draft = {id: '', name: '', language: 'python', code: ''}

type RunState = {lines: RunOutput[]; done?: RunResult}

function toDraft(sc: Script): Draft {
  return {id: sc.id, name: sc.name, language: sc.language as Language, code: sc.code}
}

type Props = {
  // Set when another page asks to open a script (e.g. right after saving a recording).
  openId: string | null
}

export function ScriptsPage({openId}: Props) {
  const [list, setList] = useState<Script[]>([])
  const [draft, setDraft] = useState<Draft>(EMPTY)
  const [saved, setSaved] = useState<Draft>(EMPTY)
  const [runs, setRuns] = useState<Record<string, RunState>>({})
  const [activeRunId, setActiveRunId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const dirty = JSON.stringify(draft) !== JSON.stringify(saved)
  const activeRun = activeRunId ? runs[activeRunId] : undefined
  const running = !!activeRun && !activeRun.done

  const refresh = useCallback(async () => {
    setList(await ListScripts())
  }, [])

  const load = useCallback(async (id: string) => {
    const d = toDraft(await GetScript(id))
    setDraft(d)
    setSaved(d)
  }, [])

  useEffect(() => {
    refresh().catch((e) => setError(String(e)))
  }, [refresh])

  useEffect(() => {
    if (openId) load(openId).catch((e) => setError(String(e)))
  }, [openId, load])

  // Subscribe once. Output is keyed by runId, so events that arrive before
  // RunScript resolves are still kept.
  useEffect(() => {
    const offOutput = onEvent<RunOutput>(EVENT.runOutput, (ev) =>
      setRuns((prev) => {
        const cur = prev[ev.runId] ?? {lines: []}
        return {...prev, [ev.runId]: {...cur, lines: [...cur.lines, ev]}}
      }),
    )
    const offDone = onEvent<RunDone>(EVENT.runDone, (ev) =>
      setRuns((prev) => {
        const cur = prev[ev.runId] ?? {lines: []}
        return {...prev, [ev.runId]: {...cur, done: ev.result}}
      }),
    )
    return () => {
      offOutput()
      offDone()
    }
  }, [])

  function confirmDiscard() {
    return !dirty || window.confirm('有未保存的修改，确定放弃吗？')
  }

  async function handleSelect(id: string) {
    if (!confirmDiscard()) return
    setError(null)
    try {
      await load(id)
    } catch (e) {
      setError(String(e))
    }
  }

  function handleNew() {
    if (!confirmDiscard()) return
    setError(null)
    setDraft(EMPTY)
    setSaved(EMPTY)
  }

  async function handleSave() {
    setError(null)
    try {
      const sc = await SaveScript(new scripts.Script({...draft}))
      const d = toDraft(sc)
      setDraft(d)
      setSaved(d)
      await refresh()
    } catch (e) {
      setError(String(e))
    }
  }

  async function handleDelete() {
    if (!draft.id || !window.confirm(`删除脚本「${draft.name}」？`)) return
    setError(null)
    try {
      await DeleteScript(draft.id)
      setDraft(EMPTY)
      setSaved(EMPTY)
      await refresh()
    } catch (e) {
      setError(String(e))
    }
  }

  async function handleRun() {
    if (!draft.id) return
    if (dirty) {
      setError('请先保存修改再运行')
      return
    }
    setError(null)
    try {
      setActiveRunId(await RunScript(draft.id))
    } catch (e) {
      setError(String(e))
    }
  }

  async function handleStop() {
    if (activeRunId) await StopRun(activeRunId)
  }

  return (
    <div className="flex min-h-0 flex-1">
      <aside className="flex w-64 shrink-0 flex-col border-r">
        <div className="flex items-center justify-between p-3">
          <h2 className="text-sm font-semibold">脚本库</h2>
          <Button size="sm" variant="outline" onClick={handleNew}>
            <Plus />
            新建
          </Button>
        </div>
        <div className="flex-1 overflow-y-auto px-2 pb-3">
          {list.length === 0 && (
            <p className="text-muted-foreground px-2 py-6 text-center text-sm">还没有脚本</p>
          )}
          {list.map((sc) => (
            <button
              key={sc.id}
              type="button"
              onClick={() => handleSelect(sc.id)}
              className={cn(
                'hover:bg-accent flex w-full flex-col items-start gap-1 rounded-md px-3 py-2 text-left text-sm',
                sc.id === draft.id && 'bg-accent',
              )}
            >
              <span className="w-full truncate font-medium">{sc.name}</span>
              <span className="text-muted-foreground text-xs">
                {sc.language} · {formatTime(sc.updatedAt)}
              </span>
            </button>
          ))}
        </div>
      </aside>

      <section className="flex min-w-0 flex-1 flex-col">
        {draft.id || dirty || draft.name || draft.code ? (
          <>
            <div className="flex flex-wrap items-center gap-2 border-b p-3">
              <Input
                className="max-w-xs"
                placeholder="脚本名称"
                value={draft.name}
                onChange={(e) => setDraft({...draft, name: e.target.value})}
              />
              <NativeSelect
                value={draft.language}
                onChange={(e) => setDraft({...draft, language: e.target.value as Language})}
              >
                {LANGUAGES.map((l) => (
                  <option key={l.value} value={l.value}>
                    {l.label}
                  </option>
                ))}
              </NativeSelect>
              {dirty && <Badge variant="secondary">未保存</Badge>}
              <div className="ml-auto flex gap-2">
                <Button variant="outline" onClick={handleSave} disabled={!dirty && !!draft.id}>
                  <Save />
                  保存
                </Button>
                {running ? (
                  <Button variant="destructive" onClick={handleStop}>
                    <Square />
                    停止
                  </Button>
                ) : (
                  <Button onClick={handleRun} disabled={!draft.id}>
                    <Play />
                    运行
                  </Button>
                )}
                {draft.id && (
                  <Button variant="ghost" size="icon" onClick={handleDelete} aria-label="删除脚本">
                    <Trash2 />
                  </Button>
                )}
              </div>
            </div>

            {error && <p className="text-destructive px-3 pt-2 text-sm">{error}</p>}

            <Textarea
              className="min-h-0 flex-1 resize-none rounded-none border-0 font-mono text-xs focus-visible:ring-0"
              placeholder="脚本内容"
              spellCheck={false}
              value={draft.code}
              onChange={(e) => setDraft({...draft, code: e.target.value})}
            />

            <div className="flex h-56 shrink-0 flex-col border-t">
              <div className="flex items-center justify-between border-b px-4 py-2 text-xs">
                <span className="font-medium">输出</span>
                {activeRun?.done && (
                  <Badge variant={activeRun.done.exitCode === 0 && !activeRun.done.error ? 'secondary' : 'destructive'}>
                    退出码 {activeRun.done.exitCode} · {activeRun.done.durationMs} ms
                  </Badge>
                )}
              </div>
              <div className="flex-1 overflow-y-auto p-4 font-mono text-xs">
                {activeRun?.lines.map((l, i) => (
                  <div key={i} className={cn('whitespace-pre-wrap', l.stream === 'stderr' && 'text-destructive')}>
                    {l.line}
                  </div>
                ))}
                {activeRun?.done?.error && <div className="text-destructive">{activeRun.done.error}</div>}
                {!activeRun && <div className="text-muted-foreground">运行脚本后输出会显示在这里。</div>}
              </div>
            </div>
          </>
        ) : (
          <div className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
            从左侧选择脚本，或点击「新建」。
          </div>
        )}
      </section>
    </div>
  )
}
