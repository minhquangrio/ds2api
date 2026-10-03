import { useEffect, useRef, useState } from 'react'
import {
    AlertCircle,
    Check,
    CheckCircle2,
    Copy,
    ExternalLink,
    Key,
    Loader2,
    Sparkles,
    X,
} from 'lucide-react'
import clsx from 'clsx'

import { useI18n } from '../../i18n'

export default function CodexLoginModal({ isOpen, onClose, authFetch, onSuccess, onMessage }) {
    const { t } = useI18n()
    const [step, setStep] = useState('idle') // idle, waiting, exchanging, success, failed
    const [sessionData, setSessionData] = useState(null)
    const [manualInput, setManualInput] = useState('')
    const [copied, setCopied] = useState(false)
    const [errorMsg, setErrorMsg] = useState('')
    const [successAccount, setSuccessAccount] = useState(null)
    const pollTimerRef = useRef(null)

    const clearPolling = () => {
        if (pollTimerRef.current) {
            clearInterval(pollTimerRef.current)
            pollTimerRef.current = null
        }
    }

    useEffect(() => {
        return () => clearPolling()
    }, [])

    const handleStartLogin = async () => {
        setErrorMsg('')
        setStep('waiting')
        try {
            const res = await authFetch('/admin/codex/login/start', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ port: 1455 }),
            })
            const data = await res.json()
            if (!res.ok) throw new Error(data.error || 'Failed to start login')

            setSessionData(data)
            startPolling(data.session_id)
        } catch (err) {
            setErrorMsg(err.message)
            setStep('failed')
        }
    }

    const startPolling = (sessionId) => {
        clearPolling()
        pollTimerRef.current = setInterval(async () => {
            try {
                const res = await authFetch('/admin/codex/login/poll', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ session_id: sessionId }),
                })
                const data = await res.json()
                if (data.status === 'completed' && data.account) {
                    clearPolling()
                    setSuccessAccount(data.account)
                    setStep('success')
                    onSuccess?.()
                } else if (data.status === 'failed') {
                    clearPolling()
                    setErrorMsg(data.error || 'Login session failed')
                    setStep('failed')
                }
            } catch (_err) {
                // Keep polling
            }
        }, 2000)
    }

    const handleCompleteManual = async (e) => {
        e?.preventDefault()
        if (!manualInput.trim() || !sessionData?.session_id) return

        clearPolling()
        setStep('exchanging')
        setErrorMsg('')

        try {
            const payload = {
                session_id: sessionData.session_id,
                redirect_url: manualInput.trim(),
            }
            const res = await authFetch('/admin/codex/login/complete', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            })
            const data = await res.json()
            if (!res.ok) throw new Error(data.error || 'Failed to complete login')

            setSuccessAccount(data.account)
            setStep('success')
            onSuccess?.()
        } catch (err) {
            setErrorMsg(err.message)
            setStep('failed')
        }
    }

    const handleCancel = async () => {
        clearPolling()
        if (sessionData?.session_id) {
            try {
                await authFetch('/admin/codex/login/cancel', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ session_id: sessionData.session_id }),
                })
            } catch (_err) {
                // ignore
            }
        }
        onClose()
    }

    const copyAuthUrl = () => {
        if (!sessionData?.authorize_url) return
        navigator.clipboard.writeText(sessionData.authorize_url)
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
    }

    if (!isOpen) return null

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-background/80 backdrop-blur-sm animate-in fade-in">
            <div className="relative w-full max-w-lg p-6 rounded-2xl bg-card border border-border shadow-2xl space-y-5">
                <div className="flex items-center justify-between pb-3 border-b border-border">
                    <div className="flex items-center gap-2.5">
                        <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-emerald-500 to-teal-600 flex items-center justify-center text-white">
                            <Sparkles className="w-4 h-4" />
                        </div>
                        <div>
                            <h3 className="font-bold text-base text-foreground">{t('codex.loginModalTitle')}</h3>
                            <p className="text-xs text-muted-foreground">{t('codex.loginModalDesc')}</p>
                        </div>
                    </div>
                    <button onClick={handleCancel} className="p-1 rounded-lg hover:bg-secondary text-muted-foreground">
                        <X className="w-5 h-5" />
                    </button>
                </div>

                {step === 'idle' && (
                    <div className="space-y-4 py-3 text-sm">
                        <p className="text-muted-foreground text-xs leading-relaxed">
                            {t('codex.loginInstructions')}
                        </p>
                        <div className="p-4 rounded-xl bg-primary/5 border border-primary/20 space-y-2 text-xs">
                            <div className="font-semibold text-foreground flex items-center gap-1.5">
                                <Key className="w-3.5 h-3.5 text-primary" />
                                {t('codex.oauthFeatures')}
                            </div>
                            <ul className="list-disc list-inside text-muted-foreground space-y-1">
                                <li>{t('codex.feature1')}</li>
                                <li>{t('codex.feature2')}</li>
                                <li>{t('codex.feature3')}</li>
                            </ul>
                        </div>
                        <div className="flex justify-end gap-2 pt-2">
                            <button
                                onClick={handleCancel}
                                className="px-4 py-2 rounded-xl border border-border hover:bg-secondary text-xs font-medium"
                            >
                                {t('common.cancel')}
                            </button>
                            <button
                                onClick={handleStartLogin}
                                className="inline-flex items-center gap-2 px-5 py-2 rounded-xl bg-primary text-primary-foreground font-medium text-xs hover:opacity-90 transition shadow-md shadow-primary/20"
                            >
                                <ExternalLink className="w-3.5 h-3.5" />
                                <span>{t('codex.startLoginBtn')}</span>
                            </button>
                        </div>
                    </div>
                )}

                {step === 'waiting' && sessionData && (
                    <div className="space-y-4 text-xs">
                        <div className="flex items-center justify-between p-3.5 rounded-xl bg-secondary/60 border border-border">
                            <div className="flex items-center gap-2">
                                <Loader2 className="w-4 h-4 animate-spin text-primary" />
                                <span className="font-medium text-foreground">{t('codex.waitingForAuth')}</span>
                            </div>
                            <span className="font-mono text-[11px] text-muted-foreground">{sessionData.redirect_uri}</span>
                        </div>

                        <div className="flex items-center gap-2">
                            <a
                                href={sessionData.authorize_url}
                                target="_blank"
                                rel="noreferrer"
                                className="flex-1 inline-flex items-center justify-center gap-2 py-2.5 px-4 rounded-xl bg-primary text-primary-foreground font-semibold text-xs hover:opacity-90 transition shadow-md shadow-primary/20"
                            >
                                <ExternalLink className="w-4 h-4" />
                                <span>{t('codex.openAuthTab')}</span>
                            </a>
                            <button
                                onClick={copyAuthUrl}
                                className="inline-flex items-center justify-center gap-1.5 py-2.5 px-3 rounded-xl bg-secondary text-secondary-foreground hover:bg-secondary/80 font-medium transition"
                            >
                                {copied ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
                                <span>{copied ? t('common.copied') : t('common.copy')}</span>
                            </button>
                        </div>

                        {/* Manual Fallback Input */}
                        <div className="pt-3 border-t border-border space-y-2">
                            <label className="text-[11px] font-semibold text-muted-foreground block">
                                {t('codex.manualFallbackLabel')}
                            </label>
                            <form onSubmit={handleCompleteManual} className="flex gap-2">
                                <input
                                    type="text"
                                    value={manualInput}
                                    onChange={(e) => setManualInput(e.target.value)}
                                    placeholder="http://localhost:1455/auth/callback?code=... hoặc mã code"
                                    className="flex-1 px-3 py-2 rounded-xl bg-background/50 border border-border focus:border-primary text-xs font-mono"
                                />
                                <button
                                    type="submit"
                                    disabled={!manualInput.trim()}
                                    className="px-3 py-2 rounded-xl bg-secondary text-secondary-foreground font-medium text-xs hover:bg-secondary/80 disabled:opacity-50"
                                >
                                    {t('codex.submitCode')}
                                </button>
                            </form>
                            <p className="text-[10px] text-muted-foreground">{t('codex.manualFallbackHint')}</p>
                        </div>

                        <div className="pt-2 flex justify-end">
                            <button
                                onClick={handleCancel}
                                className="px-4 py-1.5 rounded-xl border border-border hover:bg-secondary text-xs"
                            >
                                {t('common.cancel')}
                            </button>
                        </div>
                    </div>
                )}

                {step === 'exchanging' && (
                    <div className="py-8 flex flex-col items-center justify-center gap-3 text-muted-foreground">
                        <Loader2 className="w-8 h-8 animate-spin text-primary" />
                        <p className="text-xs animate-pulse">{t('codex.exchangingTokens')}</p>
                    </div>
                )}

                {step === 'success' && successAccount && (
                    <div className="space-y-4 py-2 text-xs">
                        <div className="p-4 rounded-xl bg-emerald-500/10 border border-emerald-500/20 flex items-center gap-3">
                            <CheckCircle2 className="w-6 h-6 text-emerald-400 shrink-0" />
                            <div>
                                <div className="font-bold text-sm text-foreground">{t('codex.authSuccessTitle')}</div>
                                <div className="text-muted-foreground mt-0.5">{t('codex.authSuccessDesc')}</div>
                            </div>
                        </div>

                        <div className="p-3.5 rounded-xl bg-background/50 border border-border space-y-1.5 font-mono">
                            <div className="flex justify-between">
                                <span className="text-muted-foreground">Email:</span>
                                <span className="font-semibold text-foreground">{successAccount.email || '—'}</span>
                            </div>
                            <div className="flex justify-between">
                                <span className="text-muted-foreground">Plan:</span>
                                <span className="font-semibold text-primary uppercase">{successAccount.codex_plan_type || 'Plus'}</span>
                            </div>
                            <div className="flex justify-between">
                                <span className="text-muted-foreground">Account ID:</span>
                                <span className="text-muted-foreground">{successAccount.codex_account_id || '—'}</span>
                            </div>
                        </div>

                        <div className="flex justify-end pt-2">
                            <button
                                onClick={onClose}
                                className="px-5 py-2 rounded-xl bg-primary text-primary-foreground font-medium text-xs hover:opacity-90"
                            >
                                {t('common.done')}
                            </button>
                        </div>
                    </div>
                )}

                {step === 'failed' && (
                    <div className="space-y-4 py-2 text-xs">
                        <div className="p-4 rounded-xl bg-destructive/10 border border-destructive/20 flex items-start gap-3">
                            <AlertCircle className="w-5 h-5 text-destructive shrink-0 mt-0.5" />
                            <div className="space-y-1">
                                <div className="font-bold text-sm text-destructive">{t('codex.authFailedTitle')}</div>
                                <div className="text-muted-foreground break-all">{errorMsg}</div>
                            </div>
                        </div>

                        <div className="flex justify-end gap-2 pt-2">
                            <button
                                onClick={handleCancel}
                                className="px-4 py-2 rounded-xl border border-border hover:bg-secondary text-xs"
                            >
                                {t('common.close')}
                            </button>
                            <button
                                onClick={handleStartLogin}
                                className="px-4 py-2 rounded-xl bg-primary text-primary-foreground font-medium text-xs hover:opacity-90"
                            >
                                {t('common.retry')}
                            </button>
                        </div>
                    </div>
                )}
            </div>
        </div>
    )
}
