import {useEffect, useState} from 'react'
import {CircleDot, Save, Settings as SettingsIcon, Square} from 'lucide-react'

import {Badge} from '@/components/ui/badge'
import {Button} from '@/components/ui/button'
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from '@/components/ui/card'
import {Input} from '@/components/ui/input'
import {NativeSelect} from '@/components/ui/native-select'
import {Textarea} from '@/components/ui/textarea'
import {
  EVENT,
  GetRuntimeStatus,
  LANGUAGES,
  MODES,
  SaveScript,
  StartRecording,
  StopRecording,
  onEvent,
  recorder,
  scripts,
  type Language,
  type Mode,
  type RecorderEvent,
} from '@/lib/api'

type Props = {
  onSaved: (id: string) => void
  onOpenSettings: () => void
}

export function RecorderPage({onSaved, onOpenSettings}: Props) {
  const [url, setUrl] = useState('https://')
  const [goal, setGoal] = useState('')
  const [language, setLanguage] = useState<Language>('python')
  const [mode, setMode] = useState<Mode>('ai')
  // The language the current code was generated in. The dropdown can change after recording,
  // so saving must use this rather than `language`.
  const [codeLanguage, setCodeLanguage] = useState<Language>('python')

  const [runtimeReady, setRuntimeReady] = useState<boolean | null>(null)
  const [recording, setRecording] = useState(false)
  const [events, setEvents] = useState<RecorderEvent[]>([])
  const [code, setCode] = useState('')
  const [scriptName, setScriptName] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    GetRuntimeStatus()
      .then((s) => setRuntimeReady(s.ready))
      .catch(() => setRuntimeReady(false))
  }, [])

  useEffect(
    () =>
      onEvent<RecorderEvent>(EVENT.recorderEvent, (ev) => {
        setEvents((prev) => [...prev, ev])
        if (ev.kind === 'code') setCode(ev.text)
        if (ev.kind === 'error') setError(ev.text)
        if (ev.kind === 'done') setRecording(false)
      }),
    [],
  )

  async function start() {
    setError(null)
    setCode('')
    setEvents([])
    setCodeLanguage(language)
    try {
      await StartRecording(new recorder.Request({url, language, goal, mode}))
      setRecording(true)
    } catch (e) {
      setError(String(e))
    }
  }

  async function stop() {
    await StopRecording()
  }

  async function save() {
    setError(null)
    const name = scriptName.trim() || `录制 ${new Date().toLocaleString()}`
    try {
      const sc = await SaveScript(new scripts.Script({id: '', name, language: codeLanguage, code}))
      onSaved(sc.id)
    } catch (e) {
      setError(String(e))
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-6">
      {runtimeReady === false && (
        <Card className="border-destructive/40">
          <CardContent className="flex items-center justify-between gap-4">
            <div className="text-sm">
              <div className="font-medium">需要先安装运行环境</div>
              <div className="text-muted-foreground">录制依赖 Node.js、Playwright 和 Chromium，首次使用请在设置中安装。</div>
            </div>
            <Button variant="outline" onClick={onOpenSettings}>
              <SettingsIcon />
              前往设置
            </Button>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>录制参数</CardTitle>
          <CardDescription>
            {mode === 'ai'
              ? 'AI 会打开浏览器按照目标操作，并生成可重复执行的脚本。'
              : '浏览器会打开由你亲自操作，每次点击、输入和选择都会被记录，点击「停止录制」或关闭浏览器后生成脚本。'}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="grid grid-cols-[1fr_160px_160px] gap-3">
            <Input
              placeholder="起始网址，例如 https://example.com"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              disabled={recording}
            />
            <NativeSelect
              value={mode}
              onChange={(e) => setMode(e.target.value as Mode)}
              disabled={recording}
            >
              {MODES.map((m) => (
                <option key={m.value} value={m.value}>
                  {m.label}
                </option>
              ))}
            </NativeSelect>
            <NativeSelect
              value={language}
              onChange={(e) => setLanguage(e.target.value as Language)}
              disabled={recording}
            >
              {LANGUAGES.map((l) => (
                <option key={l.value} value={l.value}>
                  {l.label}
                </option>
              ))}
            </NativeSelect>
          </div>
          {mode === 'ai' && (
            <Textarea
              placeholder="测试目标，例如：登录后进入订单页，检查列表至少有一条记录"
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
              disabled={recording}
              className="min-h-24"
            />
          )}
          <div className="flex gap-2">
            {recording ? (
              <Button variant="destructive" onClick={stop}>
                <Square />
                停止录制
              </Button>
            ) : (
              <Button onClick={start} disabled={!url.trim()}>
                <CircleDot />
                开始录制
              </Button>
            )}
          </div>
          {error && <p className="text-destructive text-sm">{error}</p>}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>进度</CardTitle>
        </CardHeader>
        <CardContent>
          {events.length === 0 ? (
            <p className="text-muted-foreground text-sm">尚未开始。</p>
          ) : (
            <ol className="flex max-h-64 flex-col gap-2 overflow-y-auto text-sm">
              {events.map((ev, i) => (
                <li key={i} className="flex items-start gap-2">
                  <Badge variant="outline" className="shrink-0">
                    {ev.kind}
                  </Badge>
                  <span className="whitespace-pre-wrap break-all">{ev.text}</span>
                </li>
              ))}
            </ol>
          )}
        </CardContent>
      </Card>

      {code && (
        <Card>
          <CardHeader>
            <CardTitle>生成的脚本</CardTitle>
            <CardDescription>确认无误后保存到脚本库，之后可以重复执行。</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex gap-2">
              <Input
                placeholder="脚本名称"
                value={scriptName}
                onChange={(e) => setScriptName(e.target.value)}
              />
              <Button onClick={save}>
                <Save />
                保存到脚本库
              </Button>
            </div>
            <Textarea
              value={code}
              onChange={(e) => setCode(e.target.value)}
              spellCheck={false}
              className="min-h-64 font-mono text-xs"
            />
          </CardContent>
        </Card>
      )}
    </div>
  )
}
