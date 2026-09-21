import { useState, useEffect, useMemo, useCallback } from 'react'
import {
    Activity,
    Zap,
    Check,
    Copy,
    Cpu,
    Clock,
    Key,
    RefreshCw,
    TrendingUp,
    Layers,
    Terminal,
    ArrowRight,
    ShieldCheck,
    AlertCircle,
    Server,
    Gauge,
    Calendar,
} from 'lucide-react'
import clsx from 'clsx'

import { useI18n } from '../../i18n'
import { estimateItemTokens } from '../chatHistory/ChatHistoryContainer'

export default function TokenOverviewContainer({ config, authFetch, onNavigate, onMessage }) {
    const { t } = useI18n()
    const apiFetch = authFetch || fetch

    const [historyItems, setHistoryItems] = useState([])
    const [accounts, setAccounts] = useState([])
    const [queueStatus, setQueueStatus] = useState(null)
    const [geminiQuotas, setGeminiQuotas] = useState([])
    const [loading, setLoading] = useState(true)
    const [refreshing, setRefreshing] = useState(false)
    const [activeSnippetTab, setActiveSnippetTab] = useState('curl')
    const [copiedTarget, setCopiedTarget] = useState(null)
    const [hoveredBucket, setHoveredBucket] = useState(null)

    const copyToClipboard = useCallback((text, targetId) => {
        navigator.clipboard.writeText(text).then(() => {
            setCopiedTarget(targetId)
            setTimeout(() => setCopiedTarget(null), 1800)
        })
    }, [])

    const loadData = useCallback(async (isSilent = false) => {
        if (!isSilent) setLoading(true)
        else setRefreshing(true)

        try {
            const [historyRes, accountsRes, queueRes, geminiQuotasRes] = await Promise.allSettled([
                apiFetch('/admin/chat-history?limit=100'),
                apiFetch('/admin/accounts'),
                apiFetch('/admin/queue/status'),
                apiFetch('/admin/accounts/gemini/quotas')
            ])

            if (historyRes.status === 'fulfilled' && historyRes.value.ok) {
                const data = await historyRes.value.json()
                const items = Array.isArray(data) ? data : (data.items || [])
                setHistoryItems(items)
            }

            if (accountsRes.status === 'fulfilled' && accountsRes.value.ok) {
                const data = await accountsRes.value.json()
                setAccounts(Array.isArray(data) ? data : [])
            } else if (config?.accounts) {
                setAccounts(config.accounts)
            }

            if (queueRes.status === 'fulfilled' && queueRes.value.ok) {
                const data = await queueRes.value.json()
                setQueueStatus(data)
            }

            if (geminiQuotasRes.status === 'fulfilled' && geminiQuotasRes.value.ok) {
                const gData = await geminiQuotasRes.value.json()
                setGeminiQuotas(Array.isArray(gData.accounts) ? gData.accounts : [])
            }
        } catch (_err) {
            // Best effort load
        } finally {
            setLoading(false)
            setRefreshing(false)
        }
    }, [apiFetch, config?.accounts])

    useEffect(() => {
        loadData()
    }, [loadData])

    // Compute token analytics
    const telemetry = useMemo(() => {
        let totalPrompt = 0
        let totalCompletion = 0
        let totalReasoning = 0
        let totalTokens = 0
        let successRequests = 0
        let totalElapsedMs = 0
        let measuredElapsedCount = 0

        const modelStats = {}

        historyItems.forEach((item) => {
            const { prompt, completion, total } = estimateItemTokens(item)
            const reasoning = Number(item.reasoning_tokens) || 0

            totalPrompt += prompt
            totalCompletion += completion
            totalReasoning += reasoning
            totalTokens += total

            const isSuccess = item.status === 'ok' || (item.status_code && item.status_code >= 200 && item.status_code < 400)
            if (isSuccess) successRequests += 1

            if (item.elapsed_ms && item.elapsed_ms > 0) {
                totalElapsedMs += item.elapsed_ms
                measuredElapsedCount += 1
            }

            const modelName = item.model || 'unknown'
            if (!modelStats[modelName]) {
                modelStats[modelName] = { count: 0, tokens: 0 }
            }
            modelStats[modelName].count += 1
            modelStats[modelName].tokens += total
        })

        const totalReqs = historyItems.length
        const successRate = totalReqs > 0 ? Math.round((successRequests / totalReqs) * 100) : 100
        const avgLatencyMs = measuredElapsedCount > 0 ? Math.round(totalElapsedMs / measuredElapsedCount) : 0

        // Sorted models
        const sortedModels = Object.entries(modelStats)
            .map(([name, data]) => ({
                name,
                count: data.count,
                tokens: data.tokens,
                percentage: totalTokens > 0 ? Math.round((data.tokens / totalTokens) * 100) : 0,
            }))
            .sort((a, b) => b.tokens - a.tokens)

        return {
            totalTokens,
            totalPrompt,
            totalCompletion,
            totalReasoning,
            totalReqs,
            successRate,
            avgLatencyMs,
            models: sortedModels,
        }
    }, [historyItems])

    // Upstream nodes health
    const upstreamHealth = useMemo(() => {
        const pool = accounts.length > 0 ? accounts : (config?.accounts || [])
        let ready = 0
        let cooldown = 0
        let muted = 0
        let disabled = 0

        pool.forEach((acc) => {
            if (acc.disabled || acc.banned) {
                disabled += 1
            } else if (acc.muted) {
                muted += 1
            } else if (acc.cooldown_until && acc.cooldown_until > Date.now() / 1000) {
                cooldown += 1
            } else if (acc.test_status === 'ok' || acc.has_token || acc.token) {
                ready += 1
            } else {
                ready += 1 // active by default if configured
            }
        })

        return {
            total: pool.length,
            ready,
            cooldown,
            muted,
            disabled,
        }
    }, [accounts, config?.accounts])

    // Gemini Pool Quota calculation (Both 5h and Weekly totals)
    const geminiPoolStats = useMemo(() => {
        let total5hCredits = 0
        let totalWeeklyCredits = 0
        let readyCount = 0
        let throttled5hCount = 0
        let throttledWeeklyCount = 0
        let nearest5hResetTime = null

        geminiQuotas.forEach((item) => {
            const metric5h = item.quota?.usage?.current_5h
            const metricWeekly = item.quota?.usage?.weekly

            let is5hThrottled = false
            let isWeeklyThrottled = false

            if (metric5h) {
                const rem = metric5h.remaining_credits ?? 0
                const pct = metric5h.usage_percentage ?? 0
                total5hCredits += rem
                if (pct >= 100 || rem === 0) {
                    is5hThrottled = true
                    throttled5hCount += 1
                }
                if (metric5h.reset_at) {
                    const d = new Date(metric5h.reset_at)
                    if (!nearest5hResetTime || d < nearest5hResetTime) nearest5hResetTime = d
                }
            }

            if (metricWeekly) {
                const rem = metricWeekly.remaining_credits ?? 0
                const pct = metricWeekly.usage_percentage ?? 0
                totalWeeklyCredits += rem
                if (pct >= 100 || rem === 0) {
                    isWeeklyThrottled = true
                    throttledWeeklyCount += 1
                }
            }

            if (!is5hThrottled && !isWeeklyThrottled && item.enabled !== false && !item.muted && !item.error) {
                readyCount += 1
            }
        })

        return {
            totalAccounts: geminiQuotas.length,
            total5hCredits,
            totalWeeklyCredits,
            readyCount,
            throttled5hCount,
            throttledWeeklyCount,
            totalThrottled: throttled5hCount + throttledWeeklyCount,
            nearestResetStr: nearest5hResetTime
                ? nearest5hResetTime.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
                : null,
        }
    }, [geminiQuotas])

    // Token Usage Timeline (16 buckets)
    const timelineBars = useMemo(() => {
        const bucketCount = 16
        if (!historyItems.length) {
            return Array.from({ length: bucketCount }, (_, i) => ({
                id: i,
                prompt: 0,
                completion: 0,
                total: 0,
                count: 0,
                label: `Step ${i + 1}`,
            }))
        }

        const reversed = [...historyItems].reverse()
        const chunkSize = Math.max(1, Math.ceil(reversed.length / bucketCount))
        const buckets = []

        for (let i = 0; i < bucketCount; i++) {
            const slice = reversed.slice(i * chunkSize, (i + 1) * chunkSize)
            let p = 0
            let c = 0
            let tot = 0
            slice.forEach((item) => {
                const est = estimateItemTokens(item)
                p += est.prompt
                c += est.completion
                tot += est.total
            })
            buckets.push({
                id: i,
                prompt: p,
                completion: c,
                total: tot,
                count: slice.length,
                label: slice.length > 0 && slice[0].created_at
                    ? new Date(slice[0].created_at * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
                    : `${i + 1}`,
            })
        }
        return buckets
    }, [historyItems])

    const maxBucketTotal = useMemo(() => {
        const max = Math.max(...timelineBars.map((b) => b.total), 1000)
        return max
    }, [timelineBars])

    // Gateway base URL and keys
    const baseUrl = typeof window !== 'undefined'
        ? `${window.location.protocol}//${window.location.host}/v1`
        : 'http://localhost:5001/v1'

    const configuredKeys = config?.keys || []
    const firstKey = configuredKeys[0] || 'sk-your-ds2api-key'
    const keyCount = configuredKeys.length

    // Snippets
    const snippets = {
        curl: `curl ${baseUrl}/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer ${firstKey}" \\
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "Hello, DS2API!"}],
    "stream": true
  }'`,
        python: `from openai import OpenAI

client = OpenAI(
    api_key="${firstKey}",
    base_url="${baseUrl}"
)

response = client.chat.completions.create(
    model="deepseek-chat",
    messages=[{"role": "user", "content": "Hello, DS2API!"}],
    stream=True
)

for chunk in response:
    print(chunk.choices[0].delta.content or "", end="")`,
        node: `import OpenAI from "openai";

const client = new OpenAI({
  apiKey: "${firstKey}",
  baseURL: "${baseUrl}"
});

const stream = await client.chat.completions.create({
  model: "deepseek-chat",
  messages: [{ role: "user", content: "Hello, DS2API!" }],
  stream: true,
});

for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content || "");
}`
    }

    const recentStream = historyItems.slice(0, 6)

    return (
        <div className="space-y-6 pb-12">
            {/* Header Telemetry Row */}
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold tracking-tight text-foreground flex items-center gap-3">
                        {t('overview.title')}
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-emerald-500/10 text-emerald-500 border border-emerald-500/20">
                            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
                            {t('overview.liveGateway')}
                        </span>
                    </h1>
                    <p className="text-sm text-muted-foreground mt-1">
                        {t('overview.desc')}
                    </p>
                </div>
                <div className="flex items-center gap-3">
                    <button
                        onClick={() => loadData(true)}
                        disabled={refreshing}
                        className="inline-flex items-center gap-2 px-3.5 py-2 rounded-xl text-xs font-medium border border-border bg-card/80 text-foreground hover:bg-secondary transition-all disabled:opacity-50"
                        title={t('overview.refreshData')}
                    >
                        <RefreshCw className={clsx("w-3.5 h-3.5", refreshing && "animate-spin text-primary")} />
                        {t('overview.refreshData')}
                    </button>
                    <button
                        onClick={() => onNavigate('keys')}
                        className="inline-flex items-center gap-2 px-4 py-2 rounded-xl text-xs font-semibold bg-primary text-primary-foreground hover:opacity-90 transition-all shadow-sm"
                    >
                        <Key className="w-3.5 h-3.5" />
                        {t('apiKeysManager.createKey')}
                    </button>
                </div>
            </div>

            {/* Core Metrics Grid */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
                {/* 1. Total Tokens Hero Card */}
                <div className="p-5 rounded-2xl border border-border/80 bg-card/90 shadow-sm flex flex-col justify-between relative overflow-hidden group">
                    <div className="flex items-start justify-between">
                        <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                            {t('overview.totalTokens')}
                        </div>
                        <div className="p-2 rounded-xl bg-primary/10 text-primary">
                            <Zap className="w-4 h-4" />
                        </div>
                    </div>
                    <div className="mt-3">
                        <div className="text-3xl font-extrabold tracking-tight font-mono text-foreground">
                            {telemetry.totalTokens.toLocaleString()}
                        </div>
                        <div className="flex items-center gap-3 mt-3 pt-3 border-t border-border/60 text-xs text-muted-foreground">
                            <span className="flex items-center gap-1">
                                <span className="w-2 h-2 rounded-full bg-sky-400" />
                                <span className="font-mono">{telemetry.totalPrompt.toLocaleString()}</span> in
                            </span>
                            <span className="flex items-center gap-1">
                                <span className="w-2 h-2 rounded-full bg-emerald-400" />
                                <span className="font-mono">{telemetry.totalCompletion.toLocaleString()}</span> out
                            </span>
                            {telemetry.totalReasoning > 0 && (
                                <span className="flex items-center gap-1">
                                    <span className="w-2 h-2 rounded-full bg-purple-400" />
                                    <span className="font-mono">{telemetry.totalReasoning.toLocaleString()}</span> r
                                </span>
                            )}
                        </div>
                    </div>
                </div>

                {/* 2. Requests & Success Rate */}
                <div className="p-5 rounded-2xl border border-border/80 bg-card/90 shadow-sm flex flex-col justify-between">
                    <div className="flex items-start justify-between">
                        <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                            {t('overview.totalRequests')}
                        </div>
                        <div className="p-2 rounded-xl bg-blue-500/10 text-blue-500">
                            <Activity className="w-4 h-4" />
                        </div>
                    </div>
                    <div className="mt-3">
                        <div className="text-3xl font-extrabold tracking-tight font-mono text-foreground">
                            {telemetry.totalReqs.toLocaleString()}
                        </div>
                        <div className="flex items-center justify-between mt-3 pt-3 border-t border-border/60 text-xs">
                            <span className="text-muted-foreground">{t('overview.successRate')}</span>
                            <span className={clsx(
                                "font-mono font-bold px-1.5 py-0.5 rounded",
                                telemetry.successRate >= 95 ? "text-emerald-400 bg-emerald-500/10" : "text-amber-400 bg-amber-500/10"
                            )}>
                                {telemetry.successRate}%
                            </span>
                        </div>
                    </div>
                </div>

                {/* 3. Upstream Nodes Pool */}
                <div className="p-5 rounded-2xl border border-border/80 bg-card/90 shadow-sm flex flex-col justify-between">
                    <div className="flex items-start justify-between">
                        <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                            {t('overview.upstreamNodes')}
                        </div>
                        <div className="p-2 rounded-xl bg-purple-500/10 text-purple-400">
                            <Cpu className="w-4 h-4" />
                        </div>
                    </div>
                    <div className="mt-3">
                        <div className="text-3xl font-extrabold tracking-tight font-mono text-foreground flex items-baseline gap-2">
                            <span>{upstreamHealth.ready}</span>
                            <span className="text-sm font-normal text-muted-foreground font-sans">
                                / {upstreamHealth.total} active
                            </span>
                        </div>
                        <div className="flex items-center gap-2 mt-3 pt-3 border-t border-border/60 text-xs text-muted-foreground">
                            {upstreamHealth.cooldown > 0 && (
                                <span className="text-amber-400 font-medium">
                                    {upstreamHealth.cooldown} cooldown
                                </span>
                            )}
                            {upstreamHealth.disabled > 0 && (
                                <span className="text-red-400 font-medium">
                                    {upstreamHealth.disabled} disabled
                                </span>
                            )}
                            {upstreamHealth.cooldown === 0 && upstreamHealth.disabled === 0 && (
                                <span className="text-emerald-400 flex items-center gap-1 font-medium">
                                    <ShieldCheck className="w-3.5 h-3.5" /> All nodes ready
                                </span>
                            )}
                        </div>
                    </div>
                </div>

                {/* 4. Latency & Keys Status */}
                <div className="p-5 rounded-2xl border border-border/80 bg-card/90 shadow-sm flex flex-col justify-between">
                    <div className="flex items-start justify-between">
                        <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                            {t('overview.avgLatency')}
                        </div>
                        <div className="p-2 rounded-xl bg-emerald-500/10 text-emerald-500">
                            <Clock className="w-4 h-4" />
                        </div>
                    </div>
                    <div className="mt-3">
                        <div className="text-3xl font-extrabold tracking-tight font-mono text-foreground">
                            {telemetry.avgLatencyMs > 0 ? `${telemetry.avgLatencyMs}ms` : '—'}
                        </div>
                        <div className="flex items-center justify-between mt-3 pt-3 border-t border-border/60 text-xs">
                            <span className="text-muted-foreground">{t('overview.activeKeys')}</span>
                            <span className="font-mono font-bold text-primary">
                                {keyCount} {t('sidebar.keys').toLowerCase()}
                            </span>
                        </div>
                    </div>
                </div>
            </div>

            {/* Gemini Compute Pool & Quota Section (Visible if Gemini accounts exist) */}
            {geminiQuotas.length > 0 && (
                <div className="p-5 rounded-2xl border border-purple-500/30 bg-card/90 shadow-sm space-y-4">
                    <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                        <div className="flex items-center gap-2.5">
                            <div className="w-8 h-8 rounded-xl bg-purple-500/10 text-purple-400 border border-purple-500/20 flex items-center justify-center shrink-0">
                                <Gauge className="w-4 h-4" />
                            </div>
                            <div>
                                <h2 className="text-sm font-bold text-foreground flex items-center gap-2 flex-wrap">
                                    {t('accountManager.quota.poolTitle')}
                                    <span className="text-[10px] font-mono px-2 py-0.5 rounded-full bg-indigo-500/10 text-indigo-400 border border-indigo-500/20 font-semibold">
                                        {geminiPoolStats.totalWeeklyCredits.toLocaleString()} Weekly
                                    </span>
                                    <span className="text-[10px] font-mono px-2 py-0.5 rounded-full bg-purple-500/10 text-purple-400 border border-purple-500/20 font-semibold">
                                        {geminiPoolStats.total5hCredits.toLocaleString()} 5h Credits
                                    </span>
                                </h2>
                                <p className="text-[11px] text-muted-foreground mt-0.5">
                                    {geminiPoolStats.readyCount} / {geminiPoolStats.totalAccounts} {t('accountManager.quota.readyAccounts').toLowerCase()}
                                    {geminiPoolStats.totalThrottled > 0 && (
                                        <span className="text-amber-400 font-medium">
                                            {` · ${geminiPoolStats.totalThrottled} ${t('accountManager.quota.throttledAccounts').toLowerCase()}`}
                                            {geminiPoolStats.nearestResetStr && ` (~${geminiPoolStats.nearestResetStr})`}
                                        </span>
                                    )}
                                </p>
                            </div>
                        </div>

                        <button
                            onClick={() => onNavigate('accounts')}
                            className="text-xs font-semibold text-purple-400 hover:text-purple-300 transition-colors flex items-center gap-1 self-start sm:self-auto"
                        >
                            {t('accountManager.quota.viewDetails')} &rarr;
                        </button>
                    </div>

                    {/* Account pills */}
                    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                        {geminiQuotas.map((item) => {
                            const metric5h = item.quota?.usage?.current_5h
                            const metricWeekly = item.quota?.usage?.weekly
                            const usedPct = metric5h?.usage_percentage ?? 0
                            const remaining = metric5h?.remaining_credits
                            const resetTime = metric5h?.reset_at
                                ? new Date(metric5h.reset_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
                                : null
                            const is5hThrottled = usedPct >= 100 || remaining === 0
                            const isWeeklyThrottled = (metricWeekly?.usage_percentage ?? 0) >= 100 || (metricWeekly?.remaining_credits ?? 0) === 0

                            return (
                                <div
                                    key={item.identifier}
                                    className={clsx(
                                        "p-3 rounded-xl border text-xs flex flex-col justify-between space-y-2.5",
                                        isWeeklyThrottled ? "border-rose-500/40 bg-rose-500/5" :
                                        is5hThrottled ? "border-amber-500/40 bg-amber-500/5" :
                                        item.error ? "border-destructive/30 bg-destructive/5" :
                                        "border-border/70 bg-background/60"
                                    )}
                                >
                                    <div className="flex items-center justify-between gap-2">
                                        <span className="font-semibold text-foreground truncate font-mono">
                                            {item.name || item.identifier}
                                        </span>
                                        <span className={clsx(
                                            "px-1.5 py-0.2 rounded text-[10px] font-bold font-mono uppercase",
                                            item.quota?.tier?.label === 'ULTRA' ? "text-amber-400 bg-amber-500/10" : "text-purple-400 bg-purple-500/10"
                                        )}>
                                            {item.quota?.tier?.label || 'PRO'}
                                        </span>
                                    </div>

                                    {metric5h ? (
                                        <div className="space-y-1.5">
                                            <div className="flex items-center justify-between text-[11px]">
                                                <span className="text-muted-foreground flex items-center gap-1">
                                                    <Clock className="w-3 h-3 text-purple-400" />
                                                    5h:
                                                </span>
                                                <span className="font-mono font-bold text-foreground">
                                                    {remaining} credits ({100 - usedPct}%)
                                                </span>
                                            </div>
                                            <div className="h-1.5 w-full bg-secondary rounded-full overflow-hidden">
                                                <div
                                                    className={clsx(
                                                        "h-full rounded-full transition-all duration-300",
                                                        is5hThrottled ? "bg-amber-500" : "bg-emerald-500"
                                                    )}
                                                    style={{ width: `${Math.max(4, Math.min(100, 100 - usedPct))}%` }}
                                                />
                                            </div>

                                            {metricWeekly && (
                                                <div className="flex items-center justify-between text-[11px] pt-1 border-t border-border/40">
                                                    <span className="text-muted-foreground flex items-center gap-1">
                                                        <Calendar className="w-3 h-3 text-indigo-400" />
                                                        Weekly:
                                                    </span>
                                                    <span className="font-mono font-medium text-foreground">
                                                        {metricWeekly.remaining_credits !== undefined ? `${metricWeekly.remaining_credits} credits` : '--'}
                                                    </span>
                                                </div>
                                            )}

                                            {resetTime && (
                                                <div className="text-[10px] text-muted-foreground font-mono text-right">
                                                    {t('accountManager.quota.resetAt', { time: resetTime })}
                                                </div>
                                            )}
                                        </div>
                                    ) : (
                                        <div className="text-[11px] text-muted-foreground italic truncate">
                                            {item.error || 'Cookies ready'}
                                        </div>
                                    )}
                                </div>
                            )
                        })}
                    </div>
                </div>
            )}

            {/* Middle Row: Token Consumption Chart & Model Distribution */}
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                {/* Visualizer: Token Burn Trend */}
                <div className="lg:col-span-2 p-6 rounded-2xl border border-border/80 bg-card/90 shadow-sm flex flex-col justify-between">
                    <div className="flex items-center justify-between mb-4">
                        <div>
                            <h2 className="text-base font-bold text-foreground flex items-center gap-2">
                                <TrendingUp className="w-4 h-4 text-primary" />
                                {t('overview.tokenUsageTrend')}
                            </h2>
                            <p className="text-xs text-muted-foreground mt-0.5">
                                Breakdown of input (prompt) vs output (completion) tokens across recent requests
                            </p>
                        </div>
                        <div className="flex items-center gap-3 text-xs text-muted-foreground">
                            <span className="flex items-center gap-1.5">
                                <span className="w-2.5 h-2.5 rounded-sm bg-sky-500/80" />
                                Prompt
                            </span>
                            <span className="flex items-center gap-1.5">
                                <span className="w-2.5 h-2.5 rounded-sm bg-emerald-500/80" />
                                Completion
                            </span>
                        </div>
                    </div>

                    {/* Chart area */}
                    <div className="h-44 flex items-end gap-1.5 pt-4 pb-2 px-1 relative">
                        {timelineBars.map((b) => {
                            const promptHeight = maxBucketTotal > 0 ? (b.prompt / maxBucketTotal) * 100 : 0
                            const completionHeight = maxBucketTotal > 0 ? (b.completion / maxBucketTotal) * 100 : 0
                            const isHovered = hoveredBucket === b.id

                            return (
                                <div
                                    key={b.id}
                                    className="flex-1 flex flex-col justify-end h-full group relative cursor-pointer"
                                    onMouseEnter={() => setHoveredBucket(b.id)}
                                    onMouseLeave={() => setHoveredBucket(null)}
                                >
                                    {/* Tooltip */}
                                    {isHovered && b.total > 0 && (
                                        <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-20 px-2.5 py-1.5 rounded-lg bg-popover border border-border shadow-xl text-[11px] whitespace-nowrap pointer-events-none">
                                            <div className="font-semibold text-foreground">{b.label}</div>
                                            <div className="text-sky-400 font-mono">In: {b.prompt.toLocaleString()}</div>
                                            <div className="text-emerald-400 font-mono">Out: {b.completion.toLocaleString()}</div>
                                            <div className="font-bold text-foreground font-mono mt-0.5">Total: {b.total.toLocaleString()}</div>
                                        </div>
                                    )}

                                    <div className="w-full flex flex-col justify-end rounded-t-sm overflow-hidden bg-muted/40 transition-all duration-150">
                                        <div
                                            className="w-full bg-emerald-500/85 transition-all duration-300"
                                            style={{ height: `${Math.min(100, completionHeight)}%` }}
                                        />
                                        <div
                                            className="w-full bg-sky-500/85 transition-all duration-300"
                                            style={{ height: `${Math.min(100, promptHeight)}%` }}
                                        />
                                    </div>
                                    <div className="text-[10px] text-muted-foreground text-center truncate mt-1">
                                        {b.label}
                                    </div>
                                </div>
                            )
                        })}
                    </div>
                </div>

                {/* Model Distribution Panel */}
                <div className="p-6 rounded-2xl border border-border/80 bg-card/90 shadow-sm flex flex-col justify-between">
                    <div>
                        <h2 className="text-base font-bold text-foreground flex items-center gap-2">
                            <Layers className="w-4 h-4 text-purple-400" />
                            {t('overview.modelDistribution')}
                        </h2>
                        <p className="text-xs text-muted-foreground mt-0.5">
                            Tokens consumed by requested LLM model
                        </p>

                        <div className="mt-5 space-y-4">
                            {telemetry.models.length > 0 ? (
                                telemetry.models.map((m) => {
                                    const isDeepSeekChat = m.name.includes('chat')
                                    const isDeepSeekReasoner = m.name.includes('reasoner') || m.name.includes('r1')
                                    const isGemini = m.name.includes('gemini')

                                    const barColor = isDeepSeekReasoner
                                        ? 'bg-purple-500'
                                        : isGemini
                                            ? 'bg-blue-500'
                                            : isDeepSeekChat
                                                ? 'bg-emerald-500'
                                                : 'bg-primary'

                                    return (
                                        <div key={m.name} className="space-y-1.5">
                                            <div className="flex items-center justify-between text-xs">
                                                <span className="font-semibold text-foreground font-mono">{m.name}</span>
                                                <span className="font-mono text-muted-foreground">
                                                    {m.tokens.toLocaleString()} ({m.percentage}%)
                                                </span>
                                            </div>
                                            <div className="w-full h-2 rounded-full bg-muted overflow-hidden">
                                                <div
                                                    className={clsx("h-full rounded-full transition-all duration-500", barColor)}
                                                    style={{ width: `${Math.max(4, m.percentage)}%` }}
                                                />
                                            </div>
                                        </div>
                                    )
                                })
                            ) : (
                                <div className="text-xs text-muted-foreground py-8 text-center">
                                    {t('overview.noRequestsYet')}
                                </div>
                            )}
                        </div>
                    </div>

                    <div className="pt-4 border-t border-border/60 flex items-center justify-between text-xs text-muted-foreground">
                        <span>Total Active Models:</span>
                        <span className="font-mono font-bold text-foreground">{telemetry.models.length}</span>
                    </div>
                </div>
            </div>

            {/* Bottom Row: Quick Integration Console & Recent Activity */}
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {/* Quick Integration Card */}
                <div className="p-6 rounded-2xl border border-border/80 bg-card/90 shadow-sm flex flex-col justify-between">
                    <div>
                        <div className="flex items-center justify-between mb-4">
                            <h2 className="text-base font-bold text-foreground flex items-center gap-2">
                                <Terminal className="w-4 h-4 text-primary" />
                                {t('overview.quickIntegration')}
                            </h2>
                            <div className="flex items-center gap-1 p-1 rounded-lg bg-secondary/80 border border-border">
                                {['curl', 'python', 'node'].map((tab) => (
                                    <button
                                        key={tab}
                                        onClick={() => setActiveSnippetTab(tab)}
                                        className={clsx(
                                            "px-2.5 py-1 rounded text-xs font-medium transition-colors",
                                            activeSnippetTab === tab
                                                ? "bg-card text-foreground shadow-sm"
                                                : "text-muted-foreground hover:text-foreground"
                                        )}
                                    >
                                        {tab === 'curl' ? t('overview.snippetTabCurl') : tab === 'python' ? t('overview.snippetTabPython') : t('overview.snippetTabNode')}
                                    </button>
                                ))}
                            </div>
                        </div>

                        {/* Integration Details Bar */}
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-4">
                            <div className="p-3 rounded-xl border border-border bg-background/80 flex items-center justify-between">
                                <div className="min-w-0">
                                    <div className="text-[10px] uppercase font-bold text-muted-foreground">{t('overview.baseUrl')}</div>
                                    <div className="text-xs font-mono font-semibold truncate text-foreground mt-0.5">{baseUrl}</div>
                                </div>
                                <button
                                    onClick={() => copyToClipboard(baseUrl, 'baseUrl')}
                                    className="p-1.5 rounded-lg hover:bg-secondary text-muted-foreground hover:text-foreground"
                                    title={t('overview.copySnippet')}
                                >
                                    {copiedTarget === 'baseUrl' ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                                </button>
                            </div>

                            <div className="p-3 rounded-xl border border-border bg-background/80 flex items-center justify-between">
                                <div className="min-w-0">
                                    <div className="text-[10px] uppercase font-bold text-muted-foreground">{t('overview.defaultApiKey')}</div>
                                    <div className="text-xs font-mono font-semibold truncate text-foreground mt-0.5">
                                        {firstKey ? `${firstKey.slice(0, 10)}...${firstKey.slice(-4)}` : 'sk-...'}
                                    </div>
                                </div>
                                <button
                                    onClick={() => copyToClipboard(firstKey, 'apiKey')}
                                    className="p-1.5 rounded-lg hover:bg-secondary text-muted-foreground hover:text-foreground"
                                    title={t('overview.copySnippet')}
                                >
                                    {copiedTarget === 'apiKey' ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                                </button>
                            </div>
                        </div>

                        {/* Code snippet block */}
                        <div className="relative rounded-xl border border-border/80 bg-zinc-950 p-4 font-mono text-xs overflow-x-auto custom-scrollbar">
                            <button
                                onClick={() => copyToClipboard(snippets[activeSnippetTab], 'code')}
                                className="absolute top-3 right-3 px-2.5 py-1 rounded-lg bg-zinc-800 text-zinc-300 hover:text-white text-[11px] font-sans flex items-center gap-1.5 border border-zinc-700 transition-colors"
                            >
                                {copiedTarget === 'code' ? (
                                    <>
                                        <Check className="w-3 h-3 text-emerald-400" />
                                        <span>{t('overview.copied')}</span>
                                    </>
                                ) : (
                                    <>
                                        <Copy className="w-3 h-3" />
                                        <span>{t('overview.copySnippet')}</span>
                                    </>
                                )}
                            </button>
                            <pre className="text-zinc-300 leading-relaxed">
                                {snippets[activeSnippetTab]}
                            </pre>
                        </div>
                    </div>

                    <div className="mt-4 pt-3 border-t border-border/60 flex items-center justify-between text-xs">
                        <span className="text-muted-foreground">Looking to manage access keys?</span>
                        <button
                            onClick={() => onNavigate('keys')}
                            className="text-primary hover:underline font-medium inline-flex items-center gap-1"
                        >
                            {t('nav.keys.label')} <ArrowRight className="w-3.5 h-3.5" />
                        </button>
                    </div>
                </div>

                {/* Recent Request Stream */}
                <div className="p-6 rounded-2xl border border-border/80 bg-card/90 shadow-sm flex flex-col justify-between">
                    <div>
                        <div className="flex items-center justify-between mb-4">
                            <h2 className="text-base font-bold text-foreground flex items-center gap-2">
                                <Activity className="w-4 h-4 text-emerald-400" />
                                {t('overview.recentRequests')}
                            </h2>
                            <button
                                onClick={() => onNavigate('history')}
                                className="text-xs text-primary hover:underline font-medium inline-flex items-center gap-1"
                            >
                                {t('overview.viewAllLogs')} <ArrowRight className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        {recentStream.length > 0 ? (
                            <div className="divide-y divide-border/60">
                                {recentStream.map((item) => {
                                    const est = estimateItemTokens(item)
                                    const isSuccess = item.status === 'ok' || (item.status_code && item.status_code < 400)
                                    const timeStr = item.created_at
                                        ? new Date(item.created_at * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
                                        : '—'

                                    return (
                                        <div key={item.id} className="py-3 flex items-center justify-between gap-3 text-xs">
                                            <div className="flex items-center gap-2.5 min-w-0">
                                                <span className={clsx(
                                                    "w-2 h-2 rounded-full shrink-0",
                                                    isSuccess ? "bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.5)]" : "bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.5)]"
                                                )} />
                                                <div className="min-w-0">
                                                    <div className="font-semibold text-foreground font-mono truncate">
                                                        {item.model || 'deepseek-chat'}
                                                    </div>
                                                    <div className="text-[11px] text-muted-foreground truncate max-w-[200px] sm:max-w-xs">
                                                        {item.user_input || item.preview || item.id}
                                                    </div>
                                                </div>
                                            </div>

                                            <div className="text-right shrink-0">
                                                <div className="font-mono font-bold text-foreground">
                                                    {est.total.toLocaleString()} <span className="text-[10px] font-normal text-muted-foreground font-sans">tokens</span>
                                                </div>
                                                <div className="text-[10px] text-muted-foreground font-mono">
                                                    {item.elapsed_ms ? `${item.elapsed_ms}ms` : timeStr}
                                                </div>
                                            </div>
                                        </div>
                                    )
                                })}
                            </div>
                        ) : (
                            <div className="text-xs text-muted-foreground py-12 text-center">
                                {t('overview.noRequestsYet')}
                            </div>
                        )}
                    </div>

                    <div className="mt-4 pt-3 border-t border-border/60 flex items-center justify-between text-xs text-muted-foreground">
                        <span>Showing {recentStream.length} latest requests</span>
                        <button
                            onClick={() => onNavigate('history')}
                            className="text-primary hover:underline font-medium"
                        >
                            Open Token Ledger &rarr;
                        </button>
                    </div>
                </div>
            </div>
        </div>
    )
}
