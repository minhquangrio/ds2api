import { useCallback, useEffect, useState } from 'react'
import {
    Activity,
    AlertCircle,
    Check,
    CheckCircle2,
    Clock,
    Cpu,
    ExternalLink,
    Eye,
    Layers,
    Loader2,
    Plus,
    RefreshCw,
    Search,
    Shield,
    Trash2,
    Wrench,
    X,
    Zap,
} from 'lucide-react'
import clsx from 'clsx'

import { useI18n } from '../../i18n'

const hasCap = (model, cap) => Array.isArray(model?.capabilities) && model.capabilities.includes(cap)

export default function ProvidersContainer({ authFetch, onMessage }) {
    const { t } = useI18n()
    const [providers, setProviders] = useState([])
    const [loading, setLoading] = useState(false)
    const [syncingId, setSyncingId] = useState(null)
    const [inspectingId, setInspectingId] = useState(null)
    const [selectedProvider, setSelectedProvider] = useState(null)
    const [showModal, setShowModal] = useState(false)
    const [editingProvider, setEditingProvider] = useState(null)

    // Form state
    const [formData, setFormData] = useState({
        name: '',
        base_url: '',
        token: '',
    })
    const [saving, setSaving] = useState(false)

    const fetchProviders = useCallback(async () => {
        setLoading(true)
        try {
            const res = await authFetch('/admin/providers')
            if (res.ok) {
                const data = await res.json()
                setProviders(Array.isArray(data) ? data : [])
                // Update selectedProvider if it's currently open
                if (selectedProvider) {
                    const updated = data.find(p => p.id === selectedProvider.id)
                    if (updated) setSelectedProvider(updated)
                }
            }
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setLoading(false)
        }
    }, [authFetch, onMessage, selectedProvider])

    useEffect(() => {
        fetchProviders()
    }, [])

    const handleOpenAdd = () => {
        setEditingProvider(null)
        setFormData({ name: '', base_url: '', token: '' })
        setShowModal(true)
    }

    const handleOpenEdit = (p) => {
        setEditingProvider(p)
        setFormData({
            name: p.name || '',
            base_url: p.base_url || '',
            token: '',
        })
        setShowModal(true)
    }

    const handleSave = async (e) => {
        e.preventDefault()
        setSaving(true)
        try {
            const url = editingProvider
                ? `/admin/providers/${encodeURIComponent(editingProvider.id)}`
                : '/admin/providers'
            const method = editingProvider ? 'PUT' : 'POST'
            const payload = {
                name: formData.name,
                base_url: formData.base_url,
            }
            if (formData.token) {
                payload.token = formData.token
            }
            const res = await authFetch(url, {
                method,
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            })
            if (!res.ok) {
                const text = await res.text()
                throw new Error(text || 'Failed to save provider')
            }
            onMessage?.({ type: 'success', text: t('providers.savedSuccess') })
            setShowModal(false)
            fetchProviders()
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setSaving(false)
        }
    }

    const handleDelete = async (id) => {
        if (!window.confirm(t('providers.confirmDelete'))) return
        try {
            const res = await authFetch(`/admin/providers/${encodeURIComponent(id)}`, { method: 'DELETE' })
            if (!res.ok) throw new Error('Failed to delete')
            onMessage?.({ type: 'success', text: t('providers.deletedSuccess') })
            if (selectedProvider?.id === id) setSelectedProvider(null)
            fetchProviders()
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        }
    }

    const handleSync = async (id) => {
        setSyncingId(id)
        try {
            const res = await authFetch(`/admin/providers/${encodeURIComponent(id)}/sync`, { method: 'POST' })
            const data = await res.json()
            if (!res.ok) throw new Error(data.error || 'Sync failed')
            onMessage?.({
                type: 'success',
                text: `${t('providers.syncSuccess')}: ${data.models?.length || 0} models found`,
            })
            fetchProviders()
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setSyncingId(null)
        }
    }

    const handleInspect = async (id) => {
        setInspectingId(id)
        try {
            const res = await authFetch(`/admin/providers/${encodeURIComponent(id)}/inspect`, { method: 'POST' })
            const data = await res.json()
            if (!res.ok) throw new Error(data.error || 'Inspection failed')
            onMessage?.({ type: 'success', text: t('providers.inspectSuccess') })
            fetchProviders()
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setInspectingId(null)
        }
    }

    return (
        <div className="space-y-6 max-w-7xl mx-auto pb-12 animate-in fade-in duration-300">
            {/* Header */}
            <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm">
                <div className="flex items-center gap-3">
                    <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-amber-500 to-orange-600 flex items-center justify-center text-white shadow-lg shadow-orange-500/20">
                        <Layers className="w-5 h-5" />
                    </div>
                    <div>
                        <h2 className="text-xl font-bold tracking-tight text-foreground">{t('providers.title')}</h2>
                        <p className="text-sm text-muted-foreground">{t('providers.subtitle')}</p>
                    </div>
                </div>

                <div className="flex items-center gap-2.5 w-full sm:w-auto justify-end">
                    <button
                        onClick={fetchProviders}
                        disabled={loading}
                        className="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-secondary text-secondary-foreground text-xs font-medium hover:bg-secondary/80 transition"
                    >
                        <RefreshCw className={clsx("w-3.5 h-3.5", loading && "animate-spin")} />
                        <span>{t('common.refresh')}</span>
                    </button>
                    <button
                        onClick={handleOpenAdd}
                        className="inline-flex items-center gap-2 px-4 py-2 rounded-xl bg-primary text-primary-foreground font-medium text-sm hover:opacity-90 transition shadow-md shadow-primary/20"
                    >
                        <Plus className="w-4 h-4" />
                        <span>{t('providers.addBtn')}</span>
                    </button>
                </div>
            </div>

            {/* Providers Grid */}
            {loading && providers.length === 0 ? (
                <div className="min-h-[300px] flex items-center justify-center text-muted-foreground">
                    <Loader2 className="w-8 h-8 animate-spin text-primary" />
                </div>
            ) : providers.length === 0 ? (
                <div className="py-16 text-center text-muted-foreground border border-dashed border-border/80 rounded-2xl bg-card/40 space-y-3">
                    <Layers className="w-10 h-10 mx-auto text-muted-foreground/50" />
                    <p className="text-sm font-medium">{t('providers.empty')}</p>
                    <button
                        onClick={handleOpenAdd}
                        className="inline-flex items-center gap-1.5 px-4 py-2 rounded-xl bg-primary text-primary-foreground text-xs font-medium"
                    >
                        <Plus className="w-3.5 h-3.5" />
                        <span>{t('providers.addFirst')}</span>
                    </button>
                </div>
            ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                    {providers.map((p) => {
                        const modelCount = p.models?.length || 0
                        const isSyncing = syncingId === p.id
                        const isInspecting = inspectingId === p.id

                        return (
                            <div
                                key={p.id}
                                className="p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm flex flex-col justify-between gap-5 hover:border-primary/40 transition group"
                            >
                                <div className="space-y-3">
                                    <div className="flex items-start justify-between gap-2">
                                        <div>
                                            <h3 className="font-bold text-base text-foreground flex items-center gap-2">
                                                {p.name}
                                                <span className={clsx(
                                                    "w-2 h-2 rounded-full",
                                                    p.connection_status === 'connected'
                                                        ? "bg-emerald-400"
                                                        : p.connection_status === 'error'
                                                            ? "bg-rose-400"
                                                            : "bg-muted-foreground/40"
                                                )} title={p.connection_status || 'unknown'} />
                                            </h3>
                                            <div className="text-xs font-mono text-muted-foreground truncate max-w-[220px] mt-1" title={p.base_url}>
                                                {p.base_url}
                                            </div>
                                        </div>
                                        <span className="text-[11px] font-mono px-2 py-0.5 rounded-full bg-secondary text-secondary-foreground">
                                            {modelCount} models
                                        </span>
                                    </div>

                                    {/* Token info */}
                                    <div className="text-xs text-muted-foreground flex items-center justify-between pt-2 border-t border-border/40 font-mono">
                                        <span>Token:</span>
                                        <span>{p.has_token ? p.token_mask : t('providers.noToken')}</span>
                                    </div>

                                    {/* Capabilities overview */}
                                    {modelCount > 0 && (
                                        <div className="flex flex-wrap gap-1.5 pt-1">
                                            {p.models?.some(m => hasCap(m, 'tools')) && (
                                                <span className="inline-flex items-center gap-1 text-[10px] font-medium px-2 py-0.5 rounded-md bg-blue-500/10 text-blue-400 border border-blue-500/20">
                                                    <Wrench className="w-2.5 h-2.5" /> Tools
                                                </span>
                                            )}
                                            {p.models?.some(m => hasCap(m, 'reasoning')) && (
                                                <span className="inline-flex items-center gap-1 text-[10px] font-medium px-2 py-0.5 rounded-md bg-purple-500/10 text-purple-400 border border-purple-500/20">
                                                    <Zap className="w-2.5 h-2.5" /> Reasoning
                                                </span>
                                            )}
                                            {p.models?.some(m => hasCap(m, 'vision')) && (
                                                <span className="inline-flex items-center gap-1 text-[10px] font-medium px-2 py-0.5 rounded-md bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                                                    <Eye className="w-2.5 h-2.5" /> Vision
                                                </span>
                                            )}
                                        </div>
                                    )}
                                </div>

                                {/* Actions */}
                                <div className="space-y-2 pt-3 border-t border-border/60">
                                    <div className="grid grid-cols-2 gap-2 text-xs">
                                        <button
                                            onClick={() => handleSync(p.id)}
                                            disabled={isSyncing}
                                            className="inline-flex items-center justify-center gap-1.5 py-2 px-3 rounded-xl bg-secondary text-secondary-foreground hover:bg-secondary/80 font-medium transition"
                                        >
                                            <RefreshCw className={clsx("w-3.5 h-3.5", isSyncing && "animate-spin")} />
                                            <span>{isSyncing ? t('providers.syncing') : t('providers.syncModels')}</span>
                                        </button>
                                        <button
                                            onClick={() => handleInspect(p.id)}
                                            disabled={isInspecting || modelCount === 0}
                                            className="inline-flex items-center justify-center gap-1.5 py-2 px-3 rounded-xl bg-primary/10 text-primary hover:bg-primary/20 font-medium transition disabled:opacity-40"
                                        >
                                            <Activity className={clsx("w-3.5 h-3.5", isInspecting && "animate-spin")} />
                                            <span>{isInspecting ? t('providers.inspecting') : t('providers.inspect')}</span>
                                        </button>
                                    </div>

                                    <div className="flex items-center justify-between pt-1 text-xs">
                                        <button
                                            onClick={() => setSelectedProvider(p)}
                                            className="text-primary hover:underline font-medium"
                                        >
                                            {t('providers.viewModels')} ({modelCount})
                                        </button>
                                        <div className="flex items-center gap-2">
                                            <button
                                                onClick={() => handleOpenEdit(p)}
                                                className="text-muted-foreground hover:text-foreground text-[11px]"
                                            >
                                                {t('common.edit')}
                                            </button>
                                            <button
                                                onClick={() => handleDelete(p.id)}
                                                className="text-destructive hover:underline text-[11px]"
                                            >
                                                {t('common.delete')}
                                            </button>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        )
                    })}
                </div>
            )}

            {/* Model List Drawer / Modal */}
            {selectedProvider && (
                <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-background/80 backdrop-blur-sm animate-in fade-in">
                    <div className="relative w-full max-w-3xl max-h-[85vh] flex flex-col p-6 rounded-2xl bg-card border border-border shadow-2xl space-y-4">
                        <div className="flex items-center justify-between pb-3 border-b border-border">
                            <div>
                                <h3 className="font-bold text-lg text-foreground flex items-center gap-2">
                                    <Layers className="w-5 h-5 text-primary" />
                                    {selectedProvider.name} - Models Catalog
                                </h3>
                                <p className="text-xs text-muted-foreground font-mono">{selectedProvider.base_url}</p>
                            </div>
                            <button
                                onClick={() => setSelectedProvider(null)}
                                className="p-1 rounded-lg hover:bg-secondary text-muted-foreground"
                            >
                                <X className="w-5 h-5" />
                            </button>
                        </div>

                        <div className="flex-1 overflow-y-auto space-y-3 pr-1">
                            {selectedProvider.models?.length === 0 ? (
                                <div className="py-12 text-center text-muted-foreground text-sm">
                                    {t('providers.noModelsSynced')}
                                </div>
                            ) : (
                                selectedProvider.models.map((m) => (
                                    <div
                                        key={m.id}
                                        className="p-3.5 rounded-xl border border-border/60 bg-background/50 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs"
                                    >
                                        <div className="space-y-1">
                                            <div className="font-mono font-bold text-sm text-foreground">{m.id}</div>
                                            {m.capabilities_source && (
                                                <div className="text-[11px] text-muted-foreground">
                                                    {t('providers.capabilitiesSource')}: {m.capabilities_source}
                                                    {m.inspect_status ? ` / ${m.inspect_status}` : ''}
                                                </div>
                                            )}
                                        </div>

                                        <div className="flex items-center gap-2">
                                            {hasCap(m, 'tools') && (
                                                <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-blue-500/10 text-blue-400 border border-blue-500/20 font-medium">
                                                    <Wrench className="w-3 h-3" /> Tools
                                                </span>
                                            )}
                                            {hasCap(m, 'reasoning') && (
                                                <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-purple-500/10 text-purple-400 border border-purple-500/20 font-medium">
                                                    <Zap className="w-3 h-3" /> Reasoning
                                                </span>
                                            )}
                                            {hasCap(m, 'vision') && (
                                                <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-medium">
                                                    <Eye className="w-3 h-3" /> Vision
                                                </span>
                                            )}
                                            {m.context_window > 0 && (
                                                <span className="font-mono text-muted-foreground text-[11px] bg-secondary px-2 py-0.5 rounded">
                                                    {m.context_window.toLocaleString()} ctx
                                                </span>
                                            )}
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>

                        <div className="pt-3 border-t border-border flex justify-end">
                            <button
                                onClick={() => setSelectedProvider(null)}
                                className="px-4 py-2 rounded-xl bg-secondary text-secondary-foreground text-xs font-medium"
                            >
                                {t('common.close')}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Add / Edit Modal */}
            {showModal && (
                <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-background/80 backdrop-blur-sm animate-in fade-in">
                    <div className="relative w-full max-w-lg p-6 rounded-2xl bg-card border border-border shadow-2xl space-y-5">
                        <div className="flex items-center justify-between pb-3 border-b border-border">
                            <h3 className="font-bold text-base text-foreground">
                                {editingProvider ? t('providers.editTitle') : t('providers.addTitle')}
                            </h3>
                            <button
                                onClick={() => setShowModal(false)}
                                className="p-1 rounded-lg hover:bg-secondary text-muted-foreground"
                            >
                                <X className="w-5 h-5" />
                            </button>
                        </div>

                        <form onSubmit={handleSave} className="space-y-4 text-sm">
                            <div className="space-y-1">
                                <label className="text-xs font-semibold text-muted-foreground">{t('providers.nameLabel')}</label>
                                <input
                                    type="text"
                                    required
                                    value={formData.name}
                                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                                    placeholder="OpenRouter, SiliconFlow, vLLM..."
                                    className="w-full px-3 py-2 rounded-xl bg-background/50 border border-border focus:border-primary text-xs"
                                />
                            </div>

                            <div className="space-y-1">
                                <label className="text-xs font-semibold text-muted-foreground">{t('providers.urlLabel')}</label>
                                <input
                                    type="url"
                                    required
                                    value={formData.base_url}
                                    onChange={(e) => setFormData({ ...formData, base_url: e.target.value })}
                                    placeholder="https://api.openai.com/v1"
                                    className="w-full px-3 py-2 rounded-xl bg-background/50 border border-border focus:border-primary font-mono text-xs"
                                />
                                <p className="text-[11px] text-muted-foreground">{t('providers.urlHint')}</p>
                            </div>

                            <div className="space-y-1">
                                <label className="text-xs font-semibold text-muted-foreground">{t('providers.tokenLabel')}</label>
                                <input
                                    type="password"
                                    value={formData.token}
                                    onChange={(e) => setFormData({ ...formData, token: e.target.value })}
                                    placeholder={editingProvider?.has_token ? editingProvider.token_mask : 'sk-...'}
                                    className="w-full px-3 py-2 rounded-xl bg-background/50 border border-border focus:border-primary font-mono text-xs"
                                />
                            </div>

                            <div className="pt-3 flex justify-end gap-2 border-t border-border">
                                <button
                                    type="button"
                                    onClick={() => setShowModal(false)}
                                    className="px-4 py-2 rounded-xl border border-border hover:bg-secondary text-xs font-medium"
                                >
                                    {t('common.cancel')}
                                </button>
                                <button
                                    type="submit"
                                    disabled={saving}
                                    className="inline-flex items-center gap-1.5 px-4 py-2 rounded-xl bg-primary text-primary-foreground font-medium text-xs hover:opacity-90 disabled:opacity-50"
                                >
                                    {saving && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                                    <span>{saving ? t('common.saving') : t('common.save')}</span>
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            )}
        </div>
    )
}
