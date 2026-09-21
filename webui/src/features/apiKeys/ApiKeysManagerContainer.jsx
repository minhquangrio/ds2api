import { useState, useMemo, useCallback } from 'react'
import {
    Key,
    Plus,
    Copy,
    Check,
    Pencil,
    Trash2,
    Eye,
    EyeOff,
    Search,
    Wrench,
    Shield,
    BookOpen,
    ExternalLink,
    Terminal,
} from 'lucide-react'
import clsx from 'clsx'

import { useI18n } from '../../i18n'
import { maskSecret } from '../../utils/maskSecret'
import AddKeyModal from '../account/AddKeyModal'

export default function ApiKeysManagerContainer({ config, onRefresh, onMessage, authFetch }) {
    const { t } = useI18n()
    const apiFetch = authFetch || fetch

    const [searchQuery, setSearchQuery] = useState('')
    const [copiedKey, setCopiedKey] = useState(null)
    const [revealedKeys, setRevealedKeys] = useState({})
    const [loading, setLoading] = useState(false)

    // Modal state
    const [showModal, setShowModal] = useState(false)
    const [editingKey, setEditingKey] = useState(null)
    const [keyForm, setKeyForm] = useState({ key: '', name: '', remark: '', tools_enabled: false })

    // Normalize keys from config
    const normalizedKeys = useMemo(() => {
        const raw = config?.keys || []
        return raw.map((item, index) => {
            if (typeof item === 'string') {
                return {
                    id: index,
                    key: item,
                    name: '',
                    remark: '',
                    tools_enabled: false,
                    isDefault: index === 0,
                }
            }
            return {
                id: index,
                key: item.key || '',
                name: item.name || '',
                remark: item.remark || '',
                tools_enabled: Boolean(item.tools_enabled),
                isDefault: index === 0,
            }
        }).filter((k) => Boolean(k.key))
    }, [config?.keys])

    const filteredKeys = useMemo(() => {
        if (!searchQuery.trim()) return normalizedKeys
        const q = searchQuery.toLowerCase().trim()
        return normalizedKeys.filter((k) =>
            k.key.toLowerCase().includes(q) ||
            k.name.toLowerCase().includes(q) ||
            k.remark.toLowerCase().includes(q)
        )
    }, [normalizedKeys, searchQuery])

    const toggleReveal = (keyStr) => {
        setRevealedKeys((prev) => ({
            ...prev,
            [keyStr]: !prev[keyStr],
        }))
    }

    const copyKey = (keyStr) => {
        navigator.clipboard.writeText(keyStr).then(() => {
            setCopiedKey(keyStr)
            setTimeout(() => setCopiedKey(null), 1800)
        })
    }

    const openAddModal = () => {
        setEditingKey(null)
        setKeyForm({ key: '', name: '', remark: '', tools_enabled: false })
        setShowModal(true)
    }

    const openEditModal = (item) => {
        setEditingKey(item)
        setKeyForm({
            key: item.key,
            name: item.name,
            remark: item.remark,
            tools_enabled: item.tools_enabled,
        })
        setShowModal(true)
    }

    const closeModal = () => {
        setShowModal(false)
        setEditingKey(null)
    }

    const handleSaveKey = async () => {
        const isEditing = Boolean(editingKey?.key)
        if (!isEditing && !keyForm.key.trim()) return

        setLoading(true)
        try {
            const endpoint = isEditing
                ? `/admin/keys/${encodeURIComponent(editingKey.key)}`
                : '/admin/keys'
            const method = isEditing ? 'PUT' : 'POST'
            const payload = isEditing
                ? { name: keyForm.name, remark: keyForm.remark, tools_enabled: keyForm.tools_enabled }
                : { key: keyForm.key.trim(), name: keyForm.name, remark: keyForm.remark, tools_enabled: keyForm.tools_enabled }

            const res = await apiFetch(endpoint, {
                method,
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            })

            if (res.ok) {
                onMessage('success', isEditing ? t('accountManager.updateKeySuccess') : t('accountManager.addKeySuccess'))
                closeModal()
                if (onRefresh) onRefresh()
            } else {
                const data = await res.json()
                onMessage('error', data.detail || (isEditing ? t('messages.requestFailed') : t('messages.failedToAdd')))
            }
        } catch (_err) {
            onMessage('error', t('messages.networkError'))
        } finally {
            setLoading(false)
        }
    }

    const handleDeleteKey = async (keyStr) => {
        if (!confirm(t('apiKeysManager.confirmDelete') || t('accountManager.deleteKeyConfirm'))) return

        try {
            const res = await apiFetch(`/admin/keys/${encodeURIComponent(keyStr)}`, { method: 'DELETE' })
            if (res.ok) {
                onMessage('success', t('messages.deleted'))
                if (onRefresh) onRefresh()
            } else {
                onMessage('error', t('messages.deleteFailed'))
            }
        } catch (_err) {
            onMessage('error', t('messages.networkError'))
        }
    }

    const baseUrl = typeof window !== 'undefined'
        ? `${window.location.protocol}//${window.location.host}/v1`
        : 'http://localhost:5001/v1'

    return (
        <div className="space-y-6 pb-12">
            {/* Header */}
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold tracking-tight text-foreground flex items-center gap-3">
                        {t('apiKeysManager.title')}
                        <span className="text-xs font-mono font-medium px-2.5 py-0.5 rounded-full bg-primary/10 text-primary border border-primary/20">
                            {normalizedKeys.length} {t('sidebar.keys').toLowerCase()}
                        </span>
                    </h1>
                    <p className="text-sm text-muted-foreground mt-1">
                        {t('apiKeysManager.desc')}
                    </p>
                </div>

                <div className="flex items-center gap-3">
                    <button
                        onClick={openAddModal}
                        className="inline-flex items-center gap-2 px-4 py-2 rounded-xl text-xs font-semibold bg-primary text-primary-foreground hover:opacity-90 transition-all shadow-sm"
                    >
                        <Plus className="w-4 h-4" />
                        {t('apiKeysManager.createKey')}
                    </button>
                </div>
            </div>

            {/* Quick Status Notice */}
            <div className="p-4 rounded-xl border border-border/80 bg-card/70 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs">
                <div className="flex items-center gap-2.5 text-muted-foreground">
                    <Shield className="w-4 h-4 text-emerald-400 shrink-0" />
                    <span>{t('apiKeysManager.defaultKeyNotice')}</span>
                </div>
                <div className="flex items-center gap-2 text-foreground font-mono">
                    <span className="text-muted-foreground">Endpoint:</span>
                    <span className="px-2 py-1 rounded bg-muted/60 border border-border/60 select-all">{baseUrl}</span>
                </div>
            </div>

            {/* Search & Actions Bar */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div className="relative flex-1 max-w-sm">
                    <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
                    <input
                        type="text"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                        placeholder="Search keys by name, remark, or string..."
                        className="w-full pl-9 pr-3 py-2 text-xs bg-card/80 border border-border rounded-xl focus:outline-none focus:ring-1 focus:ring-primary placeholder:text-muted-foreground text-foreground"
                    />
                </div>
                <div className="text-xs text-muted-foreground">
                    {t('apiKeysManager.keyCount', { count: filteredKeys.length })}
                </div>
            </div>

            {/* Keys Table Card */}
            <div className="rounded-2xl border border-border/80 bg-card/90 shadow-sm overflow-hidden">
                {filteredKeys.length > 0 ? (
                    <div className="overflow-x-auto">
                        <table className="w-full text-left text-xs">
                            <thead className="bg-muted/40 border-b border-border/60 text-muted-foreground uppercase tracking-wider font-semibold">
                                <tr>
                                    <th className="px-5 py-3.5">Key</th>
                                    <th className="px-4 py-3.5">Name / Label</th>
                                    <th className="px-4 py-3.5">Remark</th>
                                    <th className="px-4 py-3.5">Tools</th>
                                    <th className="px-4 py-3.5 text-right">Actions</th>
                                </tr>
                            </thead>
                            <tbody className="divide-y divide-border/60 font-sans">
                                {filteredKeys.map((item) => {
                                    const isRevealed = Boolean(revealedKeys[item.key])
                                    const displayKey = isRevealed ? item.key : maskSecret(item.key)
                                    const isCopied = copiedKey === item.key

                                    return (
                                        <tr key={item.key} className="hover:bg-muted/30 transition-colors">
                                            {/* Key String Column */}
                                            <td className="px-5 py-4">
                                                <div className="flex items-center gap-2 font-mono">
                                                    {item.isDefault && (
                                                        <span className="px-1.5 py-0.5 rounded text-[10px] font-sans font-semibold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                                                            Default
                                                        </span>
                                                    )}
                                                    <span className="text-foreground font-medium select-all">
                                                        {displayKey}
                                                    </span>
                                                    <button
                                                        type="button"
                                                        onClick={() => toggleReveal(item.key)}
                                                        className="p-1 rounded text-muted-foreground hover:text-foreground transition-colors"
                                                        title={isRevealed ? 'Hide secret' : 'Reveal secret'}
                                                    >
                                                        {isRevealed ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                                                    </button>
                                                    <button
                                                        type="button"
                                                        onClick={() => copyKey(item.key)}
                                                        className="p-1 rounded text-muted-foreground hover:text-primary transition-colors"
                                                        title="Copy API key"
                                                    >
                                                        {isCopied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                                                    </button>
                                                </div>
                                            </td>

                                            {/* Name / Label */}
                                            <td className="px-4 py-4 text-foreground font-medium">
                                                {item.name || <span className="text-muted-foreground italic">—</span>}
                                            </td>

                                            {/* Remark */}
                                            <td className="px-4 py-4 text-muted-foreground">
                                                {item.remark || '—'}
                                            </td>

                                            {/* Tools Capability */}
                                            <td className="px-4 py-4">
                                                {item.tools_enabled ? (
                                                    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-medium bg-purple-500/10 text-purple-400 border border-purple-500/20">
                                                        <Wrench className="w-3 h-3" />
                                                        Enabled
                                                    </span>
                                                ) : (
                                                    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-medium text-muted-foreground">
                                                        Default
                                                    </span>
                                                )}
                                            </td>

                                            {/* Actions */}
                                            <td className="px-4 py-4 text-right">
                                                <div className="flex items-center justify-end gap-1.5">
                                                    <button
                                                        type="button"
                                                        onClick={() => openEditModal(item)}
                                                        className="p-1.5 rounded-lg border border-border text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors"
                                                        title="Edit details"
                                                    >
                                                        <Pencil className="w-3.5 h-3.5" />
                                                    </button>
                                                    <button
                                                        type="button"
                                                        onClick={() => handleDeleteKey(item.key)}
                                                        className="p-1.5 rounded-lg border border-border text-muted-foreground hover:text-destructive hover:border-destructive/30 hover:bg-destructive/10 transition-colors"
                                                        title="Delete key"
                                                    >
                                                        <Trash2 className="w-3.5 h-3.5" />
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    )
                                })}
                            </tbody>
                        </table>
                    </div>
                ) : (
                    <div className="py-16 text-center text-muted-foreground">
                        <Key className="w-8 h-8 mx-auto mb-3 opacity-30" />
                        <p className="font-semibold text-foreground text-sm">No API keys found</p>
                        <p className="text-xs mt-1">Create an API key above to connect your clients.</p>
                    </div>
                )}
            </div>

            {/* Integration Guide Box */}
            <div className="p-6 rounded-2xl border border-border/80 bg-card/70 space-y-3">
                <div className="flex items-center gap-2 text-sm font-bold text-foreground">
                    <BookOpen className="w-4 h-4 text-primary" />
                    {t('apiKeysManager.integrationGuideTitle')}
                </div>
                <p className="text-xs text-muted-foreground leading-relaxed">
                    {t('apiKeysManager.integrationGuideText')}
                </p>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-3 pt-2 text-xs">
                    <div className="p-3 rounded-xl border border-border/60 bg-background/60">
                        <div className="font-semibold text-foreground">Cursor & Claude Code</div>
                        <div className="text-[11px] text-muted-foreground mt-1">
                            Set OpenAI API Key to your DS2API key, and Override OpenAI Base URL to <code className="text-primary font-mono">{baseUrl}</code>.
                        </div>
                    </div>
                    <div className="p-3 rounded-xl border border-border/60 bg-background/60">
                        <div className="font-semibold text-foreground">NextChat & LibreChat</div>
                        <div className="text-[11px] text-muted-foreground mt-1">
                            Select Custom OpenAI Endpoint, enter <code className="text-primary font-mono">{baseUrl}</code> and your chosen API key.
                        </div>
                    </div>
                    <div className="p-3 rounded-xl border border-border/60 bg-background/60">
                        <div className="font-semibold text-foreground">Official OpenAI SDKs</div>
                        <div className="text-[11px] text-muted-foreground mt-1">
                            Initialize <code className="text-primary font-mono">OpenAI(api_key="...", base_url="{baseUrl}")</code> directly in your code.
                        </div>
                    </div>
                </div>
            </div>

            {/* Add / Edit Key Modal */}
            <AddKeyModal
                show={showModal}
                t={t}
                editingKey={editingKey}
                newKey={keyForm}
                setNewKey={setKeyForm}
                loading={loading}
                onClose={closeModal}
                onAdd={handleSaveKey}
            />
        </div>
    )
}
