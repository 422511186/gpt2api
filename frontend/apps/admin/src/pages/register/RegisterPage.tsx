import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import {
  AlertTriangle,
  BarChart3,
  FileText,
  LoaderCircle,
  Mail,
  Play,
  Plus,
  RotateCcw,
  Save,
  Settings2,
  Square,
  Pencil,
  Trash2,
  Eye,
} from 'lucide-react';

import { ApiError } from '../../lib/api';
import { registerApi } from '../../lib/services';
import type {
  RegisterConfig,
  MailProviderConfig,
  RegisterConfigReq,
} from '../../lib/types';
import { toast } from '../../stores/toast';

const TOKEN_KEY_FOR_SSE = 'klein:admin:token';

const PROVIDER_TYPES = [
  { value: 'tempmail_lol', label: 'TempMail.lol' },
  { value: 'cloudflare_temp_email', label: 'CloudflareTempMail' },
  { value: 'moemail', label: 'MoEmail' },
  { value: 'ddg_mail', label: 'DDGMail' },
  { value: 'inbucket', label: 'Inbucket' },
] as const;

const TYPE_LABELS: Record<string, string> = Object.fromEntries(
  PROVIDER_TYPES.map((t) => [t.value, t.label])
);

// ==================== Main page ====================

export default function RegisterPage() {
  const [config, setConfig] = useState<RegisterConfig | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [providerModal, setProviderModal] = useState<{
    mode: 'add' | 'edit';
    index?: number;
    data: MailProviderConfig;
  } | null>(null);
  const [importModal, setImportModal] = useState<{ index: number } | null>(null);
  const [logsExpanded, setLogsExpanded] = useState(false);

  const configQuery = useQuery({
    queryKey: ['admin', 'register', 'config'],
    queryFn: () => registerApi.get(),
  });

  useEffect(() => {
    if (configQuery.data) {
      setConfig(configQuery.data.register);
    }
  }, [configQuery.data]);

  // SSE for real-time updates
  useEffect(() => {
    let closed = false;
    const stored = localStorage.getItem(TOKEN_KEY_FOR_SSE);
    if (!stored) return;

    const tok = JSON.parse(stored);
    const token = tok.access as string;
    if (!token) return;

    const baseURL = (import.meta.env.VITE_ADMIN_BASE_URL as string | undefined)?.replace(/\/+$/, '') ?? '/admin/api/v1';
    const sseURL = `${baseURL}/register/events?token=${encodeURIComponent(token)}`;
    const source = new EventSource(sseURL);

    source.onmessage = (event: MessageEvent) => {
      try {
        setConfig(JSON.parse(event.data) as RegisterConfig);
      } catch { /* ignore */ }
    };

    source.onerror = () => {
      source.close();
      if (!closed) {
        setTimeout(() => {
          if (!closed) {
            const s = localStorage.getItem(TOKEN_KEY_FOR_SSE);
            if (!s) return;
            const t = JSON.parse(s);
            const tok2 = t.access as string;
            if (!tok2) return;
            const ns = new EventSource(`${baseURL}/register/events?token=${encodeURIComponent(tok2)}`);
            ns.onmessage = (e: MessageEvent) => {
              try { setConfig(JSON.parse(e.data)); } catch {}
            };
          }
        }, 3000);
      }
    };

    return () => { closed = true; source.close(); };
  }, []);

  // Actions
  const saveConfig = async () => {
    if (!config) return;
    setIsSaving(true);
    try {
      const body: RegisterConfigReq = {
        mail: config.mail,
        proxy: config.proxy,
        total: config.total,
        threads: config.threads,
        mode: config.mode,
        target_quota: config.target_quota,
        target_available: config.target_available,
        check_interval: config.check_interval,
      };
      const res = await registerApi.update(body);
      setConfig(res.register);
      toast.success('配置已保存');
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '保存失败');
    } finally {
      setIsSaving(false);
    }
  };

  const startMut = useMutation({
    mutationFn: () => registerApi.start(),
    onSuccess: (res) => { setConfig(res.register); toast.success('注册任务已启动'); },
    onError: (e: ApiError) => toast.error(e.message),
  });

  const stopMut = useMutation({
    mutationFn: () => registerApi.stop(),
    onSuccess: (res) => { setConfig(res.register); toast.success('注册任务已停止'); },
    onError: (e: ApiError) => toast.error(e.message),
  });

  const resetMut = useMutation({
    mutationFn: () => registerApi.reset(),
    onSuccess: (res) => { setConfig(res.register); toast.success('统计数据已重置'); },
    onError: (e: ApiError) => toast.error(e.message),
  });

  if (configQuery.isLoading) {
    return (
      <div className="page page-wide flex items-center justify-center">
        <LoaderCircle className="size-5 animate-spin text-text-tertiary" />
      </div>
    );
  }

  if (!config) {
    return (
      <div className="page page-wide">
        <div className="empty-state">
          <p className="empty-state-title">无法加载配置</p>
        </div>
      </div>
    );
  }

  const stats = config.stats || {
    success: 0, fail: 0, done: 0, running: 0,
    threads: config.threads, elapsed_seconds: 0,
    avg_seconds: 0, success_rate: 0,
    current_quota: 0, current_available: 0,
  };

  const logs = config.logs || [];
  const isRunning = config.enabled;

  // Helpers
  const updateConfig = (updates: Partial<RegisterConfig>) =>
    setConfig((prev) => prev ? { ...prev, ...updates } : prev);

  const updateMail = (updates: Partial<RegisterConfig['mail']>) =>
    setConfig((prev) => prev ? { ...prev, mail: { ...prev.mail, ...updates } } : prev);

  const deleteProvider = (index: number) =>
    setConfig((prev) => {
      if (!prev) return prev;
      return { ...prev, mail: { ...prev.mail, providers: prev.mail.providers.filter((_, i) => i !== index) } };
    });

  const handleSaveProvider = (data: MailProviderConfig) => {
    if (!config || !data.type) return;
    if (providerModal?.mode === 'add') {
      setConfig({
        ...config,
        mail: { ...config.mail, providers: [...config.mail.providers, data] },
      });
    } else if (providerModal?.mode === 'edit' && providerModal.index !== undefined) {
      const providers = [...config.mail.providers];
      providers[providerModal.index] = { ...data, type: data.type };
      setConfig({ ...config, mail: { ...config.mail, providers } });
    }
    setProviderModal(null);
  };

  const handleImportKeys = (keys: string[]) => {
    if (!config || importModal === null) return;
    const index = importModal.index;
    const providers = [...config.mail.providers];
    const provider = providers[index];
    if (!provider) return;
    const existing = (provider.api_key || '').split('\n').filter(Boolean);
    const merged = [...new Set([...existing, ...keys])];
    providers[index] = { ...provider, api_key: merged.join('\n'), type: provider.type };
    setConfig({ ...config, mail: { ...config.mail, providers } });
    setImportModal(null);
    toast.success(`已导入 ${keys.length} 个 API Key（去重后 ${merged.length} 个）`);
  };

  // ==================== Render ====================

  return (
    <div className="page page-wide space-y-4">
      {/* Page header */}
      <header className="page-header">
        <div>
          <h1 className="page-title">ChatGPT 注册机</h1>
          <p className="page-subtitle">自动注册 OpenAI 账号并获取 OAuth Token，写入号池。</p>
        </div>
        <div className="flex items-center gap-3">
          <span className={`badge px-3 py-1 text-sm ${isRunning ? 'badge-success' : 'badge'}`}>
            {isRunning ? '运行中' : '已停止'}
          </span>
          <button className="btn btn-outline btn-sm" onClick={() => configQuery.refetch()}>
            <RotateCcw size={14} /> 刷新
          </button>
          <button
            className="btn btn-primary btn-sm"
            onClick={() => void saveConfig()}
            disabled={isSaving || isRunning}
          >
            {isSaving ? <LoaderCircle className="size-4 animate-spin" /> : <Save size={14} />}
            保存配置
          </button>
        </div>
      </header>

      {/* Status bar + controls */}
      <section className="card card-section">
        <div className="grid gap-3 lg:grid-cols-[1fr_auto] lg:items-center">
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
            <StatCard label="成功 / 成功率" value={`${stats.success} / ${stats.success_rate || 0}%`} accent="green" />
            <StatCard label="失败" value={String(stats.fail)} accent="red" />
            <StatCard label="完成 / 线程" value={`${stats.done} / ${stats.threads}`} />
            <StatCard label="运行时间" value={`${stats.elapsed_seconds || 0}s`} accent="yellow" />
          </div>
          <div className="flex gap-2">
            <button
              className="btn btn-success btn-sm"
              onClick={() => startMut.mutate()}
              disabled={startMut.isPending || isRunning}
            >
              {startMut.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Play size={15} />}
              启动
            </button>
            <button
              className="btn btn-outline btn-sm"
              onClick={() => stopMut.mutate()}
              disabled={stopMut.isPending || !isRunning}
            >
              {stopMut.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Square size={15} />}
              停止
            </button>
            <button
              className="btn btn-outline btn-sm"
              onClick={() => resetMut.mutate()}
              disabled={resetMut.isPending || isRunning}
            >
              <RotateCcw size={15} /> 重置
            </button>
          </div>
        </div>

        <div className="mt-3 grid grid-cols-3 gap-2 pt-3 border-t border-border">
          <div className="text-center">
            <div className="text-xs text-text-tertiary">平均单个</div>
            <div className="text-lg font-semibold text-text-primary">{stats.avg_seconds || 0}s</div>
          </div>
          <div className="text-center">
            <div className="text-xs text-text-tertiary">当前额度</div>
            <div className="text-lg font-semibold text-text-primary">{stats.current_quota || 0}</div>
          </div>
          <div className="text-center">
            <div className="text-xs text-text-tertiary">正常账号</div>
            <div className="text-lg font-semibold text-text-primary">{stats.current_available || 0}</div>
          </div>
        </div>
      </section>

      {/* Two-column config */}
      <div className="grid gap-4 xl:grid-cols-2">
        {/* General config */}
        <section className="card card-section space-y-4">
          <header className="flex items-start gap-3">
            <span className="grid place-items-center w-9 h-9 rounded-md bg-info-soft text-klein-500">
              <Settings2 size={18} />
            </span>
            <div>
              <h2 className="text-h5 font-semibold text-text-primary">注册配置</h2>
              <p className="text-small text-text-tertiary mt-0.5">控制注册模式、并发线程数、代理和目标阈值。</p>
            </div>
          </header>
          <div className="grid gap-3">
            <div className="grid gap-3 md:grid-cols-2">
              <Field label="注册模式">
                <select
                  className="select"
                  value={config.mode}
                  onChange={(e) => updateConfig({ mode: e.target.value as 'total' | 'quota' | 'available' })}
                  disabled={isRunning}
                >
                  <option value="total">注册总数</option>
                  <option value="quota">号池剩余额度</option>
                  <option value="available">可用账号数量</option>
                </select>
              </Field>
              <NumberField
                label={config.mode === 'total' ? '注册总数' : config.mode === 'quota' ? '目标剩余额度' : '目标可用账号'}
                value={
                  config.mode === 'total' ? config.total :
                  config.mode === 'quota' ? config.target_quota : config.target_available
                }
                min={1}
                onChange={(v) => {
                  if (config.mode === 'total') updateConfig({ total: v });
                  else if (config.mode === 'quota') updateConfig({ target_quota: v });
                  else updateConfig({ target_available: v });
                }}
                disabled={isRunning}
              />
              <NumberField label="线程数" value={config.threads} min={1} max={20} onChange={(v) => updateConfig({ threads: v })} disabled={isRunning} />
              <Field label="检查间隔（秒）">
                <input
                  type="number"
                  className="input"
                  value={config.check_interval}
                  onChange={(e) => updateConfig({ check_interval: Number(e.target.value) || 5 })}
                  disabled={isRunning || config.mode === 'total'}
                />
              </Field>
            </div>
            <Field label="注册代理" hint="注册时使用的 HTTP 代理，留空则直连">
              <input
                className="input"
                value={config.proxy}
                onChange={(e) => updateConfig({ proxy: e.target.value })}
                placeholder="http://127.0.0.1:7890"
                disabled={isRunning}
              />
            </Field>
            <Field label="FlareSolverr" hint="Cloudflare 验证器地址，用于绕过 auth.openai.com 的人机检测">
              <input
                className="input"
                value={config.flaresolverr_url}
                onChange={(e) => updateConfig({ flaresolverr_url: e.target.value })}
                placeholder="http://flaresolverr:8191"
                disabled={isRunning}
              />
            </Field>
            {!isRunning && (
              <div className="flex items-center gap-2 rounded-md border border-warning bg-warning/10 px-3 py-2 text-xs text-warning">
                <AlertTriangle size={14} className="shrink-0" />
                启动之前请先保存配置，运行中不可修改。
              </div>
            )}
          </div>
        </section>

        {/* Mail config */}
        <section className="card card-section space-y-4">
          <header className="flex items-start justify-between gap-3">
            <div className="flex items-start gap-3">
              <span className="grid place-items-center w-9 h-9 rounded-md bg-info-soft text-klein-500">
                <Mail size={18} />
              </span>
              <div>
                <h2 className="text-h5 font-semibold text-text-primary">邮箱配置</h2>
                <p className="text-small text-text-tertiary mt-0.5">临时邮箱服务，支持多个 provider 轮换。</p>
              </div>
            </div>
            <button
              type="button"
              className="btn btn-outline btn-sm shrink-0"
              onClick={() => setProviderModal({
                mode: 'add',
                data: { type: 'tempmail_lol', enable: true },
              })}
              disabled={isRunning}
            >
              <Plus size={14} /> 添加
            </button>
          </header>
          <div className="grid gap-3">
            <div className="grid gap-3 md:grid-cols-3">
              <NumberField label="请求超时" value={config.mail.request_timeout} min={5} onChange={(v) => updateMail({ request_timeout: v })} disabled={isRunning} />
              <NumberField label="验证码超时" value={config.mail.wait_timeout} min={10} onChange={(v) => updateMail({ wait_timeout: v })} disabled={isRunning} />
              <NumberField label="轮询间隔" value={config.mail.wait_interval} min={1} onChange={(v) => updateMail({ wait_interval: v })} disabled={isRunning} />
            </div>

            {/* Provider list */}
            <div className="max-h-[240px] overflow-y-auto space-y-1.5">
              {config.mail.providers.map((provider, index) => (
                <div
                  key={index}
                  className="flex items-center gap-2 rounded-md border border-border bg-surface-2 px-3 py-2"
                >
                  <input
                    type="checkbox"
                    className="rounded border-border shrink-0"
                    checked={provider.enable}
                    onChange={(e) => {
                      const providers = [...config.mail.providers];
                      const p = providers[index];
                      if (!p) return;
                      providers[index] = { ...p, enable: e.target.checked };
                      updateMail({ providers });
                    }}
                    disabled={isRunning}
                  />
                  <span className="font-mono text-small text-text-secondary min-w-[100px] truncate">
                    {TYPE_LABELS[provider.type] || provider.type}
                  </span>
                  <span className="text-small text-text-tertiary truncate flex-1">
                    {provider.api_key ? (
                      <span className="font-mono">{provider.api_key.slice(0, 12)}···</span>
                    ) : (
                      '未配置 Key'
                    )}
                  </span>
                  <div className="flex items-center gap-0.5 shrink-0">
                    <button
                      type="button"
                      className="btn btn-ghost btn-icon btn-xs"
                      onClick={() => setProviderModal({ mode: 'edit', index, data: { ...provider } })}
                      title="编辑"
                    >
                      <Pencil size={13} />
                    </button>
                    {provider.type === 'tempmail_lol' && (
                      <button
                        type="button"
                        className="btn btn-ghost btn-icon btn-xs"
                        onClick={() => setImportModal({ index })}
                        title="导入 API Key"
                      >
                        <FileText size={13} />
                      </button>
                    )}
                    <button
                      type="button"
                      className="btn btn-danger-ghost btn-icon btn-xs"
                      onClick={() => deleteProvider(index)}
                      disabled={isRunning || config.mail.providers.length <= 1}
                      title="删除"
                    >
                      <Trash2 size={13} />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>
      </div>

      {/* Logs */}
      <section className="card card-section space-y-3">
        <header className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span className="grid place-items-center w-9 h-9 rounded-md bg-info-soft text-klein-500">
              <BarChart3 size={18} />
            </span>
            <div>
              <h2 className="text-h5 font-semibold text-text-primary">实时日志</h2>
              <p className="text-small text-text-tertiary mt-0.5">SSE 实时推送，最新日志在顶部。</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <span className="badge">{logs.length}</span>
            <button
              className="btn btn-ghost btn-icon btn-xs"
              onClick={() => setLogsExpanded(!logsExpanded)}
            >
              <Eye size={14} />
            </button>
          </div>
        </header>
        <div className={`overflow-y-auto rounded-md border border-border bg-surface-1 p-3 font-mono text-xs leading-6 ${logsExpanded ? 'max-h-[400px]' : 'max-h-[160px]'}`}>
          {logs.length === 0 ? (
            <div className="text-text-tertiary">暂无日志，启动注册任务后将显示实时日志。</div>
          ) : (
            logs.slice().reverse().map((item, index) => (
              <div
                key={`${item.time}-${index}`}
                className={
                  item.level === 'red' ? 'text-danger'
                    : item.level === 'green' ? 'text-success'
                    : item.level === 'yellow' ? 'text-warning'
                    : 'text-text-primary'
                }
              >
                <span className="text-text-tertiary">{new Date(item.time).toLocaleTimeString()}</span>
                <span className="pl-2">{item.text}</span>
              </div>
            ))
          )}
        </div>
      </section>

      {/* Modals */}
      {providerModal && (
        <ProviderEditModal
          data={providerModal.data}
          isRunning={isRunning}
          onClose={() => setProviderModal(null)}
          onSave={handleSaveProvider}
        />
      )}

      {importModal && (
        <ImportKeysModal
          onClose={() => setImportModal(null)}
          onImport={handleImportKeys}
        />
      )}
    </div>
  );
}

// ==================== Sub-components ====================

function StatCard({ label, value, accent }: { label: string; value: string; accent?: 'green' | 'red' | 'yellow' }) {
  const color = accent === 'green' ? 'text-success' : accent === 'red' ? 'text-danger' : accent === 'yellow' ? 'text-warning' : 'text-text-primary';
  return (
    <div className="rounded-lg border border-border bg-surface-1 px-3 py-2.5">
      <div className="text-xs text-text-tertiary">{label}</div>
      <div className={`mt-0.5 text-lg font-semibold ${color}`}>{value}</div>
    </div>
  );
}

function Field({ label, hint, className, children }: { label: string; hint?: React.ReactNode; className?: string; children: React.ReactNode }) {
  return (
    <label className={`field ${className || ''}`}>
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

function NumberField({ label, value, min = 0, max, onChange, disabled }: { label: string; value: number; min?: number; max?: number; onChange: (v: number) => void; disabled?: boolean }) {
  return (
    <Field label={label}>
      <input
        type="number"
        className="input"
        min={min}
        max={max}
        value={value}
        onChange={(e) => onChange(Number(e.target.value) || min)}
        disabled={disabled}
      />
    </Field>
  );
}

// ==================== Provider Edit Modal ====================

function ProviderEditModal({
  data,
  isRunning,
  onClose,
  onSave,
}: {
  data: MailProviderConfig;
  isRunning: boolean;
  onClose: () => void;
  onSave: (d: MailProviderConfig) => void;
}) {
  const [form, setForm] = useState<MailProviderConfig>({ ...data });

  const set = <K extends keyof MailProviderConfig>(k: K, v: MailProviderConfig[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-black/40 p-4 backdrop-blur-sm" onClick={onClose}>
      <div className="dialog-surface w-full max-w-xl klein-fade-in" onClick={(e) => e.stopPropagation()}>
        <header className="flex h-12 items-center justify-between border-b border-border px-5">
          <h3 className="font-semibold text-text-primary">
            {data.api_key ? '编辑 Provider' : '添加 Provider'}
          </h3>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose} aria-label="关闭">×</button>
        </header>
        <div className="max-h-[75vh] overflow-y-auto p-5">
          <div className="space-y-3">
            <Field label="类型">
              <select
                className="select"
                value={form.type}
                onChange={(e) => set('type', e.target.value)}
                disabled={isRunning}
              >
                {PROVIDER_TYPES.map((t) => (
                  <option key={t.value} value={t.value}>{t.label}</option>
                ))}
              </select>
            </Field>

            <div className="flex items-center justify-between gap-4 rounded-md border border-border bg-surface-2 p-3">
              <span className="text-small font-medium text-text-primary">启用</span>
              <button
                type="button"
                role="switch"
                aria-checked={form.enable}
                onClick={() => set('enable', !form.enable)}
                className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition ${form.enable ? 'bg-klein-500' : 'bg-surface-3'}`}
              >
                <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition transform ${form.enable ? 'translate-x-5' : 'translate-x-0.5'}`} />
              </button>
            </div>

            <Field label="API Base">
              <input className="input" value={form.api_base || ''} onChange={(e) => set('api_base', e.target.value)} placeholder="https://api.tempmail.lol" disabled={isRunning} />
            </Field>

            {form.type === 'tempmail_lol' && (
              <>
                <Field label="API Key" hint="一行一个，支持多个 Key 轮换">
                  <textarea
                    className="textarea font-mono text-small min-h-[80px]"
                    value={form.api_key || ''}
                    onChange={(e) => set('api_key', e.target.value)}
                    placeholder="sk-xxxx"
                    disabled={isRunning}
                  />
                </Field>
                <Field label="Domain" hint="每行一个，留空使用默认">
                  <textarea
                    className="textarea font-mono text-small min-h-[60px]"
                    value={(form.domain || []).join('\n')}
                    onChange={(e) => set('domain', e.target.value.split('\n').filter(Boolean))}
                    placeholder="example.com"
                    disabled={isRunning}
                  />
                </Field>
              </>
            )}

            {form.type === 'cloudflare_temp_email' && (
              <div className="grid gap-3 md:grid-cols-2">
                <Field label="CF Inbox JWT"><input className="input" value={form.cf_inbox_jwt || ''} onChange={(e) => set('cf_inbox_jwt', e.target.value)} disabled={isRunning} /></Field>
                <Field label="CF API Base"><input className="input" value={form.cf_api_base || ''} onChange={(e) => set('cf_api_base', e.target.value)} disabled={isRunning} /></Field>
                <Field label="CF API Key"><input className="input" value={form.cf_api_key || ''} onChange={(e) => set('cf_api_key', e.target.value)} disabled={isRunning} /></Field>
                <Field label="CF Auth Mode"><input className="input" value={form.cf_auth_mode || ''} onChange={(e) => set('cf_auth_mode', e.target.value)} disabled={isRunning} /></Field>
              </div>
            )}

            {form.type === 'ddg_mail' && (
              <Field label="DDG Token"><input className="input" value={form.ddg_token || ''} onChange={(e) => set('ddg_token', e.target.value)} disabled={isRunning} /></Field>
            )}

            {form.type === 'moemail' && (
              <div className="grid gap-3 md:grid-cols-2">
                <Field label="Admin Password"><input className="input" value={form.admin_password || ''} onChange={(e) => set('admin_password', e.target.value)} disabled={isRunning} /></Field>
                <Field label="Subdomain"><input className="input" value={form.subdomain || ''} onChange={(e) => set('subdomain', e.target.value)} disabled={isRunning} /></Field>
                <Field label="Default Domain"><input className="input" value={form.default_domain || ''} onChange={(e) => set('default_domain', e.target.value)} disabled={isRunning} /></Field>
              </div>
            )}

            {form.type === 'inbucket' && (
              <Field label="API Base"><input className="input" value={form.api_base || ''} onChange={(e) => set('api_base', e.target.value)} disabled={isRunning} /></Field>
            )}
          </div>
        </div>
        <footer className="flex justify-end gap-2 border-t border-border px-5 py-3">
          <button className="btn btn-outline btn-md" onClick={onClose}>取消</button>
          <button className="btn btn-primary btn-md" onClick={() => onSave(form)}>保存</button>
        </footer>
      </div>
    </div>
  );
}

// ==================== Import Keys Modal ====================

function ImportKeysModal({
  onClose,
  onImport,
}: {
  onClose: () => void;
  onImport: (keys: string[]) => void;
}) {
  const [text, setText] = useState('');
  const [fileLabel, setFileLabel] = useState('');

  const keyCount = useMemo(() => text.split(/[\n,;]+/).map((s) => s.trim()).filter(Boolean).length, [text]);

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setFileLabel(file.name);
    const content = await file.text();
    setText(content);
  };

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-black/40 p-4 backdrop-blur-sm" onClick={onClose}>
      <div className="dialog-surface w-full max-w-lg klein-fade-in" onClick={(e) => e.stopPropagation()}>
        <header className="flex h-12 items-center justify-between border-b border-border px-5">
          <h3 className="font-semibold text-text-primary">导入 API Key</h3>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose} aria-label="关闭">×</button>
        </header>
        <div className="p-5 space-y-3">
          <div className="flex items-center gap-2">
            <label className="btn btn-outline btn-sm cursor-pointer">
              <input type="file" accept=".txt,.json,.csv" className="hidden" onChange={handleFile} />
              选择文件
            </label>
            {fileLabel && <span className="text-small text-text-tertiary truncate">{fileLabel}</span>}
          </div>
          <Field label="API Key 列表" hint="每行一个，也支持逗号/分号分隔">
            <textarea
              className="textarea font-mono text-small min-h-[200px]"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={`sk-xxxx\nsk-yyyy\nsk-zzzz`}
            />
          </Field>
          {keyCount > 0 && (
            <div className="text-small text-text-tertiary">
              已识别 <span className="font-semibold text-text-primary">{keyCount}</span> 个 API Key
            </div>
          )}
        </div>
        <footer className="flex justify-end gap-2 border-t border-border px-5 py-3">
          <button className="btn btn-outline btn-md" onClick={onClose}>取消</button>
          <button
            className="btn btn-primary btn-md"
            disabled={keyCount === 0}
            onClick={() => {
              const keys = text.split(/[\n,;]+/).map((s) => s.trim()).filter(Boolean);
              onImport(keys);
            }}
          >
            导入 {keyCount > 0 ? `(${keyCount} 个)` : ''}
          </button>
        </footer>
      </div>
    </div>
  );
}
