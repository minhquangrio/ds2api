import { useState } from 'react'
import { X, Copy, Check, Info } from 'lucide-react'

const EXTRACT_SCRIPT = `copy(JSON.stringify({token: localStorage.getItem('userToken'), cookies: document.cookie}))`

export default function AddAccountModal({
    show,
    t,
    newAccount,
    setNewAccount,
    loading,
    onClose,
    onAdd,
}) {
    const [dsAuthMode, setDsAuthMode] = useState('cookie')
    const [copiedScript, setCopiedScript] = useState(false)

    if (!show) {
        return null
    }

    const handleCopyScript = () => {
        navigator.clipboard.writeText(EXTRACT_SCRIPT)
        setCopiedScript(true)
        setTimeout(() => setCopiedScript(false), 2000)
    }

    const isGemini = newAccount.provider === 'gemini'

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4 animate-in fade-in">
            <div className="bg-card w-full max-w-md rounded-xl border border-border shadow-2xl overflow-hidden animate-in zoom-in-95">
                <div className="p-4 border-b border-border flex justify-between items-center">
                    <h3 className="font-semibold">{t('accountManager.modalAddAccountTitle')}</h3>
                    <button onClick={onClose} className="text-muted-foreground hover:text-foreground">
                        <X className="w-5 h-5" />
                    </button>
                </div>
                <div className="p-6 space-y-4 max-h-[85vh] overflow-y-auto custom-scrollbar">
                    <div>
                        <label className="block text-sm font-medium mb-1.5">Provider</label>
                        <select
                            className="input-field"
                            value={newAccount.provider || 'deepseek'}
                            onChange={e => setNewAccount({ ...newAccount, provider: e.target.value })}
                        >
                            <option value="deepseek">DeepSeek</option>
                            <option value="gemini">Google Gemini Web</option>
                        </select>
                    </div>

                    {!isGemini && (
                        <div>
                            <label className="block text-sm font-medium mb-1.5">{t('accountManager.authModeLabel')}</label>
                            <div className="grid grid-cols-2 gap-2">
                                <button
                                    type="button"
                                    onClick={() => setDsAuthMode('cookie')}
                                    className={`px-3 py-2 text-xs font-medium rounded-lg border transition-all ${
                                        dsAuthMode === 'cookie'
                                            ? 'bg-primary text-primary-foreground border-primary shadow-xs'
                                            : 'bg-secondary/40 text-muted-foreground border-border hover:bg-secondary'
                                    }`}
                                >
                                    {t('accountManager.authModeCookie')}
                                </button>
                                <button
                                    type="button"
                                    onClick={() => setDsAuthMode('password')}
                                    className={`px-3 py-2 text-xs font-medium rounded-lg border transition-all ${
                                        dsAuthMode === 'password'
                                            ? 'bg-primary text-primary-foreground border-primary shadow-xs'
                                            : 'bg-secondary/40 text-muted-foreground border-border hover:bg-secondary'
                                    }`}
                                >
                                    {t('accountManager.authModePassword')}
                                </button>
                            </div>
                        </div>
                    )}

                    <div>
                        <label className="block text-sm font-medium mb-1.5">
                            {t('accountManager.nameOptional')}
                            {(isGemini || dsAuthMode === 'cookie') && (
                                <span className="text-xs text-muted-foreground font-normal ml-1">
                                    ({t('accountManager.geminiNameOrEmailHint')})
                                </span>
                            )}
                        </label>
                        <input
                            type="text"
                            className="input-field"
                            placeholder={t('accountManager.namePlaceholder')}
                            value={newAccount.name || ''}
                            onChange={e => setNewAccount({ ...newAccount, name: e.target.value })}
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium mb-1.5">{t('accountManager.remarkOptional')}</label>
                        <input
                            type="text"
                            className="input-field"
                            placeholder={t('accountManager.remarkPlaceholder')}
                            value={newAccount.remark || ''}
                            onChange={e => setNewAccount({ ...newAccount, remark: e.target.value })}
                        />
                    </div>

                    {isGemini ? (
                        <>
                            <div>
                                <label className="block text-sm font-medium mb-1.5">{t('accountManager.emailOptional')}</label>
                                <input
                                    type="email"
                                    className="input-field"
                                    placeholder="user@gmail.com"
                                    value={newAccount.email || ''}
                                    onChange={e => setNewAccount({ ...newAccount, email: e.target.value })}
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium mb-1.5">Gemini Cookies <span className="text-destructive">*</span></label>
                                <textarea
                                    className="input-field bg-background font-mono text-xs h-24"
                                    placeholder="__Secure-1PSID=...; __Secure-1PSIDTS=... hoặc JSON"
                                    value={newAccount.cookies || ''}
                                    onChange={e => setNewAccount({ ...newAccount, cookies: e.target.value })}
                                />
                            </div>
                        </>
                    ) : dsAuthMode === 'cookie' ? (
                        <>
                            <div>
                                <div className="flex items-center justify-between mb-1.5">
                                    <label className="block text-sm font-medium">
                                        {t('accountManager.deepseekCookiesLabel')} <span className="text-destructive">*</span>
                                    </label>
                                    <button
                                        type="button"
                                        onClick={handleCopyScript}
                                        className="text-xs text-primary hover:underline flex items-center gap-1 font-medium"
                                    >
                                        {copiedScript ? <Check className="w-3.5 h-3.5 text-emerald-500" /> : <Copy className="w-3.5 h-3.5" />}
                                        {copiedScript ? t('accountManager.copiedScript') : t('accountManager.copyExtractScript')}
                                    </button>
                                </div>
                                <textarea
                                    className="input-field bg-background font-mono text-xs h-24"
                                    placeholder={t('accountManager.deepseekCookiesPlaceholder')}
                                    value={newAccount.cookies || ''}
                                    onChange={e => setNewAccount({ ...newAccount, cookies: e.target.value })}
                                />
                            </div>
                            <div className="rounded-lg border border-border/60 bg-secondary/30 p-2.5 text-xs text-muted-foreground flex gap-2">
                                <Info className="w-4 h-4 text-primary shrink-0 mt-0.5" />
                                <div>{t('accountManager.extractScriptHint')}</div>
                            </div>
                        </>
                    ) : (
                        <>
                            <div>
                                <label className="block text-sm font-medium mb-1.5">
                                    {t('accountManager.emailOptional')}
                                    <span className="text-xs text-muted-foreground font-normal ml-1">
                                        ({t('accountManager.deepseekEmailOrMobileHint')})
                                    </span>
                                </label>
                                <input
                                    type="email"
                                    className="input-field"
                                    placeholder="user@example.com"
                                    value={newAccount.email || ''}
                                    onChange={e => setNewAccount({ ...newAccount, email: e.target.value })}
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium mb-1.5">{t('accountManager.mobileOptional')}</label>
                                <input
                                    type="text"
                                    className="input-field"
                                    placeholder="+86..."
                                    value={newAccount.mobile || ''}
                                    onChange={e => setNewAccount({ ...newAccount, mobile: e.target.value })}
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium mb-1.5">{t('accountManager.passwordLabel')} <span className="text-destructive">*</span></label>
                                <input
                                    type="password"
                                    className="input-field bg-background"
                                    placeholder={t('accountManager.passwordPlaceholder')}
                                    value={newAccount.password || ''}
                                    onChange={e => setNewAccount({ ...newAccount, password: e.target.value })}
                                />
                            </div>
                        </>
                    )}
                    <div>
                        <label className="block text-sm font-medium mb-1.5">{t('accountManager.poolTypeLabel')}</label>
                        <select
                            className="input-field"
                            value={newAccount.pool_type || 'default'}
                            onChange={e => setNewAccount({ ...newAccount, pool_type: e.target.value })}
                        >
                            <option value="default">{t('accountManager.poolTypeDefault')}</option>
                            <option value="no_tools">{t('accountManager.poolTypeNoTools')}</option>
                            <option value="tools_only">{t('accountManager.poolTypeToolsOnly')}</option>
                        </select>
                    </div>
                    <div className="flex justify-end gap-2 pt-2">
                        <button onClick={onClose} className="px-4 py-2 rounded-lg border border-border hover:bg-secondary transition-colors text-sm font-medium">{t('actions.cancel')}</button>
                        <button onClick={onAdd} disabled={loading} className="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors text-sm font-medium disabled:opacity-50">
                            {loading ? t('accountManager.addAccountLoading') : t('accountManager.addAccountAction')}
                        </button>
                    </div>
                </div>
            </div>
        </div>
    )
}
