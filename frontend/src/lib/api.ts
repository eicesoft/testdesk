import {EventsOn} from '../../wailsjs/runtime/runtime'
import {scripts, settings} from '../../wailsjs/go/models'

export {
  ListScripts,
  GetScript,
  SaveScript,
  DeleteScript,
  RunScript,
  StopRun,
  StartRecording,
  StopRecording,
  GetSettings,
  SaveSettings,
  GetRuntimeStatus,
  InstallRuntime,
} from '../../wailsjs/go/main/App'
export {recorder, runtimeenv, scripts, settings} from '../../wailsjs/go/models'

export type Script = scripts.Script
export type AppSettings = settings.Settings
export type Language = 'python' | 'javascript'

export const LANGUAGES: {value: Language; label: string}[] = [
  {value: 'python', label: 'Python'},
  {value: 'javascript', label: 'JavaScript'},
]

// Keep in sync with recorder.Mode in internal/recorder/recorder.go.
export type Mode = 'ai' | 'manual'

export const MODES: {value: Mode; label: string}[] = [
  {value: 'ai', label: 'AI 录制'},
  {value: 'manual', label: '人工录制'},
]

// Keep in sync with the constants in app.go.
export const EVENT = {
  runOutput: 'run:output',
  runDone: 'run:done',
  recorderEvent: 'recorder:event',
  setupProgress: 'setup:progress',
} as const

export type RunOutput = {runId: string; stream: 'stdout' | 'stderr'; line: string}
export type RunResult = {exitCode: number; durationMs: number; error?: string}
export type RunDone = {runId: string; scriptId: string; result: RunResult}

export type RecorderEventKind = 'status' | 'message' | 'action' | 'code' | 'error' | 'done'
export type RecorderEvent = {kind: RecorderEventKind; text: string}

export type SetupStep = 'node' | 'packages' | 'sidecar' | 'chromium' | 'python' | 'all'
export type SetupProgress = {step: SetupStep; status: 'running' | 'done' | 'failed' | 'skipped' | 'log'; message: string}

/** Subscribes to a backend event. Returns the unsubscribe function. */
export function onEvent<T>(name: string, handler: (data: T) => void): () => void {
  return EventsOn(name, handler)
}

export function formatTime(value: unknown): string {
  if (typeof value !== 'string' && typeof value !== 'number') return ''
  const d = new Date(value)
  return isNaN(d.getTime()) ? '' : d.toLocaleString()
}
