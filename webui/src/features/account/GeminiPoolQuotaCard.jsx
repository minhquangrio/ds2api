import { useState, useEffect, useMemo, useCallback } from 'react'
import {
    Gauge,
    RotateCw,
    Sparkles,
    Clock,
    CheckCircle2,
    AlertTriangle,
    ShieldAlert,
    ChevronRight,
    Zap,
    ExternalLink,
    Calendar,
    Filter,
} from 'lucide-react'
import clsx from 'clsx'

export default function GeminiPoolQuotaCard({ accounts, apiFetch, t, onViewQuota }) {
    const geminiAccounts = useMemo(() => {
        return (accounts || []).filter((a) => a.provider === 'gemini')
    }, [accounts])

    const [quotasData, setQuotasData] = useState({})
    const [loadingAll, setLoadingAll] = useState(false)
    const [refreshingAll, setRefreshingAll] = useState(false)
    const [filterMode, setFilterMode] = useState('all') // 'all' | 'ready' | 'attention'

    const fetchAllQuotas = useCallback(async (forceRefresh = false) => {
        if (geminiAccounts.length === 0) return
        if (forceRefresh) setRefreshingAll(true)
        else setLoadingAll(true)

        try {
            const url = `/admin/accounts/gemini/quotas${forceRefresh ? '?refresh=true' : ''}`
            const res = await apiFetch(url)
            if (res.ok) {
                const data = await res.json()
                const map = {}
                if (Array.isArray(data.accounts)) {
                    data.accounts.forEach((item) => {
                        map[item.identifier] = {
                            quota: item.quota,
                            error: item.error,
                            loading: false,
                        }
                    })
                }
                setQuotasData(map)
            } else {
                // Fallback: fetch individually
                const promises = geminiAccounts.map(async (acc) => {
                    const id = acc.identifier || acc.name || acc.email
                    try {
                        const r = await apiFetch(`/admin/accounts/${encodeURIComponent(id)}/quota${forceRefresh ? '?refresh=true' : ''}`)
                        const q = await r.json()
                        return { id, quota: r.ok ? q : null, error: r.ok ? null : q.detail }
                    } catch (e) {
                        return { id, quota: null, error: String(e) }
                    }
                })
                const results = await Promise.all(promises)
                const map = {}
                results.forEach((r) => {
                    map[r.id] = { quota: r.quota, error: r.error, loading: false }
                })
                setQuotasData(map)
            }
        } catch (_err) {
            // Keep existing state on network error
        } finally {
            setLoadingAll(false)
            setRefreshingAll(false)
        }
    }, [apiFetch, geminiAccounts])

    useEffect(() => {
        if (geminiAccounts.length > 0) {
            fetchAllQuotas(false)
        }
    }, [fetchAllQuotas, geminiAccounts.length])

    const fetchSingleQuota = async (acc, forceRefresh = true) => {
        const id = acc.identifier || acc.name || acc.email
        setQuotasData((prev) => ({
            ...prev,
            [id]: { ...(prev[id] || {}), loading: true },
        }))

        try {
            const r = await apiFetch(`/admin/accounts/${encodeURIComponent(id)}/quota${forceRefresh ? '?refresh=true' : ''}`)
            const q = await r.json()
            setQuotasData((prev) => ({
                ...prev,
                [id]: { quota: r.ok ? q : null, error: r.ok ? null : q.detail, loading: false },
            }))
        } catch (e) {
            setQuotasData((prev) => ({
                ...prev,
                [id]: { quota: null, error: String(e), loading: false },
            }))
        }
    }

    const formatResetTimeOnly = (isoString) => {
        if (!isoString) return ''
        try {
            const d = new Date(isoString)
            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
        } catch {
            return ''
        }
    }

    const formatResetDateTime = (isoString) => {
        if (!isoString) return ''
        try {
            const d = new Date(isoString)
            const now = new Date()
            const isToday = d.toDateString() === now.toDateString()
            const timeStr = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
            if (isToday) {
                return timeStr
            }
            const dateStr = d.toLocaleDateString([], { day: '2-digit', month: '2-digit' })
            return `${dateStr} ${timeStr}`
        } catch {
            return ''
        }
    }

    const get5hProgressColor = (usedPct) => {
        if (usedPct >= 90) return 'bg-rose-500'
        if (usedPct >= 75) return 'bg-amber-500'
        return 'bg-emerald-500'
    }

    const getWeeklyProgressColor = (usedPct) => {
        if (usedPct >= 90) return 'bg-rose-500'
        if (usedPct >= 75) return 'bg-amber-500'
        return 'bg-indigo-500'
    }

    // Aggregate pool metrics: includes both 5h and Weekly totals
    const poolStats = useMemo(() => {
        let total5hCredits = 0
        let totalWeeklyCredits = 0
        let totalAiCredits = 0
        let readyAccounts = 0
        let throttled5hAccounts = 0
        let throttledWeeklyAccounts = 0
        let nearest5hResetTime = null
        let nearestWeeklyResetTime = null
        let total5hMeasured = 0
        let totalWeeklyMeasured = 0
        let sum5hUsagePct = 0
        let sumWeeklyUsagePct = 0

        geminiAccounts.forEach((acc) => {
            const id = acc.identifier || acc.name || acc.email
            const item = quotasData[id]
            const quota = item?.quota
            const metric5h = quota?.usage?.current_5h
            const metricWeekly = quota?.usage?.weekly
            const aiCredits = quota?.usage?.ai_credits_remaining

            if (typeof aiCredits === 'number' && aiCredits > 0) {
                totalAiCredits += aiCredits
            }

            let is5hThrottled = false
            let isWeeklyThrottled = false

            if (metric5h) {
                total5hMeasured += 1
                const remaining = metric5h.remaining_credits ?? 0
                const usedPct = metric5h.usage_percentage ?? 0
                total5hCredits += remaining
                sum5hUsagePct += usedPct

                if (usedPct >= 100 || remaining === 0) {
                    is5hThrottled = true
                }

                if (metric5h.reset_at) {
                    const resetDate = new Date(metric5h.reset_at)
                    if (!nearest5hResetTime || resetDate < nearest5hResetTime) {
                        nearest5hResetTime = resetDate
                    }
                }
            }

            if (metricWeekly) {
                totalWeeklyMeasured += 1
                const remaining = metricWeekly.remaining_credits ?? 0
                const usedPct = metricWeekly.usage_percentage ?? 0
                totalWeeklyCredits += remaining
                sumWeeklyUsagePct += usedPct

                if (usedPct >= 100 || remaining === 0) {
                    isWeeklyThrottled = true
                }

                if (metricWeekly.reset_at) {
                    const resetDate = new Date(metricWeekly.reset_at)
                    if (!nearestWeeklyResetTime || resetDate < nearestWeeklyResetTime) {
                        nearestWeeklyResetTime = resetDate
                    }
                }
            }

            if (isWeeklyThrottled) {
                throttledWeeklyAccounts += 1
            } else if (is5hThrottled) {
                throttled5hAccounts += 1
            } else if (acc.enabled !== false && !acc.muted && !acc.banned && !item?.error) {
                readyAccounts += 1
            }
        })

        const avg5hUsagePct = total5hMeasured > 0 ? Math.round(sum5hUsagePct / total5hMeasured) : 0
        const avgWeeklyUsagePct = totalWeeklyMeasured > 0 ? Math.round(sumWeeklyUsagePct / totalWeeklyMeasured) : 0

        return {
            totalAccounts: geminiAccounts.length,
            total5hCredits,
            totalWeeklyCredits,
            totalAiCredits,
            readyAccounts,
            throttled5hAccounts,
            throttledWeeklyAccounts,
            totalThrottled: throttled5hAccounts + throttledWeeklyAccounts,
            total5hMeasured,
            totalWeeklyMeasured,
            avg5hUsagePct,
            avgWeeklyUsagePct,
            nearest5hResetStr: nearest5hResetTime
                ? nearest5hResetTime.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
                : null,
            nearestWeeklyResetStr: nearestWeeklyResetTime
                ? formatResetDateTime(nearestWeeklyResetTime.toISOString())
                : null,
        }
    }, [geminiAccounts, quotasData])

    // Filter accounts by attention mode
    const filteredAccounts = useMemo(() => {
        if (filterMode === 'all') return geminiAccounts
        return geminiAccounts.filter((acc) => {
            const id = acc.identifier || acc.name || acc.email
            const state = quotasData[id]
            const quota = state?.quota
            const error = state?.error
            const metric5h = quota?.usage?.current_5h
            const metricWeekly = quota?.usage?.weekly

            const is5hThrottled = metric5h && ((metric5h.usage_percentage ?? 0) >= 100 || (metric5h.remaining_credits ?? 0) === 0)
            const isWeeklyThrottled = metricWeekly && ((metricWeekly.usage_percentage ?? 0) >= 100 || (metricWeekly.remaining_credits ?? 0) === 0)
            const hasIssue = Boolean(error || is5hThrottled || isWeeklyThrottled || acc.enabled === false || acc.muted || acc.banned)

            if (filterMode === 'attention') return hasIssue
            if (filterMode === 'ready') return !hasIssue
            return true
        })
    }, [geminiAccounts, filterMode, quotasData])

    if (geminiAccounts.length === 0) {
        return null
    }

    const isSingleAccount = geminiAccounts.length === 1

    return (
        <div className="rounded-2xl border border-border/80 bg-card/90 shadow-sm p-5 sm:p-6 space-y-6 overflow-hidden">
            {/* Header & Main Actions */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <div className="w-10 h-10 rounded-xl bg-purple-500/10 text-purple-400 border border-purple-500/20 flex items-center justify-center shrink-0 shadow-sm">
                        <Gauge className="w-5 h-5" />
                    </div>
                    <div>
                        <h2 className="text-base font-bold text-foreground flex items-center gap-2">
                            {isSingleAccount ? t('accountManager.quota.singleAccountTitle') : t('accountManager.quota.poolTitle')}
                            <span className="text-[11px] font-mono px-2 py-0.5 rounded-full bg-purple-500/10 text-purple-400 border border-purple-500/20 font-semibold">
                                {geminiAccounts.length} {geminiAccounts.length === 1 ? 'Node' : 'Nodes'}
                            </span>
                        </h2>
                        <p className="text-xs text-muted-foreground mt-0.5">
                            {t('accountManager.quota.poolDesc')}
                        </p>
                    </div>
                </div>

                <div className="flex items-center gap-2 self-start sm:self-auto">
                    <button
                        type="button"
                        onClick={() => fetchAllQuotas(true)}
                        disabled={refreshingAll || loadingAll}
                        className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl text-xs font-medium border border-border bg-background/80 hover:bg-secondary text-foreground transition-all disabled:opacity-50"
                    >
                        <RotateCw className={clsx("w-3.5 h-3.5", refreshingAll && "animate-spin text-primary")} />
                        <span>{refreshingAll ? t('accountManager.quota.refreshingAll') : t('accountManager.quota.refreshAll')}</span>
                    </button>
                </div>
            </div>

            {/* Aggregate Pool Metric Summary: Primary Weekly Quota & 5h Compute */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3.5">
                {/* 1. Weekly Pool Quota Hero */}
                <div className="p-4 rounded-xl border border-border/80 bg-background/80 flex flex-col justify-between space-y-2.5">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-1.5 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                            <Calendar className="w-3.5 h-3.5 text-indigo-500" />
                            <span>{t('accountManager.quota.totalWeeklyCredits')}</span>
                        </div>
                        <span className="text-[11px] font-mono font-medium text-muted-foreground">
                            {poolStats.totalWeeklyMeasured} nodes
                        </span>
                    </div>

                    <div>
                        <div className="text-2xl font-extrabold font-mono text-foreground">
                            {poolStats.totalWeeklyCredits.toLocaleString()} <span className="text-xs font-normal text-muted-foreground font-sans">credits</span>
                        </div>
                        {/* Weekly capacity bar */}
                        <div className="mt-2 h-1.5 w-full bg-secondary rounded-full overflow-hidden">
                            <div
                                className="h-full bg-indigo-500 rounded-full transition-all duration-500"
                                style={{ width: `${Math.max(4, Math.min(100, 100 - poolStats.avgWeeklyUsagePct))}%` }}
                            />
                        </div>
                    </div>

                    <div className="flex items-center justify-between text-[11px] text-muted-foreground pt-0.5">
                        <span>{t('accountManager.quota.avgWeeklyUsage', { pct: poolStats.avgWeeklyUsagePct })}</span>
                        <span className="font-mono text-foreground font-medium">
                            {poolStats.nearestWeeklyResetStr ? t('accountManager.quota.weeklyResetAt', { time: poolStats.nearestWeeklyResetStr }) : '—'}
                        </span>
                    </div>
                </div>

                {/* 2. 5-Hour Compute Pool Hero */}
                <div className="p-4 rounded-xl border border-border/80 bg-background/80 flex flex-col justify-between space-y-2.5">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-1.5 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                            <Clock className="w-3.5 h-3.5 text-purple-500" />
                            <span>{t('accountManager.quota.total5hCredits')}</span>
                        </div>
                        <span className="text-[11px] font-mono font-medium text-muted-foreground">
                            {poolStats.total5hMeasured} nodes
                        </span>
                    </div>

                    <div>
                        <div className="text-2xl font-extrabold font-mono text-foreground">
                            {poolStats.total5hCredits.toLocaleString()} <span className="text-xs font-normal text-muted-foreground font-sans">credits</span>
                        </div>
                        {/* 5h capacity bar */}
                        <div className="mt-2 h-1.5 w-full bg-secondary rounded-full overflow-hidden">
                            <div
                                className="h-full bg-purple-500 rounded-full transition-all duration-500"
                                style={{ width: `${Math.max(4, Math.min(100, 100 - poolStats.avg5hUsagePct))}%` }}
                            />
                        </div>
                    </div>

                    <div className="flex items-center justify-between text-[11px] text-muted-foreground pt-0.5">
                        <span>{t('accountManager.quota.remainingPct', { pct: 100 - poolStats.avg5hUsagePct })}</span>
                        <span className="font-mono text-foreground font-medium">
                            {poolStats.nearest5hResetStr ? t('accountManager.quota.resetAt', { time: poolStats.nearest5hResetStr }) : '—'}
                        </span>
                    </div>
                </div>

                {/* 3. Ready Accounts Status */}
                <div className="p-4 rounded-xl border border-border/70 bg-background/80 flex flex-col justify-between space-y-2.5">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-1.5 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                            <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
                            <span>{t('accountManager.quota.readyAccounts')}</span>
                        </div>
                        <span className="text-[11px] font-mono text-muted-foreground">
                            {poolStats.totalAccounts} total
                        </span>
                    </div>

                    <div className="text-2xl font-extrabold font-mono text-emerald-400">
                        {poolStats.readyAccounts} <span className="text-xs font-normal text-muted-foreground font-sans">/ {poolStats.totalAccounts} nodes</span>
                    </div>

                    <div className="text-[11px] text-muted-foreground truncate">
                        {poolStats.totalThrottled > 0 ? (
                            <span className="text-amber-400 font-medium">
                                {poolStats.throttled5hAccounts > 0 && `${poolStats.throttled5hAccounts} ${t('accountManager.quota.throttled5h')} `}
                                {poolStats.throttledWeeklyAccounts > 0 && `· ${poolStats.throttledWeeklyAccounts} ${t('accountManager.quota.throttledWeekly')}`}
                            </span>
                        ) : (
                            <span className="text-emerald-400 font-medium">{t('accountManager.quota.allReady')}</span>
                        )}
                    </div>
                </div>

                {/* 4. AI Credits & Extras */}
                <div className="p-4 rounded-xl border border-border/70 bg-background/80 flex flex-col justify-between space-y-2.5">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-1.5 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                            <Sparkles className="w-3.5 h-3.5 text-amber-400" />
                            <span>AI Credits</span>
                        </div>
                        <span className="text-[11px] text-muted-foreground font-mono">
                            Pool extras
                        </span>
                    </div>

                    <div className="text-2xl font-extrabold font-mono text-amber-400">
                        {poolStats.totalAiCredits.toLocaleString()} <span className="text-xs font-normal text-muted-foreground font-sans">credits</span>
                    </div>

                    <div className="text-[11px] text-muted-foreground truncate">
                        Grounding, Web Search, Code Exec
                    </div>
                </div>
            </div>

            {/* Account Filters (Visible when more than 1 account) */}
            {!isSingleAccount && (
                <div className="flex items-center justify-between pt-1 border-t border-border/50">
                    <div className="flex items-center gap-2">
                        <button
                            type="button"
                            onClick={() => setFilterMode('all')}
                            className={clsx(
                                "px-3 py-1 rounded-lg text-xs font-medium transition-all",
                                filterMode === 'all'
                                    ? "bg-purple-500/10 text-purple-400 border border-purple-500/30"
                                    : "text-muted-foreground hover:text-foreground hover:bg-secondary/60"
                            )}
                        >
                            {t('accountManager.quota.filterAll', { count: geminiAccounts.length })}
                        </button>
                        <button
                            type="button"
                            onClick={() => setFilterMode('ready')}
                            className={clsx(
                                "px-3 py-1 rounded-lg text-xs font-medium transition-all",
                                filterMode === 'ready'
                                    ? "bg-emerald-500/10 text-emerald-400 border border-emerald-500/30"
                                    : "text-muted-foreground hover:text-foreground hover:bg-secondary/60"
                            )}
                        >
                            {t('accountManager.quota.filterReady', { count: poolStats.readyAccounts })}
                        </button>
                        <button
                            type="button"
                            onClick={() => setFilterMode('attention')}
                            className={clsx(
                                "px-3 py-1 rounded-lg text-xs font-medium transition-all",
                                filterMode === 'attention'
                                    ? "bg-amber-500/10 text-amber-400 border border-amber-500/30"
                                    : "text-muted-foreground hover:text-foreground hover:bg-secondary/60"
                            )}
                        >
                            {t('accountManager.quota.filterAttention', { count: poolStats.totalThrottled })}
                        </button>
                    </div>

                    <span className="text-xs text-muted-foreground font-mono">
                        {filteredAccounts.length} / {geminiAccounts.length} accounts shown
                    </span>
                </div>
            )}

            {/* Multi-Account Cards Matrix */}
            <div className={clsx(
                "grid gap-4",
                isSingleAccount ? "grid-cols-1" : "grid-cols-1 md:grid-cols-2 lg:grid-cols-3"
            )}>
                {filteredAccounts.map((acc) => {
                    const id = acc.identifier || acc.name || acc.email
                    const state = quotasData[id]
                    const quota = state?.quota
                    const error = state?.error
                    const isLoading = state?.loading || (loadingAll && !quota)

                    const metric5h = quota?.usage?.current_5h
                    const metricWeekly = quota?.usage?.weekly
                    const aiCredits = quota?.usage?.ai_credits_remaining
                    const tierLabel = quota?.tier?.label || 'PRO'

                    const used5hPct = metric5h?.usage_percentage ?? 0
                    const remaining5h = metric5h?.remaining_credits
                    const reset5hStr = formatResetTimeOnly(metric5h?.reset_at)
                    const is5hThrottled = used5hPct >= 100 || remaining5h === 0

                    const usedWeeklyPct = metricWeekly?.usage_percentage ?? 0
                    const remainingWeekly = metricWeekly?.remaining_credits
                    const resetWeeklyStr = formatResetDateTime(metricWeekly?.reset_at)
                    const isWeeklyThrottled = usedWeeklyPct >= 100 || remainingWeekly === 0

                    const hasAttention = Boolean(error || is5hThrottled || isWeeklyThrottled)

                    return (
                        <div
                            key={id}
                            className={clsx(
                                "p-4 rounded-xl border transition-all duration-200 flex flex-col justify-between space-y-4",
                                isWeeklyThrottled
                                    ? "border-rose-500/40 bg-rose-500/5"
                                    : is5hThrottled
                                        ? "border-amber-500/40 bg-amber-500/5"
                                        : error
                                            ? "border-destructive/30 bg-destructive/5"
                                            : "border-border/70 bg-background/60 hover:border-purple-500/40"
                            )}
                        >
                            {/* Card Top: Identifier, Tier, Controls */}
                            <div>
                                <div className="flex items-start justify-between gap-2">
                                    <div className="min-w-0">
                                        <div className="flex items-center gap-2">
                                            <span className="font-semibold text-sm text-foreground truncate">
                                                {acc.name || id}
                                            </span>
                                            <span className={clsx(
                                                "px-1.5 py-0.2 rounded text-[10px] font-bold font-mono uppercase tracking-wider shrink-0 border",
                                                tierLabel === 'ULTRA' ? "bg-amber-500/10 text-amber-400 border-amber-500/30" :
                                                tierLabel === 'PRO' ? "bg-purple-500/10 text-purple-400 border-purple-500/30" :
                                                tierLabel === 'PLUS' ? "bg-blue-500/10 text-blue-400 border-blue-500/30" :
                                                "bg-zinc-500/10 text-zinc-400 border-zinc-500/30"
                                            )}>
                                                {tierLabel}
                                            </span>
                                        </div>
                                        {acc.name && (
                                            <div className="text-[11px] text-muted-foreground font-mono truncate mt-0.5">
                                                {id}
                                            </div>
                                        )}
                                    </div>

                                    {/* Action buttons */}
                                    <div className="flex items-center gap-1 shrink-0">
                                        <button
                                            type="button"
                                            onClick={() => fetchSingleQuota(acc, true)}
                                            disabled={isLoading}
                                            className="p-1.5 rounded-lg hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
                                            title={t('accountManager.quota.refresh')}
                                        >
                                            <RotateCw className={clsx("w-3.5 h-3.5", isLoading && "animate-spin text-primary")} />
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => onViewQuota(acc)}
                                            className="p-1.5 rounded-lg hover:bg-secondary text-muted-foreground hover:text-primary transition-colors"
                                            title={t('accountManager.quota.viewDetails')}
                                        >
                                            <ChevronRight className="w-4 h-4" />
                                        </button>
                                    </div>
                                </div>

                                {/* Dual-Meter: 5-Hour Compute & Weekly Limit */}
                                <div className="mt-3.5 space-y-3">
                                    {/* 1. 5h Compute Gauge */}
                                    <div className="space-y-1.5 p-2.5 rounded-lg bg-secondary/30 border border-border/40">
                                        <div className="flex items-center justify-between text-xs">
                                            <span className="text-muted-foreground flex items-center gap-1 font-medium">
                                                <Clock className="w-3 h-3 text-purple-400" />
                                                {t('accountManager.quota.h5Capacity')}
                                            </span>
                                            <span className="font-mono font-bold text-foreground">
                                                {remaining5h !== undefined
                                                    ? `${remaining5h} credits`
                                                    : error ? 'Error' : 'Loading...'}
                                            </span>
                                        </div>

                                        {metric5h ? (
                                            <>
                                                <div className="h-1.5 w-full bg-secondary/80 rounded-full overflow-hidden">
                                                    <div
                                                        className={clsx("h-full rounded-full transition-all duration-300", get5hProgressColor(used5hPct))}
                                                        style={{ width: `${Math.max(4, Math.min(100, 100 - used5hPct))}%` }}
                                                    />
                                                </div>
                                                <div className="flex items-center justify-between text-[10px]">
                                                    <span className={clsx(
                                                        "font-medium",
                                                        is5hThrottled ? "text-amber-400" : "text-emerald-400"
                                                    )}>
                                                        {is5hThrottled
                                                            ? t('accountManager.quota.throttled5h')
                                                            : t('accountManager.quota.remainingPct', { pct: 100 - used5hPct })}
                                                    </span>
                                                    {reset5hStr && (
                                                        <span className="text-muted-foreground font-mono">
                                                            {t('accountManager.quota.resetAt', { time: reset5hStr })}
                                                        </span>
                                                    )}
                                                </div>
                                            </>
                                        ) : error ? (
                                            <div className="text-[11px] text-destructive bg-destructive/10 p-1.5 rounded border border-destructive/20 truncate">
                                                {error}
                                            </div>
                                        ) : (
                                            <div className="h-1.5 w-full bg-muted/60 rounded-full animate-pulse" />
                                        )}
                                    </div>

                                    {/* 2. Weekly Quota Gauge */}
                                    <div className="space-y-1.5 p-2.5 rounded-lg bg-secondary/30 border border-border/40">
                                        <div className="flex items-center justify-between text-xs">
                                            <span className="text-muted-foreground flex items-center gap-1 font-medium">
                                                <Calendar className="w-3 h-3 text-indigo-400" />
                                                {t('accountManager.quota.weeklyCapacity')}
                                            </span>
                                            <span className="font-mono font-bold text-foreground">
                                                {remainingWeekly !== undefined
                                                    ? `${remainingWeekly} credits`
                                                    : metricWeekly ? t('accountManager.quota.noWeeklyLimit') : error ? '—' : 'Loading...'}
                                            </span>
                                        </div>

                                        {metricWeekly ? (
                                            <>
                                                <div className="h-1.5 w-full bg-secondary/80 rounded-full overflow-hidden">
                                                    <div
                                                        className={clsx("h-full rounded-full transition-all duration-300", getWeeklyProgressColor(usedWeeklyPct))}
                                                        style={{ width: `${Math.max(4, Math.min(100, 100 - usedWeeklyPct))}%` }}
                                                    />
                                                </div>
                                                <div className="flex items-center justify-between text-[10px]">
                                                    <span className={clsx(
                                                        "font-medium",
                                                        isWeeklyThrottled ? "text-rose-400" : "text-indigo-400"
                                                    )}>
                                                        {isWeeklyThrottled
                                                            ? t('accountManager.quota.throttledWeekly')
                                                            : t('accountManager.quota.usedPct', { pct: usedWeeklyPct })}
                                                    </span>
                                                    {resetWeeklyStr && (
                                                        <span className="text-muted-foreground font-mono">
                                                            {t('accountManager.quota.weeklyResetAt', { time: resetWeeklyStr })}
                                                        </span>
                                                    )}
                                                </div>
                                            </>
                                        ) : (
                                            <div className="text-[10px] text-muted-foreground italic">
                                                {t('accountManager.quota.noWeeklyLimit')}
                                            </div>
                                        )}
                                    </div>
                                </div>
                            </div>

                            {/* Card Footer: Status Indicators & Detail Link */}
                            <div className="pt-2 border-t border-border/50 flex items-center justify-between text-[11px]">
                                <div className="flex items-center gap-2">
                                    {isWeeklyThrottled ? (
                                        <span className="inline-flex items-center gap-1 text-rose-400 font-medium">
                                            <span className="w-1.5 h-1.5 rounded-full bg-rose-500" />
                                            {t('accountManager.quota.throttledWeekly')}
                                        </span>
                                    ) : is5hThrottled ? (
                                        <span className="inline-flex items-center gap-1 text-amber-400 font-medium">
                                            <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
                                            {t('accountManager.quota.throttled5h')}
                                        </span>
                                    ) : error ? (
                                        <span className="inline-flex items-center gap-1 text-destructive font-medium">
                                            <span className="w-1.5 h-1.5 rounded-full bg-destructive" />
                                            {t('accountManager.quota.statusError')}
                                        </span>
                                    ) : (
                                        <span className="inline-flex items-center gap-1 text-emerald-400 font-medium">
                                            <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
                                            {t('accountManager.quota.statusReady')}
                                        </span>
                                    )}

                                    {aiCredits !== null && aiCredits !== undefined && (
                                        <span className="inline-flex items-center gap-1 text-amber-400 font-medium ml-1">
                                            <Sparkles className="w-3 h-3" />
                                            {aiCredits}
                                        </span>
                                    )}
                                </div>

                                <button
                                    type="button"
                                    onClick={() => onViewQuota(acc)}
                                    className="text-primary hover:underline font-medium inline-flex items-center gap-0.5"
                                >
                                    {t('accountManager.quota.viewDetails')}
                                </button>
                            </div>
                        </div>
                    )
                })}
            </div>
        </div>
    )
}
