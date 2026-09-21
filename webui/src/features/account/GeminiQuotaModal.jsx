import { useState, useEffect } from 'react'
import { X, Gauge, RotateCw, AlertCircle, Sparkles, CheckCircle2, Clock, ShieldAlert } from 'lucide-react'
import clsx from 'clsx'

export default function GeminiQuotaModal({
    show,
    account,
    apiFetch,
    t,
    onClose,
}) {
    if (!show || !account) return null

    const [loading, setLoading] = useState(true)
    const [refreshing, setRefreshing] = useState(false)
    const [error, setError] = useState(null)
    const [data, setData] = useState(null)

    const fetchQuota = async (forceRefresh = false) => {
        if (forceRefresh) {
            setRefreshing(true)
        } else {
            setLoading(true)
        }
        setError(null)

        try {
            const identifier = account.identifier || account.name || account.email || ''
            const url = `/admin/accounts/${encodeURIComponent(identifier)}/quota${forceRefresh ? '?refresh=true' : ''}`
            const res = await apiFetch(url)
            const json = await res.json()
            if (!res.ok) {
                throw new Error(json.detail || json.message || t('accountManager.quota.loadFailed', { error: res.status }))
            }
            setData(json)
        } catch (err) {
            setError(err.message || String(err))
        } finally {
            setLoading(false)
            setRefreshing(false)
        }
    }

    useEffect(() => {
        fetchQuota(false)
    }, [account])

    const formatResetTime = (isoString) => {
        if (!isoString) return ''
        try {
            const d = new Date(isoString)
            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
        } catch {
            return ''
        }
    }

    const formatTimestamp = (ts) => {
        if (!ts || ts <= 0) return ''
        try {
            const d = new Date(ts * 1000)
            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
        } catch {
            return ''
        }
    }

    const tierLabel = data?.tier?.label || 'PRO'
    const metric5h = data?.usage?.current_5h
    const metricWeekly = data?.usage?.weekly
    const aiCredits = data?.usage?.ai_credits_remaining
    const quotas = data?.quotas || {}
    const extra = data?.extra_features

    const getProgressColor = (usedPct) => {
        if (usedPct >= 90) return 'from-rose-500 to-red-600'
        if (usedPct >= 75) return 'from-amber-500 to-yellow-500'
        return 'from-emerald-500 to-teal-400'
    }

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 animate-in fade-in">
            <div className="bg-card w-full max-w-xl rounded-2xl border border-border shadow-2xl overflow-hidden flex flex-col max-h-[90vh] animate-in zoom-in-95">
                {/* Header */}
                <div className="p-4 sm:p-5 border-b border-border flex items-center justify-between bg-muted/30">
                    <div className="flex items-center gap-3 min-w-0">
                        <div className="w-10 h-10 rounded-xl bg-primary/10 text-primary border border-primary/20 flex items-center justify-center shrink-0 shadow-sm">
                            <Gauge className="w-5 h-5" />
                        </div>
                        <div className="min-w-0">
                            <div className="flex items-center gap-2">
                                <h3 className="font-bold text-base sm:text-lg truncate">
                                    {t('accountManager.quota.modalTitle')}
                                </h3>
                                <span className={clsx(
                                    "px-2 py-0.5 rounded-full text-xs font-bold uppercase tracking-wider shrink-0 border",
                                    tierLabel === 'ULTRA' ? "bg-amber-500/10 text-amber-500 border-amber-500/30" :
                                    tierLabel === 'PRO' ? "bg-purple-500/10 text-purple-500 border-purple-500/30" :
                                    tierLabel === 'PLUS' ? "bg-blue-500/10 text-blue-500 border-blue-500/30" :
                                    "bg-zinc-500/10 text-zinc-400 border-zinc-500/30"
                                )}>
                                    {tierLabel}
                                </span>
                            </div>
                            <p className="text-xs text-muted-foreground truncate">
                                {account.name || account.identifier}
                            </p>
                        </div>
                    </div>
                    <div className="flex items-center gap-1">
                        <button
                            onClick={() => fetchQuota(true)}
                            disabled={refreshing || loading}
                            title={t('accountManager.quota.refresh')}
                            className="p-2 text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-colors disabled:opacity-50"
                        >
                            <RotateCw className={clsx("w-4 h-4", refreshing && "animate-spin text-primary")} />
                        </button>
                        <button
                            onClick={onClose}
                            className="p-2 text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-colors"
                        >
                            <X className="w-5 h-5" />
                        </button>
                    </div>
                </div>

                {/* Content */}
                <div className="p-4 sm:p-6 overflow-y-auto space-y-5">
                    {loading && (
                        <div className="py-16 flex flex-col items-center justify-center gap-3 text-muted-foreground">
                            <div className="w-8 h-8 border-3 border-primary border-t-transparent rounded-full animate-spin" />
                            <span className="text-sm">{t('accountManager.quota.refreshing')}</span>
                        </div>
                    )}

                    {error && !loading && (
                        <div className="p-4 rounded-xl border border-destructive/30 bg-destructive/10 text-destructive flex items-start gap-3">
                            <AlertCircle className="w-5 h-5 shrink-0 mt-0.5" />
                            <div className="text-sm space-y-2">
                                <p className="font-semibold">{t('actions.failed')}</p>
                                <p className="text-xs opacity-90">{error}</p>
                                <button
                                    onClick={() => fetchQuota(true)}
                                    className="px-3 py-1 bg-destructive/20 hover:bg-destructive/30 rounded text-xs font-medium transition-colors"
                                >
                                    {t('accountManager.quota.refresh')}
                                </button>
                            </div>
                        </div>
                    )}

                    {data && !loading && (
                        <>
                            {/* 5-Hour Compute Limit Card (Main Hero) */}
                            <div className="p-4 sm:p-5 rounded-xl border border-border bg-gradient-to-br from-card to-muted/40 shadow-sm space-y-3">
                                <div className="flex items-center justify-between">
                                    <div className="flex items-center gap-2">
                                        <Clock className="w-4 h-4 text-primary" />
                                        <span className="font-semibold text-sm">
                                            {t('accountManager.quota.h5Title')}
                                        </span>
                                    </div>
                                    <span className="text-xs font-medium px-2 py-0.5 rounded-md bg-secondary text-secondary-foreground border border-border/50">
                                        {metric5h?.reset_at ? t('accountManager.quota.resetAt', { time: formatResetTime(metric5h.reset_at) }) : '5h'}
                                    </span>
                                </div>

                                {metric5h && metric5h.remaining_credits !== undefined ? (
                                    <>
                                        <div className="h-3 w-full bg-secondary rounded-full overflow-hidden p-0.5 border border-border/40">
                                            <div
                                                className={clsx(
                                                    "h-full rounded-full transition-all duration-500 bg-gradient-to-r",
                                                    getProgressColor(metric5h.usage_percentage ?? 0)
                                                )}
                                                style={{ width: `${Math.max(0, Math.min(100, 100 - (metric5h.usage_percentage ?? 0)))}%` }}
                                            />
                                        </div>
                                        <div className="flex items-center justify-between text-xs text-muted-foreground pt-0.5">
                                            <span className="font-medium text-foreground">
                                                {t('accountManager.quota.creditsRemaining', {
                                                    remaining: metric5h.remaining_credits ?? 0,
                                                    used: metric5h.usage_percentage ?? 0,
                                                })}
                                            </span>
                                            <span>
                                                {100 - (metric5h.usage_percentage ?? 0)}% {t('accountManager.available')}
                                            </span>
                                        </div>
                                    </>
                                ) : (
                                    <div className="text-xs text-muted-foreground italic">
                                        {t('accountManager.quota.unlimited')}
                                    </div>
                                )}
                            </div>

                            {/* Weekly limit & AI credits row */}
                            {(metricWeekly || aiCredits !== null && aiCredits !== undefined) && (
                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                    {metricWeekly && (
                                        <div className="p-3.5 rounded-xl border border-border bg-card/60 space-y-2">
                                            <div className="flex items-center justify-between text-xs">
                                                <span className="font-medium text-muted-foreground">
                                                    {t('accountManager.quota.weeklyTitle')}
                                                </span>
                                                {metricWeekly.reset_at && (
                                                    <span className="text-[11px] text-muted-foreground">
                                                        {formatResetTime(metricWeekly.reset_at)}
                                                    </span>
                                                )}
                                            </div>
                                            <div className="text-sm font-bold text-foreground">
                                                {metricWeekly.remaining_credits !== undefined ? (
                                                    t('accountManager.quota.creditsRemainingSimple', { remaining: metricWeekly.remaining_credits })
                                                ) : '--'}
                                            </div>
                                            <div className="h-1.5 w-full bg-secondary rounded-full overflow-hidden">
                                                <div
                                                    className="h-full bg-primary rounded-full transition-all duration-300"
                                                    style={{ width: `${Math.max(0, Math.min(100, 100 - (metricWeekly.usage_percentage ?? 0)))}%` }}
                                                />
                                            </div>
                                        </div>
                                    )}

                                    {aiCredits !== null && aiCredits !== undefined && (
                                        <div className="p-3.5 rounded-xl border border-border bg-card/60 flex items-center justify-between">
                                            <div className="space-y-1">
                                                <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                                                    <Sparkles className="w-3.5 h-3.5 text-amber-500" />
                                                    <span>AI Credits</span>
                                                </div>
                                                <div className="text-sm font-bold text-foreground">
                                                    {t('accountManager.quota.aiCredits', { count: aiCredits })}
                                                </div>
                                            </div>
                                        </div>
                                    )}
                                </div>
                            )}

                            {/* Per-Model Quotas breakdown */}
                            {Object.keys(quotas).length > 0 && (
                                <div className="space-y-2.5">
                                    <div className="text-xs font-bold uppercase tracking-wider text-muted-foreground">
                                        {t('accountManager.quota.modelsBreakdown')}
                                    </div>
                                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5">
                                        {Object.entries(quotas).map(([aid, q]) => {
                                            const usedPct = q.usage_percentage ?? 0
                                            return (
                                                <div key={aid} className="p-3 rounded-xl border border-border bg-secondary/30 space-y-2">
                                                    <div className="text-xs font-semibold truncate text-foreground" title={q.label}>
                                                        {q.label}
                                                    </div>
                                                    <div className="text-sm font-bold">
                                                        {q.is_unlimited ? (
                                                            <span className="text-emerald-500 text-xs font-medium bg-emerald-500/10 px-1.5 py-0.5 rounded border border-emerald-500/20">
                                                                {t('accountManager.quota.unlimited')}
                                                            </span>
                                                        ) : (
                                                            <span>{q.remaining} <span className="text-xs text-muted-foreground font-normal">/ {q.total}</span></span>
                                                        )}
                                                    </div>
                                                    {!q.is_unlimited && (
                                                        <div className="h-1.5 w-full bg-secondary rounded-full overflow-hidden">
                                                            <div
                                                                className={clsx(
                                                                    "h-full rounded-full transition-all duration-300 bg-gradient-to-r",
                                                                    getProgressColor(usedPct)
                                                                )}
                                                                style={{ width: `${Math.max(0, Math.min(100, 100 - usedPct))}%` }}
                                                            />
                                                        </div>
                                                    )}
                                                    {q.reset_time > 0 && (
                                                        <div className="text-[10px] text-muted-foreground">
                                                            {formatTimestamp(q.reset_time)}
                                                        </div>
                                                    )}
                                                </div>
                                            )
                                        })}
                                    </div>
                                </div>
                            )}

                            {/* Extra Features & Diagnostics */}
                            <div className="p-3.5 rounded-xl border border-border bg-secondary/20 flex items-center justify-between">
                                <div className="text-xs space-y-0.5">
                                    <div className="font-semibold text-foreground">
                                        {t('accountManager.quota.extraFeatures')}
                                    </div>
                                    <div className="text-muted-foreground">
                                        Grounding Search, Code Execution, etc.
                                    </div>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    {extra?.is_blocked ? (
                                        <span className="flex items-center gap-1 text-xs font-semibold text-destructive bg-destructive/10 border border-destructive/20 px-2 py-0.5 rounded-full">
                                            <ShieldAlert className="w-3.5 h-3.5" />
                                            {t('accountManager.quota.extraBlocked')}
                                        </span>
                                    ) : (
                                        <span className="flex items-center gap-1 text-xs font-semibold text-emerald-500 bg-emerald-500/10 border border-emerald-500/20 px-2 py-0.5 rounded-full">
                                            <CheckCircle2 className="w-3.5 h-3.5" />
                                            {t('accountManager.quota.extraOk')}
                                        </span>
                                    )}
                                </div>
                            </div>
                        </>
                    )}
                </div>

                {/* Footer */}
                <div className="p-4 border-t border-border flex justify-end bg-muted/20">
                    <button
                        onClick={onClose}
                        className="px-4 py-2 text-xs font-medium bg-secondary text-secondary-foreground hover:bg-secondary/80 rounded-lg transition-colors"
                    >
                        {t('actions.close') || 'Đóng'}
                    </button>
                </div>
            </div>
        </div>
    )
}
