import { useState } from 'react'
import { X, Copy, Check } from 'lucide-react'

const EXTRACT_SCRIPT = `copy(JSON.stringify({token: localStorage.getItem('userToken'), cookies: document.cookie}))`

export default function EditAccountModal({
    show,
    t,
    editingAccount,
    editAccount,
    setEditAccount,
    loading,
    onClose,
    onSave,
}) {
    const [copiedScript, setCopiedScript] = useState(false)

    if (!show || !editingAccount) {
        return null
    }

    const handleCopyScript = () => {
        navigator.clipboard.writeText(EXTRACT_SCRIPT)
        setCopiedScript(true)
        setTimeout(() => setCopiedScript(false), 2000)
    }

    const isGemini = editingAccount.provider === 'gemini'

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4 animate-in fade-in">
            <div className="bg-card w-full max-w-md rounded-xl border border-border shadow-2xl overflow-hidden animate-in zoom-in-95">
                <div className="p-4 border-b border-border flex justify-between items-start gap-4">
                    <div className="min-w-0">
                        <h3 className="font-semibold">{t('accountManager.modalEditAccountTitle')}</h3>
                        <p className="mt-1 text-xs text-muted-foreground">{t('accountManager.editAccountHint')}</p>
                    </div>
                    <button onClick={onClose} className="text-muted-foreground hover:text-foreground">
                        <X className="w-5 h-5" />
                    </button>
                </div>
                <div className="p-6 space-y-4 max-h-[85vh] overflow-y-auto custom-scrollbar">
                    <div className="rounded-lg border border-border bg-muted/20 px-3 py-2">
                        <div className="text-xs font-medium text-muted-foreground mb-1">{t('accountManager.accountIdentifierLabel')}</div>
                        <code className="text-sm font-mono text-foreground break-all">{editingAccount.identifier}</code>
                    </div>
                    <div>
                        <label className="block text-sm font-medium mb-1.5">{t('accountManager.nameOptional')}</label>
                        <input
                            type="text"
                            className="input-field"
                            placeholder={t('accountManager.namePlaceholder')}
                            value={editAccount.name}
                            onChange={e => setEditAccount({ ...editAccount, name: e.target.value })}
                            autoFocus
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium mb-1.5">{t('accountManager.remarkOptional')}</label>
                        <input
                            type="text"
                            className="input-field"
                            placeholder={t('accountManager.remarkPlaceholder')}
                            value={editAccount.remark || ''}
                            onChange={e => setEditAccount({ ...editAccount, remark: e.target.value })}
                        />
                    </div>
                    <div>
                        <div className="flex items-center justify-between mb-1.5">
                            <label className="block text-sm font-medium">
                                {isGemini ? 'Gemini Cookies' : t('accountManager.deepseekCookiesLabel')}
                            </label>
                            {!isGemini && (
                                <button
                                    type="button"
                                    onClick={handleCopyScript}
                                    className="text-xs text-primary hover:underline flex items-center gap-1 font-medium"
                                >
                                    {copiedScript ? <Check className="w-3.5 h-3.5 text-emerald-500" /> : <Copy className="w-3.5 h-3.5" />}
                                    {copiedScript ? t('accountManager.copiedScript') : t('accountManager.copyExtractScript')}
                                </button>
                            )}
                        </div>
                        <textarea
                            className="input-field bg-background font-mono text-xs h-24"
                            placeholder={isGemini ? t('accountManager.cookiesEditPlaceholder') : t('accountManager.deepseekCookiesPlaceholder')}
                            value={editAccount.cookies || ''}
                            onChange={e => setEditAccount({ ...editAccount, cookies: e.target.value })}
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium mb-1.5">{t('accountManager.poolTypeLabel')}</label>
                        <select
                            className="input-field"
                            value={editAccount.pool_type || 'default'}
                            onChange={e => setEditAccount({ ...editAccount, pool_type: e.target.value })}
                        >
                            <option value="default">{t('accountManager.poolTypeDefault')}</option>
                            <option value="no_tools">{t('accountManager.poolTypeNoTools')}</option>
                            <option value="tools_only">{t('accountManager.poolTypeToolsOnly')}</option>
                        </select>
                    </div>
                    <div className="flex justify-end gap-2 pt-2">
                        <button onClick={onClose} className="px-4 py-2 rounded-lg border border-border hover:bg-secondary transition-colors text-sm font-medium">{t('actions.cancel')}</button>
                        <button onClick={onSave} disabled={loading} className="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors text-sm font-medium disabled:opacity-50">
                            {loading ? t('accountManager.editAccountLoading') : t('accountManager.editAccountAction')}
                        </button>
                    </div>
                </div>
            </div>
        </div>
    )
}
