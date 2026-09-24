import { useState } from 'react'
import { X, Plus, Trash2 } from 'lucide-react'
import { v4 as uuidv4 } from 'uuid'
import clsx from 'clsx'

import { maskSecret } from '../../utils/maskSecret'

const SUGGESTED_MODELS = [
    'deepseek-v4-flash',
    'deepseek-v4-pro',
    'deepseek-v4-vision',
    'deepseek-v4-flash-search',
    'gemini-2.5-flash',
    'gemini-2.5-pro',
]

export default function AddKeyModal({
    show,
    t,
    editingKey,
    newKey,
    setNewKey,
    loading,
    onClose,
    onAdd,
    accounts = [],
}) {
    const [customModelInput, setCustomModelInput] = useState('')

    if (!show) {
        return null
    }

    const isEditing = Boolean(editingKey?.key)
    const displayKey = isEditing ? maskSecret(editingKey?.key || newKey.key) : newKey.key

    const selectedAccounts = Array.isArray(newKey.accounts) ? newKey.accounts : []
    const selectedModels = Array.isArray(newKey.models) ? newKey.models : []

    const toggleAccount = (accId) => {
        if (!accId) return
        const next = selectedAccounts.includes(accId)
            ? selectedAccounts.filter(id => id !== accId)
            : [...selectedAccounts, accId]
        setNewKey({ ...newKey, accounts: next })
    }

    const toggleModel = (model) => {
        if (!model) return
        const norm = model.toLowerCase().trim()
        const next = selectedModels.some(m => m.toLowerCase() === norm)
            ? selectedModels.filter(m => m.toLowerCase() !== norm)
            : [...selectedModels, norm]
        setNewKey({ ...newKey, models: next })
    }

    const addCustomModel = () => {
        const norm = customModelInput.toLowerCase().trim()
        if (!norm) return
        if (!selectedModels.some(m => m.toLowerCase() === norm)) {
            setNewKey({ ...newKey, models: [...selectedModels, norm] })
        }
        setCustomModelInput('')
    }

    const setQuickQuota = (tokens) => {
        setNewKey({ ...newKey, quota_tokens: tokens > 0 ? String(tokens) : '' })
    }

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4 animate-in fade-in overflow-y-auto">
            <div className="bg-card w-full max-w-lg rounded-xl border border-border shadow-2xl overflow-hidden animate-in zoom-in-95 my-8">
                <div className="p-4 border-b border-border flex justify-between items-center">
                    <h3 className="font-semibold">{isEditing ? t('accountManager.modalEditKeyTitle') : t('accountManager.modalAddKeyTitle')}</h3>
                    <button onClick={onClose} className="text-muted-foreground hover:text-foreground">
                        <X className="w-5 h-5" />
                    </button>
                </div>
                <div className="p-6 space-y-4 max-h-[80vh] overflow-y-auto">
                    {/* Key Input */}
                    <div>
                        <label className="block text-sm font-medium mb-1.5">{isEditing ? t('accountManager.keyLabel') : t('accountManager.newKeyLabel')}</label>
                        <div className="flex gap-2">
                            <input
                                type="text"
                                className={isEditing ? "input-field bg-muted/30 flex-1 cursor-not-allowed" : "input-field bg-background flex-1"}
                                placeholder={isEditing ? t('accountManager.keyReadonlyPlaceholder') : t('accountManager.newKeyPlaceholder')}
                                value={displayKey}
                                onChange={e => setNewKey({ ...newKey, key: e.target.value })}
                                autoFocus={!isEditing}
                                readOnly={isEditing}
                            />
                            {!isEditing && (
                                <button
                                    type="button"
                                    onClick={() => setNewKey({ ...newKey, key: 'sk-' + uuidv4().replace(/-/g, '') })}
                                    className="px-3 py-2 bg-secondary text-secondary-foreground rounded-lg hover:bg-secondary/80 transition-colors text-sm font-medium border border-border whitespace-nowrap"
                                >
                                    {t('accountManager.generate')}
                                </button>
                            )}
                        </div>
                        <p className="text-xs text-muted-foreground mt-1.5">
                            {isEditing ? t('accountManager.keyReadonlyHint') : t('accountManager.generateHint')}
                        </p>
                    </div>

                    {/* Name */}
                    <div>
                        <label className="block text-sm font-medium mb-1.5">{t('accountManager.nameOptional')}</label>
                        <input
                            type="text"
                            className="input-field"
                            placeholder={t('accountManager.namePlaceholder')}
                            value={newKey.name || ''}
                            onChange={e => setNewKey({ ...newKey, name: e.target.value })}
                            autoFocus={isEditing}
                        />
                    </div>

                    {/* Remark */}
                    <div>
                        <label className="block text-sm font-medium mb-1.5">{t('accountManager.remarkOptional')}</label>
                        <input
                            type="text"
                            className="input-field"
                            placeholder={t('accountManager.remarkPlaceholder')}
                            value={newKey.remark || ''}
                            onChange={e => setNewKey({ ...newKey, remark: e.target.value })}
                        />
                    </div>

                    {/* Allowed Accounts Multi-select */}
                    <div className="pt-1 border-t border-border/60">
                        <div className="flex items-center justify-between mb-1.5">
                            <label className="block text-sm font-medium">{t('accountManager.allowedAccountsLabel')}</label>
                            {selectedAccounts.length > 0 && (
                                <button
                                    type="button"
                                    onClick={() => setNewKey({ ...newKey, accounts: [] })}
                                    className="text-xs text-muted-foreground hover:text-destructive transition-colors"
                                >
                                    {t('accountManager.allAccounts')}
                                </button>
                            )}
                        </div>
                        <p className="text-xs text-muted-foreground mb-2">
                            {t('accountManager.allowedAccountsHint')}
                        </p>
                        {accounts && accounts.length > 0 ? (
                            <div className="flex flex-wrap gap-1.5 max-h-32 overflow-y-auto p-2 rounded-lg border border-border/60 bg-muted/20">
                                {accounts.map((acc, idx) => {
                                    const accId = acc.email || acc.mobile || acc.name || acc.token || `acc-${idx}`
                                    const isSelected = selectedAccounts.includes(accId)
                                    const label = acc.name
                                        ? `${acc.name} (${acc.email || acc.mobile || acc.pool_type || 'acc'})`
                                        : (acc.email || acc.mobile || acc.token?.slice(0, 10) || `Account #${idx + 1}`)
                                    return (
                                        <button
                                            key={accId}
                                            type="button"
                                            onClick={() => toggleAccount(accId)}
                                            className={clsx(
                                                "px-2.5 py-1 rounded-md text-xs font-medium border transition-colors",
                                                isSelected
                                                    ? "bg-primary text-primary-foreground border-primary"
                                                    : "bg-background text-muted-foreground border-border hover:bg-muted"
                                            )}
                                        >
                                            {label}
                                        </button>
                                    )
                                })}
                            </div>
                        ) : (
                            <p className="text-xs text-muted-foreground italic">
                                {t('accountManager.noAccounts')}
                            </p>
                        )}
                        <div className="text-xs text-muted-foreground mt-1">
                            {selectedAccounts.length === 0 ? (
                                <span className="text-emerald-500 font-medium">✓ {t('accountManager.allAccounts')}</span>
                            ) : (
                                <span>{t('accountManager.accountsSelected', { count: selectedAccounts.length })}</span>
                            )}
                        </div>
                    </div>

                    {/* Allowed Models Multi-select */}
                    <div className="pt-1 border-t border-border/60">
                        <div className="flex items-center justify-between mb-1.5">
                            <label className="block text-sm font-medium">{t('accountManager.allowedModelsLabel')}</label>
                            {selectedModels.length > 0 && (
                                <button
                                    type="button"
                                    onClick={() => setNewKey({ ...newKey, models: [] })}
                                    className="text-xs text-muted-foreground hover:text-destructive transition-colors"
                                >
                                    {t('accountManager.allModels')}
                                </button>
                            )}
                        </div>
                        <p className="text-xs text-muted-foreground mb-2">
                            {t('accountManager.allowedModelsHint')}
                        </p>
                        <div className="flex flex-wrap gap-1.5 mb-2">
                            {SUGGESTED_MODELS.map((model) => {
                                const isSelected = selectedModels.some(m => m.toLowerCase() === model.toLowerCase())
                                return (
                                    <button
                                        key={model}
                                        type="button"
                                        onClick={() => toggleModel(model)}
                                        className={clsx(
                                            "px-2 py-0.5 rounded text-xs font-mono font-medium border transition-colors",
                                            isSelected
                                                ? "bg-primary text-primary-foreground border-primary"
                                                : "bg-background text-muted-foreground border-border hover:bg-muted"
                                        )}
                                    >
                                        {model}
                                    </button>
                                )
                            })}
                        </div>
                        {/* Custom model input */}
                        <div className="flex gap-2">
                            <input
                                type="text"
                                className="input-field text-xs py-1 flex-1 font-mono"
                                placeholder="custom-model-id"
                                value={customModelInput}
                                onChange={e => setCustomModelInput(e.target.value)}
                                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addCustomModel() } }}
                            />
                            <button
                                type="button"
                                onClick={addCustomModel}
                                className="px-2.5 py-1 bg-secondary text-secondary-foreground rounded-lg hover:bg-secondary/80 text-xs font-medium border border-border"
                            >
                                <Plus className="w-3.5 h-3.5 inline mr-1" />
                                Add
                            </button>
                        </div>
                        {selectedModels.length > 0 && (
                            <div className="flex flex-wrap gap-1 mt-2">
                                {selectedModels.map((m) => (
                                    <span key={m} className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-mono bg-primary/10 text-primary border border-primary/20">
                                        {m}
                                        <button type="button" onClick={() => toggleModel(m)} className="hover:text-destructive">
                                            <X className="w-3 h-3" />
                                        </button>
                                    </span>
                                ))}
                            </div>
                        )}
                        <div className="text-xs text-muted-foreground mt-1">
                            {selectedModels.length === 0 ? (
                                <span className="text-emerald-500 font-medium">✓ {t('accountManager.allModels')}</span>
                            ) : (
                                <span>{t('accountManager.modelsSelected', { count: selectedModels.length })}</span>
                            )}
                        </div>
                    </div>

                    {/* Quota Tokens */}
                    <div className="pt-1 border-t border-border/60">
                        <label className="block text-sm font-medium mb-1.5">{t('accountManager.quotaTokensLabel')}</label>
                        <input
                            type="number"
                            min="0"
                            step="1000"
                            className="input-field"
                            placeholder={t('accountManager.quotaTokensPlaceholder')}
                            value={newKey.quota_tokens || ''}
                            onChange={e => setNewKey({ ...newKey, quota_tokens: e.target.value })}
                        />
                        <div className="flex items-center gap-1.5 mt-2">
                            <span className="text-xs text-muted-foreground mr-1">Quick:</span>
                            <button
                                type="button"
                                onClick={() => setQuickQuota(1000000)}
                                className="px-2 py-0.5 text-xs rounded border border-border bg-muted/40 hover:bg-muted text-muted-foreground"
                            >
                                1M
                            </button>
                            <button
                                type="button"
                                onClick={() => setQuickQuota(5000000)}
                                className="px-2 py-0.5 text-xs rounded border border-border bg-muted/40 hover:bg-muted text-muted-foreground"
                            >
                                5M
                            </button>
                            <button
                                type="button"
                                onClick={() => setQuickQuota(10000000)}
                                className="px-2 py-0.5 text-xs rounded border border-border bg-muted/40 hover:bg-muted text-muted-foreground"
                            >
                                10M
                            </button>
                            <button
                                type="button"
                                onClick={() => setQuickQuota(0)}
                                className="px-2 py-0.5 text-xs rounded border border-border bg-muted/40 hover:bg-muted text-muted-foreground"
                            >
                                {t('accountManager.unlimitedQuota')}
                            </button>
                        </div>
                        <p className="text-xs text-muted-foreground mt-1.5">
                            {t('accountManager.quotaTokensHint')}
                        </p>
                    </div>

                    {/* Tools Enabled */}
                    <div className="pt-1 border-t border-border/60">
                        <label className="flex items-center gap-3 cursor-pointer">
                            <button
                                type="button"
                                role="switch"
                                aria-checked={!!newKey.tools_enabled}
                                onClick={() => setNewKey({ ...newKey, tools_enabled: !newKey.tools_enabled })}
                                className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-card ${newKey.tools_enabled ? 'border-primary bg-primary' : 'border-border bg-muted'}`}
                            >
                                <span className={`inline-block h-4 w-4 transform rounded-full shadow-sm transition-transform ${newKey.tools_enabled ? 'translate-x-6 bg-primary-foreground' : 'translate-x-1 bg-muted-foreground'}`} />
                            </button>
                            <span className="text-sm font-medium">{t('accountManager.toolsEnabledLabel')}</span>
                        </label>
                        <p className="text-xs text-amber-500/90 mt-1.5">{t('accountManager.toolsEnabledHint')}</p>
                    </div>

                    {/* Actions */}
                    <div className="flex justify-end gap-2 pt-2 border-t border-border">
                        <button onClick={onClose} className="px-4 py-2 rounded-lg border border-border hover:bg-secondary transition-colors text-sm font-medium">{t('actions.cancel')}</button>
                        <button onClick={onAdd} disabled={loading} className="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors text-sm font-medium disabled:opacity-50">
                            {loading
                                ? (isEditing ? t('accountManager.editKeyLoading') : t('accountManager.addKeyLoading'))
                                : (isEditing ? t('accountManager.editKeyAction') : t('accountManager.addKeyAction'))}
                        </button>
                    </div>
                </div>
            </div>
        </div>
    )
}
