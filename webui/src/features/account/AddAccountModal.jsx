import { X } from 'lucide-react'

export default function AddAccountModal({
    show,
    t,
    newAccount,
    setNewAccount,
    loading,
    onClose,
    onAdd,
}) {
    if (!show) {
        return null
    }

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4 animate-in fade-in">
            <div className="bg-card w-full max-w-md rounded-xl border border-border shadow-2xl overflow-hidden animate-in zoom-in-95">
                <div className="p-4 border-b border-border flex justify-between items-center">
                    <h3 className="font-semibold">{t('accountManager.modalAddAccountTitle')}</h3>
                    <button onClick={onClose} className="text-muted-foreground hover:text-foreground">
                        <X className="w-5 h-5" />
                    </button>
                </div>
                <div className="p-6 space-y-4">
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
                    <div>
                        <label className="block text-sm font-medium mb-1.5">
                            {t('accountManager.nameOptional')}
                            {newAccount.provider === 'gemini' && (
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
                    {newAccount.provider === 'gemini' ? (
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
