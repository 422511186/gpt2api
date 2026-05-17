import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import {
  AlertTriangle,
  LoaderCircle,
  Play,
  RotateCcw,
  Save,
  Square,
  UserPlus,
  Plus,
  Trash2,
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

function Field({
  label,
  hint,
  className,
  children,
}: {
  label: string;
  hint?: React.ReactNode;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <label className={`field ${className || ''}`}>
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

export default function RegisterPage() {
  const [config, setConfig] = useState<RegisterConfig | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  // Load initial config
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
        const data = JSON.parse(event.data) as RegisterConfig;
        setConfig(data);
      } catch {
        // Ignore parse errors
      }
    };

    source.onerror = () => {
      source.close();
      if (!closed) {
        setTimeout(() => {
          if (!closed) {
            const stored2 = localStorage.getItem(TOKEN_KEY_FOR_SSE);
            if (!stored2) return;
            const tok2 = JSON.parse(stored2);
            const token2 = tok2.access as string;
            if (!token2) return;
            const newURL = `${baseURL}/register/events?token=${encodeURIComponent(token2)}`;
            const newSource = new EventSource(newURL);
            newSource.onmessage = (e: MessageEvent) => {
              try {
                setConfig(JSON.parse(e.data));
              } catch {}
            };
          }
        }, 3000);
      }
    };

    return () => {
      closed = true;
      source.close();
    };
  }, []);

  // Save config
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

  // Start registration
  const startMut = useMutation({
    mutationFn: () => registerApi.start(),
    onSuccess: (res) => {
      setConfig(res.register);
      toast.success('注册任务已启动');
    },
    onError: (e: ApiError) => toast.error(e.message),
  });

  // Stop registration
  const stopMut = useMutation({
    mutationFn: () => registerApi.stop(),
    onSuccess: (res) => {
      setConfig(res.register);
      toast.success('注册任务已停止');
    },
    onError: (e: ApiError) => toast.error(e.message),
  });

  // Reset stats
  const resetMut = useMutation({
    mutationFn: () => registerApi.reset(),
    onSuccess: (res) => {
      setConfig(res.register);
      toast.success('统计数据已重置');
    },
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
    success: 0,
    fail: 0,
    done: 0,
    running: 0,
    threads: config.threads,
    elapsed_seconds: 0,
    avg_seconds: 0,
    success_rate: 0,
    current_quota: 0,
    current_available: 0,
  };

  const logs = config.logs || [];

  const updateConfig = (updates: Partial<RegisterConfig>) => {
    setConfig((prev) => prev ? { ...prev, ...updates } : prev);
  };

  const updateMail = (updates: Partial<RegisterConfig['mail']>) => {
    setConfig((prev) => prev ? { ...prev, mail: { ...prev.mail, ...updates } } : prev);
  };

  const addProvider = () => {
    setConfig((prev) => {
      if (!prev) return prev;
      const newProvider: MailProviderConfig = {
        type: 'tempmail_lol',
        enable: true,
      };
      return {
        ...prev,
        mail: {
          ...prev.mail,
          providers: [...prev.mail.providers, newProvider],
        },
      };
    });
  };

  const updateProvider = (index: number, updates: Partial<MailProviderConfig>) => {
    setConfig((prev) => {
      if (!prev) return prev;
      const providers = [...prev.mail.providers];
      const orig = providers[index];
      if (!orig) return prev;
      providers[index] = {
        ...orig,
        ...updates,
        type: orig.type,
        enable: updates.enable !== undefined ? updates.enable : orig.enable,
      };
      return {
        ...prev,
        mail: {
          ...prev.mail,
          providers,
        },
      };
    });
  };

  const deleteProvider = (index: number) => {
    setConfig((prev) => {
      if (!prev) return prev;
      const providers = prev.mail.providers.filter((_, i) => i !== index);
      return {
        ...prev,
        mail: {
          ...prev.mail,
          providers,
        },
      };
    });
  };

  return (
    <div className="page page-wide space-y-4">
      <header className="page-header">
        <div>
          <h1 className="page-title">ChatGPT 注册机</h1>
          <p className="page-subtitle">自动注册 OpenAI 账号并获取 OAuth Token，写入 Token 管理。</p>
        </div>
      </header>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* Left: Configuration */}
        <section className="card space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <div className="flex size-9 items-center justify-center rounded-md bg-surface-2">
                <UserPlus className="size-5 text-text-secondary" />
              </div>
              <h2 className="text-lg font-semibold tracking-tight">注册配置</h2>
            </div>
            <button
              className="btn btn-primary btn-sm"
              onClick={() => void saveConfig()}
              disabled={isSaving || config.enabled}
            >
              {isSaving ? <LoaderCircle className="size-4 animate-spin" /> : <Save className="size-4" />}
              保存配置
            </button>
          </div>

          <div className="grid gap-4 md:grid-cols-3">
            <Field label="注册模式">
              <select
                className="select select-sm"
                value={config.mode}
                onChange={(e) => updateConfig({ mode: e.target.value as 'total' | 'quota' | 'available' })}
                disabled={config.enabled}
              >
                <option value="total">注册总数</option>
                <option value="quota">号池剩余额度</option>
                <option value="available">可用账号数量</option>
              </select>
            </Field>
            <Field label="注册总数">
              <input
                type="number"
                className="input input-sm"
                value={config.total}
                onChange={(e) => updateConfig({ total: Number(e.target.value) || 10 })}
                disabled={config.enabled || config.mode !== 'total'}
              />
            </Field>
            <Field label="线程数">
              <input
                type="number"
                className="input input-sm"
                value={config.threads}
                onChange={(e) => updateConfig({ threads: Number(e.target.value) || 3 })}
                disabled={config.enabled}
              />
            </Field>
            <Field label="注册代理">
              <input
                className="input input-sm"
                value={config.proxy}
                onChange={(e) => updateConfig({ proxy: e.target.value })}
                placeholder="http://127.0.0.1:7890"
                disabled={config.enabled}
              />
            </Field>
            <Field label="目标剩余额度">
              <input
                type="number"
                className="input input-sm"
                value={config.target_quota}
                onChange={(e) => updateConfig({ target_quota: Number(e.target.value) || 100 })}
                disabled={config.enabled || config.mode !== 'quota'}
              />
            </Field>
            <Field label="目标可用账号">
              <input
                type="number"
                className="input input-sm"
                value={config.target_available}
                onChange={(e) => updateConfig({ target_available: Number(e.target.value) || 10 })}
                disabled={config.enabled || config.mode !== 'available'}
              />
            </Field>
            <Field label="检查间隔（秒）">
              <input
                type="number"
                className="input input-sm"
                value={config.check_interval}
                onChange={(e) => updateConfig({ check_interval: Number(e.target.value) || 5 })}
                disabled={config.enabled || config.mode === 'total'}
              />
            </Field>
          </div>

          {/* Mail providers */}
          <div className="space-y-3 border-t border-border pt-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <h3 className="text-sm font-semibold text-text-primary">邮箱配置</h3>
                <p className="mt-1 text-xs text-text-tertiary">可配置多个 provider，按启用顺序轮换。</p>
              </div>
              <button
                type="button"
                className="btn btn-outline btn-sm"
                onClick={addProvider}
                disabled={config.enabled}
              >
                <Plus className="size-4" />
                添加
              </button>
            </div>

            <div className="grid gap-4 md:grid-cols-3">
              <Field label="请求超时">
                <input
                  type="number"
                  className="input input-sm"
                  value={config.mail.request_timeout}
                  onChange={(e) => updateMail({ request_timeout: Number(e.target.value) || 30 })}
                  disabled={config.enabled}
                />
              </Field>
              <Field label="等待验证码超时">
                <input
                  type="number"
                  className="input input-sm"
                  value={config.mail.wait_timeout}
                  onChange={(e) => updateMail({ wait_timeout: Number(e.target.value) || 30 })}
                  disabled={config.enabled}
                />
              </Field>
              <Field label="轮询间隔">
                <input
                  type="number"
                  className="input input-sm"
                  value={config.mail.wait_interval}
                  onChange={(e) => updateMail({ wait_interval: Number(e.target.value) || 2 })}
                  disabled={config.enabled}
                />
              </Field>
            </div>

            {config.mail.providers.map((provider, index) => (
              <div key={index} className="space-y-3 border-t border-border pt-3">
                <div className="flex items-center justify-between gap-3">
                  <label className="flex items-center gap-3 text-sm text-text-primary">
                    <input
                      type="checkbox"
                      className="rounded border-border"
                      checked={provider.enable}
                      onChange={(e) => updateProvider(index, { enable: e.target.checked })}
                      disabled={config.enabled}
                    />
                    启用
                  </label>
                  <button
                    type="button"
                    className="btn btn-danger-ghost btn-icon btn-sm"
                    onClick={() => deleteProvider(index)}
                    disabled={config.enabled || config.mail.providers.length <= 1}
                    title="删除 provider"
                  >
                    <Trash2 className="size-4" />
                  </button>
                </div>

                <div className="grid gap-4 md:grid-cols-2">
                  <Field label="类型">
                    <select
                      className="select select-sm"
                      value={provider.type}
                      onChange={(e) => updateProvider(index, { type: e.target.value })}
                      disabled={config.enabled}
                    >
                      <option value="tempmail_lol">TempMail.lol</option>
                      <option value="cloudflare_temp_email">CloudflareTempMail</option>
                      <option value="moemail">MoEmail</option>
                      <option value="ddg_mail">DDGMail</option>
                      <option value="inbucket">Inbucket</option>
                    </select>
                  </Field>
                  {provider.type === 'tempmail_lol' && (
                    <Field label="API Key">
                      <input
                        className="input input-sm"
                        value={provider.api_key || ''}
                        onChange={(e) => updateProvider(index, { api_key: e.target.value })}
                        disabled={config.enabled}
                      />
                    </Field>
                  )}
                  {provider.type === 'tempmail_lol' && (
                    <Field label="Domain">
                      <textarea
                        className="textarea min-h-[60px] font-mono text-small"
                        value={(provider.domain || []).join('\n')}
                        onChange={(e) => updateProvider(index, {
                          domain: e.target.value.split('\n').filter(Boolean),
                        })}
                        placeholder="每行一个域名，留空使用默认"
                        disabled={config.enabled}
                      />
                    </Field>
                  )}
                </div>
              </div>
            ))}
          </div>
        </section>

        {/* Right: Status */}
        <section className="card space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h2 className="text-lg font-semibold tracking-tight">运行结果</h2>
              <p className="mt-1 text-sm text-text-tertiary">SSE 实时推送当前状态。</p>
            </div>
            <span className={`badge ${config.enabled ? 'badge-success' : 'badge'}`}>
              {config.enabled ? '运行中' : '已停止'}
            </span>
          </div>

          <div className="grid grid-cols-4 gap-2">
            {[
              ['成功 / 成功率', `${stats.success} / ${stats.success_rate || 0}%`],
              ['失败', stats.fail],
              ['完成', stats.done],
              ['运行 / 线程', `${stats.running} / ${stats.threads}`],
              ['运行时间', `${stats.elapsed_seconds || 0}s`],
              ['平均注册单个', `${stats.avg_seconds || 0}s`],
              ['当前额度', stats.current_quota || 0],
              ['正常账号', stats.current_available || 0],
            ].map(([label, value]) => (
              <div key={label} className="border border-border bg-surface-1 px-3 py-2">
                <div className="text-xs text-text-tertiary">{label}</div>
                <div className="mt-1 text-base font-semibold text-text-primary">{String(value)}</div>
              </div>
            ))}
          </div>

          <div className="grid grid-cols-3 gap-2">
            <button
              className="btn btn-primary btn-sm"
              onClick={() => startMut.mutate()}
              disabled={isSaving || startMut.isPending || config.enabled}
            >
              {startMut.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Play className="size-4" />}
              启动
            </button>
            <button
              className="btn btn-outline btn-sm"
              onClick={() => stopMut.mutate()}
              disabled={isSaving || stopMut.isPending || !config.enabled}
            >
              {stopMut.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Square className="size-4" />}
              停止
            </button>
            <button
              className="btn btn-outline btn-sm"
              onClick={() => resetMut.mutate()}
              disabled={isSaving || resetMut.isPending || config.enabled}
            >
              <RotateCcw className="size-4" />
              重置
            </button>
          </div>

          <div className="flex items-center gap-2 border border-warning bg-warning/10 px-3 py-2 text-xs text-warning">
            <AlertTriangle className="size-4 shrink-0" />
            启动之前注意先保存配置。
          </div>

          {/* Logs */}
          <div className="space-y-3 border-t border-border pt-4">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold text-text-primary">实时日志</h3>
              <span className="badge">{logs.length}</span>
            </div>
            <div className="max-h-[300px] overflow-y-auto border border-border bg-surface-1 p-3 font-mono text-xs leading-6">
              {logs.length === 0 ? (
                <div className="text-text-tertiary">暂无日志</div>
              ) : (
                logs.slice().reverse().map((item, index) => (
                  <div
                    key={`${item.time}-${index}`}
                    className={
                      item.level === 'red'
                        ? 'text-danger'
                        : item.level === 'green'
                        ? 'text-success'
                        : item.level === 'yellow'
                        ? 'text-warning'
                        : 'text-text-primary'
                    }
                  >
                    <span className="text-text-tertiary">{new Date(item.time).toLocaleTimeString()}</span>
                    <span className="pl-2">{item.text}</span>
                  </div>
                ))
              )}
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}