import {useCallback, useEffect, useState, type ReactNode} from 'react'
import {Check, Loader2, Minus, Save, Wrench, X} from 'lucide-react'

import {Badge} from '@/components/ui/badge'
import {Button} from '@/components/ui/button'
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from '@/components/ui/card'
import {Input} from '@/components/ui/input'
import {
  EVENT,
  GetRuntimeStatus,
  GetSettings,
  InstallRuntime,
  SaveSettings,
  onEvent,
  settings,
  type SetupProgress,
  type SetupStep,
  runtimeenv,
} from '@/lib/api'
import {cn} from '@/lib/utils'

type Form = {
  openaiBaseUrl: string
  openaiApiKey: string
  openaiModel: string
  nodePath: string
  pythonPath: string
}

const STEPS: {id: SetupStep; label: string}[] = [
  {id: 'node', label: 'Node.js'},
  {id: 'packages', label: 'Playwright 依赖（npm）'},
  {id: 'sidecar', label: '侧车脚本'},
  {id: 'chromium', label: 'Chromium 浏览器'},
  {id: 'python', label: 'Python 运行环境（可选）'},
]

type StepState = {status: SetupProgress['status']; message: string}

export function SettingsPage() {
  const [form, setForm] = useState<Form | null>(null)
  const [saved, setSaved] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [runtime, setRuntime] = useState<runtimeenv.Status | null>(null)
  const [steps, setSteps] = useState<Partial<Record<SetupStep, StepState>>>({})
  const [logs, setLogs] = useState<string[]>([])
  const [installing, setInstalling] = useState(false)

  const refreshRuntime = useCallback(async () => {
    setRuntime(await GetRuntimeStatus())
  }, [])

  useEffect(() => {
    GetSettings()
      .then((s) => setForm({...s}))
      .catch((e) => setError(String(e)))
    refreshRuntime().catch((e) => setError(String(e)))
  }, [refreshRuntime])

  useEffect(
    () =>
      onEvent<SetupProgress>(EVENT.setupProgress, (p) => {
        if (p.step === 'all') {
          setInstalling(false)
          refreshRuntime().catch((e) => setError(String(e)))
          return
        }
        if (p.status === 'log') {
          setLogs((prev) => [...prev.slice(-199), p.message])
          return
        }
        setSteps((prev) => ({...prev, [p.step]: {status: p.status, message: p.message}}))
        if (p.status === 'failed') {
          setInstalling(false)
          setError(p.message)
          refreshRuntime().catch(() => {})
        }
      }),
    [refreshRuntime],
  )

  function update<K extends keyof Form>(key: K, value: string) {
    setForm((prev) => (prev ? {...prev, [key]: value} : prev))
    setSaved(null)
  }

  async function save() {
    if (!form) return
    setError(null)
    try {
      const s = await SaveSettings(new settings.Settings({...form}))
      setForm({...s})
      setSaved('已保存')
      refreshRuntime().catch(() => {})
    } catch (e) {
      setError(String(e))
    }
  }

  async function install() {
    setError(null)
    setLogs([])
    setSteps({})
    setInstalling(true)
    try {
      await InstallRuntime()
    } catch (e) {
      setInstalling(false)
      setError(String(e))
    }
  }

  if (!form) {
    return <div className="text-muted-foreground p-6 text-sm">{error ?? '加载中…'}</div>
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-6">
      <Card>
        <CardHeader>
          <CardTitle>AI 接口（OpenAI 兼容）</CardTitle>
          <CardDescription>
            支持任何兼容 OpenAI Chat Completions 与 function calling 的服务，例如 OpenAI、各类代理网关或本地模型服务。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Field label="Base URL">
            <Input
              placeholder="https://api.openai.com/v1"
              value={form.openaiBaseUrl}
              onChange={(e) => update('openaiBaseUrl', e.target.value)}
            />
          </Field>
          <Field label="API Key">
            <Input
              type="password"
              autoComplete="off"
              placeholder="sk-..."
              value={form.openaiApiKey}
              onChange={(e) => update('openaiApiKey', e.target.value)}
            />
          </Field>
          <Field label="模型名称">
            <Input
              placeholder="例如 gpt-4o、你所用服务商提供的模型 ID"
              value={form.openaiModel}
              onChange={(e) => update('openaiModel', e.target.value)}
            />
          </Field>
          <p className="text-muted-foreground text-xs">
            API Key 以明文保存在用户配置目录下的 settings.json 中，请注意保管该文件。
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>本机解释器</CardTitle>
          <CardDescription>未在 PATH 中时，填写可执行文件的完整路径。</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Field label="Node.js">
            <Input value={form.nodePath} onChange={(e) => update('nodePath', e.target.value)} />
          </Field>
          <Field label="Python">
            <Input value={form.pythonPath} onChange={(e) => update('pythonPath', e.target.value)} />
          </Field>
          <div className="flex items-center gap-3">
            <Button onClick={save}>
              <Save />
              保存设置
            </Button>
            {saved && <span className="text-muted-foreground text-sm">{saved}</span>}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>运行环境</CardTitle>
          <CardDescription>
            录制和回放依赖 Node.js、Playwright 与 Chromium。首次使用请点击安装，之后可随时重新检查或修复。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-wrap gap-2 text-sm">
            <StatusBadge ok={!!runtime?.nodeOk} label={runtime?.nodeVersion ? `Node ${runtime.nodeVersion}` : 'Node 未检测到'} />
            <StatusBadge ok={!!runtime?.playwrightOk} label="Playwright" />
            <StatusBadge ok={!!runtime?.chromiumOk} label="Chromium" />
            <StatusBadge ok={!!runtime?.pythonOk} label={runtime?.pythonOk ? 'Python 运行环境' : 'Python 运行环境（可选）'} optional />
          </div>
          {runtime && <p className="text-muted-foreground font-mono text-xs break-all">{runtime.runtimeDir}</p>}

          <div className="flex items-center gap-3">
            <Button onClick={install} disabled={installing}>
              {installing ? <Loader2 className="animate-spin" /> : <Wrench />}
              {runtime?.ready ? '重新检查并修复' : '安装运行环境'}
            </Button>
            {runtime?.ready && <span className="text-sm text-emerald-600">运行环境已就绪</span>}
          </div>

          {(installing || Object.keys(steps).length > 0) && (
            <ol className="flex flex-col gap-2 text-sm">
              {STEPS.map((s) => {
                const st = steps[s.id]
                return (
                  <li key={s.id} className="flex items-start gap-2">
                    <StepIcon status={st?.status} />
                    <div className="min-w-0">
                      <div className="font-medium">{s.label}</div>
                      {st?.message && <div className="text-muted-foreground text-xs break-all">{st.message}</div>}
                    </div>
                  </li>
                )
              })}
            </ol>
          )}

          {logs.length > 0 && (
            <pre className="bg-muted/40 max-h-56 overflow-y-auto rounded-md p-3 font-mono text-xs whitespace-pre-wrap">
              {logs.join('\n')}
            </pre>
          )}

          {error && <p className="text-destructive text-sm break-all">{error}</p>}
        </CardContent>
      </Card>
    </div>
  )
}

function Field({label, children}: {label: string; children: ReactNode}) {
  return (
    <label className="grid grid-cols-[120px_1fr] items-center gap-3 text-sm">
      <span className="text-muted-foreground">{label}</span>
      {children}
    </label>
  )
}

function StatusBadge({ok, label, optional}: {ok: boolean; label: string; optional?: boolean}) {
  return (
    <Badge variant={ok ? 'secondary' : 'outline'} className={cn('gap-1', !ok && !optional && 'text-destructive')}>
      {ok ? <Check /> : <X />}
      {label}
    </Badge>
  )
}

function StepIcon({status}: {status?: SetupProgress['status']}) {
  switch (status) {
    case 'done':
      return <Check className="mt-0.5 size-4 shrink-0 text-emerald-600" />
    case 'failed':
      return <X className="text-destructive mt-0.5 size-4 shrink-0" />
    case 'running':
      return <Loader2 className="mt-0.5 size-4 shrink-0 animate-spin" />
    case 'skipped':
      return <Minus className="text-muted-foreground mt-0.5 size-4 shrink-0" />
    default:
      return <span className="border-muted-foreground/40 mt-0.5 inline-block size-4 shrink-0 rounded-full border" />
  }
}
