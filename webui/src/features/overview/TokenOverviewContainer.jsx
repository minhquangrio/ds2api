import { useState, useEffect, useMemo, useCallback, useRef } from 'react'
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
    Radio,
    BarChart3,
    SlidersHorizontal,
    Timer,
    Play,
    Pause,
    RotateCcw,
} from 'lucide-react'
import clsx from 'clsx'

import { useI18n } from '../../i18n'
import { estimateItemTokens } from '../chatHistory/ChatHistoryContainer'

export function parseItemTimestamp(ts) {
    if (!ts) return null
    const num = Number(ts)
    if (!Number.isFinite(num) || num <= 0) return null
    return num > 1e11 ? num : num * 1000
}

function formatTokenCount(num) {
    if (!num || num <= 0) return '0'
    if (num >= 1_000_000) return `${(num / 1_000_000).toFixed(1)}M`
    if (num >= 10_000) return `${Math.round(num / 1_000)}k`
    if (num >= 1_000) return `${(num / 1_000).toFixed(1)}k`
    return String(num)
}

const TIME_RANGE_OPTIONS = [
    { key: 'all', labelKey: 'overview.rangeAll' },
    { key: '15m', labelKey: 'overview.range15m', durationMs: 15 * 60 * 1000 },
    { key: '1h', labelKey: 'overview.range1h', durationMs: 60 * 60 * 1000 },
    { key: '24h', labelKey: 'overview.range24h', durationMs: 24 * 60 * 60 * 1000 },
    { key: '7d', labelKey: 'overview.range7d', durationMs: 7 * 24 * 60 * 60 * 1000 },
    { key: '30d', labelKey: 'overview.range30d', durationMs: 30 * 24 * 60 * 60 * 1000 },
    { key: 'custom', labelKey: 'overview.rangeCustom' },
]

const CADENCE_OPTIONS = [
    { ms: 3000, labelKey: 'overview.cadence3s', short: '3s' },
    { ms: 5000, labelKey: 'overview.cadence5s', short: '5s' },
    { ms: 10000, labelKey: 'overview.cadence10s', short: '10s' },
]

export default function TokenOverviewContainer({ config, authFetch, onNavigate, onMessage }) {
    const { t, lang } = useI18n()
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

    // Time filter states
    const [timeRange, setTimeRange] = useState(() => {
        if (typeof localStorage === 'undefined') return 'all'
        const stored = localStorage.getItem('ds2api_overview_timerange')
        return stored || 'all'
    })
    const [customStart, setCustomStart] = useState('')
    const [customEnd, setCustomEnd] = useState('')
    const [customAppliedStart, setCustomAppliedStart] = useState('')
    const [customAppliedEnd, setCustomAppliedEnd] = useState('')
    const [chartViewMode, setChartViewMode] = useState('buckets') // 'buckets' | 'requests'

    // Real-time live monitoring states
    const [realtimeEnabled, setRealtimeEnabled] = useState(() => {
        if (typeof localStorage === 'undefined') return false
        return localStorage.getItem('ds2api_overview_realtime') === 'true'
    })
    const [refreshCadence, setRefreshCadence] = useState(() => {
        if (typeof localStorage === 'undefined') return 5000
        const stored = Number(localStorage.getItem('ds2api_overview_cadence'))
        return [3000, 5000, 10000].includes(stored) ? stored : 5000
    })
    const [lastUpdatedTime, setLastUpdatedTime] = useState(null)
    const [dataPulse, setDataPulse] = useState(false)

    const historyETagRef = useRef('')
    const inFlightRef = useRef(false)

    useEffect(() => {
        if (typeof localStorage === 'undefined') return
        localStorage.setItem('ds2api_overview_timerange', timeRange)
    }, [timeRange])

    useEffect(() => {
        if (typeof localStorage === 'undefined') return
        localStorage.setItem('ds2api_overview_realtime', String(realtimeEnabled))
    }, [realtimeEnabled])

    useEffect(() => {
        if (typeof localStorage === 'undefined') return
        localStorage.setItem('ds2api_overview_cadence', String(refreshCadence))
    }, [refreshCadence])

    const copyToClipboard = useCallback((text, targetId) => {
        navigator.clipboard.writeText(text).then(() => {
            setCopiedTarget(targetId)
            setTimeout(() => setCopiedTarget(null), 1800)
        })
    }, [])

    const loadData = useCallback(async (isSilent = false) => {
        if (inFlightRef.current) return
        inFlightRef.current = true

        if (!isSilent) setLoading(true)
        else setRefreshing(true)

        try {
            const historyHeaders = {}
            if (isSilent && historyETagRef.current) {
                historyHeaders['If-None-Match'] = historyETagRef.current
            }

            const [historyRes, accountsRes, queueRes, geminiQuotasRes] = await Promise.allSettled([
                apiFetch('/admin/chat-history?limit=100', { headers: historyHeaders }),
                apiFetch('/admin/accounts'),
                apiFetch('/admin/queue/status'),
                apiFetch('/admin/accounts/gemini/quotas')
            ])

            let hasNewHistory = false
            if (historyRes.status === 'fulfilled') {
                const res = historyRes.value
                if (res.ok) {
                    const etag = res.headers?.get('ETag') || ''
                    if (etag) historyETagRef.current = etag
                    const data = await res.json()
                    const items = Array.isArray(data) ? data : (data.items || [])
                    setHistoryItems(items)
                    hasNewHistory = true
                } else if (res.status === 304) {
                    // Cache fresh, no change needed
                }
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

            setLastUpdatedTime(new Date())
            if (hasNewHistory && isSilent) {
                setDataPulse(true)
                setTimeout(() => setDataPulse(false), 800)
            }
        } catch (_err) {
            // Best effort load
        } finally {
            inFlightRef.current = false
            setLoading(false)
            setRefreshing(false)
        }
    }, [apiFetch, config?.accounts])

    // Initial load
    useEffect(() => {
        loadData()
    }, [loadData])

    // Real-time live polling interval
    useEffect(() => {
        if (!realtimeEnabled) return undefined
        const timer = window.setInterval(() => {
            loadData(true)
        }, refreshCadence)
        return () => window.clearInterval(timer)
    }, [realtimeEnabled, refreshCadence, loadData])

    // Filter items based on selected timeRange
    const filteredHistoryItems = useMemo(() => {
        if (!historyItems.length) return []
        if (timeRange === 'all') return historyItems

        const now = Date.now()
        if (timeRange === 'custom') {
            const startMs = customAppliedStart ? new Date(customAppliedStart).getTime() : 0
            const endMs = customAppliedEnd ? new Date(customAppliedEnd).getTime() : Infinity
            return historyItems.filter((item) => {
                const ts = parseItemTimestamp(item.created_at)
                if (!ts) return false
                return ts >= startMs && ts <= endMs
            })
        }

        const opt = TIME_RANGE_OPTIONS.find((o) => o.key === timeRange)
        if (!opt || !opt.durationMs) return historyItems

        const cutoff = now - opt.durationMs
        return historyItems.filter((item) => {
            const ts = parseItemTimestamp(item.created_at)
            return ts && ts >= cutoff
        })
    }, [historyItems, timeRange, customAppliedStart, customAppliedEnd])

    // Streaming requests count
    const streamingCount = useMemo(() => {
        return historyItems.filter((item) => item.status === 'streaming').length
    }, [historyItems])

    // Compute token analytics based on FILTERED history items
    const telemetry = useMemo(() => {
        let totalPrompt = 0
        let totalCompletion = 0
        let totalReasoning = 0
        let totalTokens = 0
        let successRequests = 0
        let totalElapsedMs = 0
        let measuredElapsedCount = 0
        let minTs = Infinity
        let maxTs = 0

        const modelStats = {}

        filteredHistoryItems.forEach((item) => {
            const { prompt, completion, total } = estimateItemTokens(item)
            const reasoning = Number(item.reasoning_tokens) || 0

            totalPrompt += prompt
            totalCompletion += completion
            totalReasoning += reasoning
            totalTokens += total

            const ts = parseItemTimestamp(item.created_at)
            if (ts) {
                if (ts < minTs) minTs = ts
                if (ts > maxTs) maxTs = ts
            }

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

        const totalReqs = filteredHistoryItems.length
        const successRate = totalReqs > 0 ? Math.round((successRequests / totalReqs) * 100) : 100
        const avgLatencyMs = measuredElapsedCount > 0 ? Math.round(totalElapsedMs / measuredElapsedCount) : 0

        // Calculate rate (tokens/s and tokens/min)
        let tokensPerSec = 0
        let tokensPerMin = 0
        if (totalReqs > 0 && maxTs > minTs) {
            const spanSec = Math.max(1, Math.round((maxTs - minTs) / 1000))
            tokensPerSec = Number((totalTokens / spanSec).toFixed(1))
            tokensPerMin = Math.round(tokensPerSec * 60)
        }

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
            tokensPerSec,
            tokensPerMin,
            models: sortedModels,
        }
    }, [filteredHistoryItems])

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

    // Gemini Pool Quota calculation
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

    // Token Usage Timeline Bars: Adaptive Buckets or Per-Request Flow
    const timelineBars = useMemo(() => {
        if (!filteredHistoryItems.length) return []

        if (chartViewMode === 'requests') {
            // Per-request view: take up to 20 latest requests, ordered chronologically
            const recent = filteredHistoryItems.slice(0, 20).reverse()
            return recent.map((item, idx) => {
                const est = estimateItemTokens(item)
                const ts = parseItemTimestamp(item.created_at)
                const date = ts ? new Date(ts) : null
                const label = date
                    ? date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
                    : `#${idx + 1}`
                const fullTime = date
                    ? `${date.toLocaleDateString([], { month: '2-digit', day: '2-digit' })} ${date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}`
                    : label

                return {
                    id: item.id || `req-${idx}`,
                    prompt: est.prompt,
                    completion: est.completion,
                    total: est.total,
                    model: item.model || '',
                    status: item.status || (item.status_code ? String(item.status_code) : ''),
                    elapsedMs: item.elapsed_ms || null,
                    count: 1,
                    label,
                    fullTime,
                }
            })
        }

        // Time Buckets View: Aggregate requests into time intervals
        const now = Date.now()
        let numBuckets = 16
        let bucketDurationMs = 60 * 1000 // default 1 min
        let windowStart = now - (numBuckets * bucketDurationMs)
        let isDateScale = false

        if (timeRange === '15m') {
            numBuckets = 15
            bucketDurationMs = 60 * 1000 // 1 min
            windowStart = now - (15 * 60 * 1000)
        } else if (timeRange === '1h') {
            numBuckets = 12
            bucketDurationMs = 5 * 60 * 1000 // 5 mins
            windowStart = now - (60 * 60 * 1000)
        } else if (timeRange === '24h') {
            numBuckets = 24
            bucketDurationMs = 60 * 60 * 1000 // 1 hour
            windowStart = now - (24 * 60 * 60 * 1000)
        } else if (timeRange === '7d') {
            numBuckets = 7
            bucketDurationMs = 24 * 60 * 60 * 1000 // 1 day
            windowStart = now - (7 * 24 * 60 * 60 * 1000)
            isDateScale = true
        } else if (timeRange === '30d') {
            numBuckets = 15
            bucketDurationMs = 2 * 24 * 60 * 60 * 1000 // 2 days
            windowStart = now - (30 * 24 * 60 * 60 * 1000)
            isDateScale = true
        } else {
            // 'all' or 'custom': compute dynamically from items
            let minTs = Infinity
            let maxTs = now
            filteredHistoryItems.forEach((item) => {
                const ts = parseItemTimestamp(item.created_at)
                if (ts && ts < minTs) minTs = ts
                if (ts && ts > maxTs) maxTs = ts
            })
            if (minTs === Infinity) minTs = now - 3600 * 1000
            const span = Math.max(60 * 1000, maxTs - minTs)
            numBuckets = Math.min(20, Math.max(8, Math.round(span / (60 * 1000))))
            bucketDurationMs = Math.ceil(span / numBuckets)
            windowStart = minTs
            if (span > 2 * 24 * 60 * 60 * 1000) isDateScale = true
        }

        // Initialize empty buckets
        const buckets = []
        for (let i = 0; i < numBuckets; i++) {
            const bStart = windowStart + (i * bucketDurationMs)
            const bEnd = bStart + bucketDurationMs
            const date = new Date(bStart)
            const endDate = new Date(bEnd)

            let label = ''
            if (isDateScale) {
                label = `${date.getDate()}/${date.getMonth() + 1}`
            } else if (bucketDurationMs >= 3600 * 1000) {
                label = date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
            } else {
                label = date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
            }

            const fullTime = `${date.toLocaleDateString([], { month: '2-digit', day: '2-digit' })} ${date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} - ${endDate.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`

            buckets.push({
                id: `bucket-${i}`,
                bStart,
                bEnd,
                prompt: 0,
                completion: 0,
                total: 0,
                count: 0,
                elapsedSum: 0,
                models: new Set(),
                label,
                fullTime,
            })
        }

        // Fill buckets with filtered items
        filteredHistoryItems.forEach((item) => {
            const ts = parseItemTimestamp(item.created_at)
            if (!ts) return
            const est = estimateItemTokens(item)

            // Find matching bucket
            const bucket = buckets.find((b) => ts >= b.bStart && ts < b.bEnd)
            if (bucket) {
                bucket.prompt += est.prompt
                bucket.completion += est.completion
                bucket.total += est.total
                bucket.count += 1
                if (item.elapsed_ms) bucket.elapsedSum += item.elapsed_ms
                if (item.model) bucket.models.add(item.model)
            } else if (ts >= buckets[buckets.length - 1].bEnd) {
                // Attach to last bucket if slightly ahead
                const last = buckets[buckets.length - 1]
                last.prompt += est.prompt
                last.completion += est.completion
                last.total += est.total
                last.count += 1
                if (item.elapsed_ms) last.elapsedSum += item.elapsed_ms
                if (item.model) last.models.add(item.model)
            }
        })

        return buckets.map((b) => ({
            ...b,
            model: Array.from(b.models).slice(0, 2).join(', ') + (b.models.size > 2 ? '...' : ''),
            elapsedMs: b.count > 0 ? Math.round(b.elapsedSum / b.count) : null,
        }))
    }, [filteredHistoryItems, chartViewMode, timeRange])

    const maxBucketTotal = useMemo(() => {
        if (!timelineBars.length) return 1000
        const max = Math.max(...timelineBars.map((b) => b.total), 0)
        return max > 0 ? max : 1000
    }, [timelineBars])

    // Apply custom range
    const handleApplyCustomRange = () => {
        if (!customStart && !customEnd) return
        setCustomAppliedStart(customStart)
        setCustomAppliedEnd(customEnd)
    }

    // Reset filter
    const handleResetFilter = () => {
        setTimeRange('all')
        setCustomStart('')
        setCustomEnd('')
        setCustomAppliedStart('')
        setCustomAppliedEnd('')
    }

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

    const recentStream = filteredHistoryItems.slice(0, 6)

    return (
        <div className="space-y-6 pb-12">
            {/* Header Telemetry Row */}
            <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4">
                <div>
                    <h1 className="text-2xl font-bold tracking-tight text-foreground flex items-center gap-3 flex-wrap">
                        {t('overview.title')}
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-emerald-500/10 text-emerald-500 border border-emerald-500/20">
                            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
                            {t('overview.liveGateway')}
                        </span>
                        {streamingCount > 0 && (
                            <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-amber-500/10 text-amber-500 border border-amber-500/20 animate-pulse">
                                <span className="w-2 h-2 rounded-full bg-amber-500 animate-ping" />
                                {streamingCount} {t('overview.streamingInFlight')}
                            </span>
                        )}
                    </h1>
                    <p className="text-sm text-muted-foreground mt-1">
                        {t('overview.desc')}
                    </p>
                </div>

                {/* Control Actions: Real-time Live Mode & Manual Refresh */}
                <div className="flex flex-wrap items-center gap-2.5">
                    {/* Real-time Toggle Button */}
                    <div className="flex items-center rounded-xl border border-border bg-card/80 p-1 shadow-sm">
                        <button
                            onClick={() => setRealtimeEnabled(prev => !prev)}
                            className={clsx(
                                "inline-flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium transition-all",
                                realtimeEnabled
                                    ? "bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 shadow-sm"
                                    : "text-muted-foreground hover:text-foreground hover:bg-secondary/60"
                            )}
                            title={realtimeEnabled ? t('overview.realtimeActive') : t('overview.realtimePaused')}
                        >
                            <span className="relative flex h-2 w-2">
                                {realtimeEnabled && (
                                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                                )}
                                <span className={clsx(
                                    "relative inline-flex rounded-full h-2 w-2",
                                    realtimeEnabled ? "bg-emerald-500" : "bg-muted-foreground/40"
                                )} />
                            </span>
                            <span className="font-semibold">{t('overview.realtime')}</span>
                        </button>

                        {/* Cadence dropdown if realtime is enabled */}
                        {realtimeEnabled && (
                            <div className="flex items-center gap-0.5 pl-1.5 border-l border-border/60">
                                {CADENCE_OPTIONS.map((c) => (
                                    <button
                                        key={c.ms}
                                        onClick={() => setRefreshCadence(c.ms)}
                                        className={clsx(
                                            "px-2 py-1 rounded text-[11px] font-mono transition-colors",
                                            refreshCadence === c.ms
                                                ? "bg-primary text-primary-foreground font-bold shadow-xs"
                                                : "text-muted-foreground hover:text-foreground hover:bg-secondary"
                                        )}
                                        title={t(c.labelKey)}
                                    >
                                        {c.short}
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* Manual Refresh Button */}
                    <button
                        onClick={() => loadData(true)}
                        disabled={refreshing}
                        className="inline-flex items-center gap-2 px-3.5 py-2 rounded-xl text-xs font-medium border border-border bg-card/80 text-foreground hover:bg-secondary transition-all disabled:opacity-50"
                        title={lastUpdatedTime ? `${t('overview.refreshData')} (Last: ${lastUpdatedTime.toLocaleTimeString()})` : t('overview.refreshData')}
                    >
                        <RefreshCw className={clsx("w-3.5 h-3.5", refreshing && "animate-spin text-primary")} />
                        <span className="hidden sm:inline">{t('overview.refreshData')}</span>
                    </button>

                    {/* Create Key */}
                    <button
                        onClick={() => onNavigate('keys')}
                        className="inline-flex items-center gap-2 px-4 py-2 rounded-xl text-xs font-semibold bg-primary text-primary-foreground hover:opacity-90 transition-all shadow-sm"
                    >
                        <Key className="w-3.5 h-3.5" />
                        {t('apiKeysManager.createKey')}
                    </button>
                </div>
            </div>

            {/* Time Range Filter Bar */}
            <div className="p-4 rounded-2xl border border-border/80 bg-card/80 shadow-sm space-y-3">
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                    <div className="flex items-center gap-2">
                        <Calendar className="w-4 h-4 text-primary shrink-0" />
                        <span className="text-xs font-bold text-foreground uppercase tracking-wider">
                            {t('overview.timeRange')}
                        </span>
                        <span className="text-xs text-muted-foreground font-mono">
                            ({t('overview.filteredSummary', {
                                count: filteredHistoryItems.length,
                                tokens: formatTokenCount(telemetry.totalTokens)
                            })})
                        </span>
                    </div>

                    {/* Quick reset button if filtered and less than total */}
                    {timeRange !== 'all' && (
                        <button
                            onClick={handleResetFilter}
                            className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-primary transition-colors self-start sm:self-auto"
                        >
                            <RotateCcw className="w-3 h-3" />
                            {t('overview.resetFilter')}
                        </button>
                    )}
                </div>

                {/* Presets Button Pills */}
                <div className="flex flex-wrap items-center gap-1.5">
                    {TIME_RANGE_OPTIONS.map((opt) => {
                        const isActive = timeRange === opt.key
                        return (
                            <button
                                key={opt.key}
                                onClick={() => setTimeRange(opt.key)}
                                className={clsx(
                                    "px-3 py-1.5 rounded-xl text-xs font-medium transition-all border",
                                    isActive
                                        ? "bg-primary text-primary-foreground border-primary shadow-sm font-semibold"
                                        : "bg-background/80 text-muted-foreground border-border/70 hover:text-foreground hover:bg-secondary/70 hover:border-border"
                                )}
                                title={t('overview.filterPresetTooltip', { range: t(opt.labelKey) })}
                            >
                                {t(opt.labelKey)}
                            </button>
                        )
                    })}
                </div>

                {/* Custom Time Range Picker inputs (visible when 'custom' selected) */}
                {timeRange === 'custom' && (
                    <div className="pt-3 border-t border-border/60 flex flex-wrap items-center gap-3 text-xs">
                        <div className="flex items-center gap-2">
                            <span className="text-muted-foreground font-medium">{t('overview.customFrom')}:</span>
                            <input
                                type="datetime-local"
                                value={customStart}
                                onChange={(e) => setCustomStart(e.target.value)}
                                className="px-2.5 py-1.5 rounded-lg border border-border bg-background text-foreground text-xs font-mono focus:outline-hidden focus:ring-1 focus:ring-primary"
                            />
                        </div>

                        <div className="flex items-center gap-2">
                            <span className="text-muted-foreground font-medium">{t('overview.customTo')}:</span>
                            <input
                                type="datetime-local"
                                value={customEnd}
                                onChange={(e) => setCustomEnd(e.target.value)}
                                className="px-2.5 py-1.5 rounded-lg border border-border bg-background text-foreground text-xs font-mono focus:outline-hidden focus:ring-1 focus:ring-primary"
                            />
                        </div>

                        <button
                            onClick={handleApplyCustomRange}
                            className="px-3 py-1.5 rounded-lg bg-secondary text-foreground hover:bg-secondary/80 font-medium transition-colors border border-border"
                        >
                            {t('overview.applyRange')}
                        </button>
                    </div>
                )}
            </div>

            {/* Core Metrics Grid */}
            <div className={clsx("grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 transition-all duration-300", dataPulse && "ring-1 ring-primary/40 rounded-2xl")}>
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

                {/* 2. Requests & Rate / Success */}
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
                        <div className="flex items-baseline justify-between">
                            <div className="text-3xl font-extrabold tracking-tight font-mono text-foreground">
                                {telemetry.totalReqs.toLocaleString()}
                            </div>
                            {telemetry.tokensPerSec > 0 && (
                                <div className="text-right">
                                    <span className="text-[11px] font-mono font-semibold text-emerald-400 bg-emerald-500/10 px-2 py-0.5 rounded-full border border-emerald-500/20">
                                        ~{telemetry.tokensPerSec} {t('overview.tokensPerSecUnit')}
                                    </span>
                                </div>
                            )}
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
                    <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-4">
                        <div>
                            <h2 className="text-base font-bold text-foreground flex items-center gap-2">
                                <TrendingUp className="w-4 h-4 text-primary" />
                                {t('overview.tokenUsageTrend')}
                            </h2>
                            <p className="text-xs text-muted-foreground mt-0.5">
                                {t('overview.tokenUsageTrendDesc')}
                            </p>
                        </div>

                        {/* Controls: Chart View Mode + Legend */}
                        <div className="flex flex-wrap items-center gap-3 text-xs self-start sm:self-auto">
                            {/* Switch View Mode: Buckets vs Requests */}
                            <div className="flex items-center rounded-lg border border-border bg-secondary/70 p-0.5">
                                <button
                                    onClick={() => setChartViewMode('buckets')}
                                    className={clsx(
                                        "px-2.5 py-1 rounded text-[11px] font-medium transition-colors flex items-center gap-1",
                                        chartViewMode === 'buckets' ? "bg-card text-foreground shadow-xs font-semibold" : "text-muted-foreground hover:text-foreground"
                                    )}
                                >
                                    <BarChart3 className="w-3 h-3" />
                                    {t('overview.viewModeBuckets')}
                                </button>
                                <button
                                    onClick={() => setChartViewMode('requests')}
                                    className={clsx(
                                        "px-2.5 py-1 rounded text-[11px] font-medium transition-colors flex items-center gap-1",
                                        chartViewMode === 'requests' ? "bg-card text-foreground shadow-xs font-semibold" : "text-muted-foreground hover:text-foreground"
                                    )}
                                >
                                    <SlidersHorizontal className="w-3 h-3" />
                                    {t('overview.viewModeRequests')}
                                </button>
                            </div>

                            {/* Legend */}
                            <div className="flex items-center gap-2.5 text-muted-foreground">
                                <span className="flex items-center gap-1.5">
                                    <span className="w-2.5 h-2.5 rounded-xs bg-sky-500 shadow-xs" />
                                    Prompt
                                </span>
                                <span className="flex items-center gap-1.5">
                                    <span className="w-2.5 h-2.5 rounded-xs bg-emerald-500 shadow-xs" />
                                    Completion
                                </span>
                            </div>
                        </div>
                    </div>

                    {/* Chart area */}
                    {timelineBars.length === 0 ? (
                        <div className="h-48 flex flex-col items-center justify-center text-center p-6 rounded-xl border border-dashed border-border/60 bg-muted/10">
                            <TrendingUp className="w-8 h-8 text-muted-foreground/30 mb-2" />
                            <div className="text-sm font-medium text-foreground">{t('overview.noRequestsInRange')}</div>
                            <button
                                onClick={handleResetFilter}
                                className="mt-3 text-xs text-primary hover:underline font-semibold"
                            >
                                {t('overview.resetFilter')} &rarr;
                            </button>
                        </div>
                    ) : (
                        <div className="flex flex-col justify-between h-48 pt-2">
                            {/* Plot Area with background grid lines */}
                            <div className="h-36 relative flex items-end gap-1.5 sm:gap-2 px-1">
                                {/* Horizontal grid reference lines & Y-axis labels */}
                                <div className="absolute inset-0 flex flex-col justify-between pointer-events-none pb-0.5">
                                    <div className="border-b border-dashed border-border/40 w-full flex justify-end">
                                        <span className="text-[9px] font-mono text-muted-foreground/60 -mt-2.5 pr-0.5 select-none">
                                            {formatTokenCount(maxBucketTotal)}
                                        </span>
                                    </div>
                                    <div className="border-b border-dashed border-border/30 w-full flex justify-end">
                                        <span className="text-[9px] font-mono text-muted-foreground/50 -mt-2.5 pr-0.5 select-none">
                                            {formatTokenCount(Math.round(maxBucketTotal / 2))}
                                        </span>
                                    </div>
                                    <div className="border-b border-border/60 w-full flex justify-end">
                                        <span className="text-[9px] font-mono text-muted-foreground/40 -mt-2 pr-0.5 select-none">
                                            0
                                        </span>
                                    </div>
                                </div>

                                {/* Bars */}
                                {timelineBars.map((b, idx) => {
                                    const barHeightPercent = maxBucketTotal > 0 ? (b.total / maxBucketTotal) * 100 : 0
                                    const displayHeight = b.total > 0 ? Math.min(100, Math.max(barHeightPercent, 4)) : 0
                                    const isHovered = hoveredBucket === b.id

                                    // Position tooltip safely to prevent overflow on first/last bars
                                    const tooltipAlignClass = idx === 0
                                        ? 'left-0'
                                        : idx === timelineBars.length - 1
                                            ? 'right-0'
                                            : 'left-1/2 -translate-x-1/2'

                                    return (
                                        <div
                                            key={b.id}
                                            className="flex-1 max-w-[40px] h-full flex flex-col justify-end items-center relative group cursor-pointer z-10"
                                            onMouseEnter={() => setHoveredBucket(b.id)}
                                            onMouseLeave={() => setHoveredBucket(null)}
                                        >
                                            {/* Column hover guide track */}
                                            <div className="absolute inset-0 rounded-md bg-muted/20 opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none -z-10" />

                                            {/* Tooltip */}
                                            {isHovered && (
                                                <div className={`absolute bottom-full mb-2 ${tooltipAlignClass} z-30 px-3 py-2.5 rounded-xl bg-popover/95 backdrop-blur-md border border-border shadow-2xl text-[11px] whitespace-nowrap pointer-events-none transition-all duration-150`}>
                                                    <div className="flex items-center justify-between gap-3 pb-1.5 mb-1.5 border-b border-border/50">
                                                        <span className="font-semibold text-foreground font-mono">{b.fullTime}</span>
                                                        {b.count > 1 && (
                                                            <span className="text-[9px] px-1.5 py-0.5 rounded bg-primary/10 text-primary font-mono">
                                                                {b.count} reqs
                                                            </span>
                                                        )}
                                                        {b.model && (
                                                            <span className="text-[9px] px-1.5 py-0.5 rounded bg-secondary text-muted-foreground font-mono truncate max-w-[120px]">
                                                                {b.model}
                                                            </span>
                                                        )}
                                                    </div>
                                                    <div className="space-y-1">
                                                        <div className="flex items-center justify-between gap-4">
                                                            <span className="text-muted-foreground flex items-center gap-1.5">
                                                                <span className="w-2 h-2 rounded-full bg-sky-500" />
                                                                Prompt:
                                                            </span>
                                                            <span className="text-sky-400 font-mono font-medium">{b.prompt.toLocaleString()}</span>
                                                        </div>
                                                        <div className="flex items-center justify-between gap-4">
                                                            <span className="text-muted-foreground flex items-center gap-1.5">
                                                                <span className="w-2 h-2 rounded-full bg-emerald-500" />
                                                                Completion:
                                                            </span>
                                                            <span className="text-emerald-400 font-mono font-medium">{b.completion.toLocaleString()}</span>
                                                        </div>
                                                        {b.elapsedMs && (
                                                            <div className="flex items-center justify-between gap-4 text-muted-foreground text-[10px]">
                                                                <span className="flex items-center gap-1.5">
                                                                    <Clock className="w-2.5 h-2.5" />
                                                                    Latency:
                                                                </span>
                                                                <span className="font-mono">{b.elapsedMs}ms</span>
                                                            </div>
                                                        )}
                                                        <div className="flex items-center justify-between gap-4 pt-1.5 mt-1 border-t border-border/50 font-bold">
                                                            <span className="text-foreground">Total:</span>
                                                            <span className="text-foreground font-mono">{b.total.toLocaleString()}</span>
                                                        </div>
                                                    </div>
                                                </div>
                                            )}

                                            {/* Stacked Bar with flex distribution */}
                                            <div
                                                className="w-full max-w-[28px] flex flex-col justify-end rounded-t-md overflow-hidden transition-all duration-200 group-hover:brightness-110 shadow-xs"
                                                style={{ height: `${displayHeight}%` }}
                                            >
                                                {/* Completion tokens (top) */}
                                                {b.completion > 0 && (
                                                    <div
                                                        className="w-full bg-emerald-500 hover:bg-emerald-400 transition-colors"
                                                        style={{ flex: `${b.completion} 0 0%` }}
                                                    />
                                                )}
                                                {/* Prompt tokens (bottom) */}
                                                {b.prompt > 0 && (
                                                    <div
                                                        className="w-full bg-sky-500 hover:bg-sky-400 transition-colors"
                                                        style={{ flex: `${b.prompt} 0 0%` }}
                                                    />
                                                )}
                                            </div>
                                        </div>
                                    )
                                })}
                            </div>

                            {/* X-Axis Labels */}
                            <div className="flex gap-1.5 sm:gap-2 px-1 mt-2 border-t border-transparent">
                                {timelineBars.map((b) => (
                                    <div
                                        key={b.id}
                                        className="flex-1 max-w-[40px] text-[10px] text-muted-foreground text-center truncate font-mono select-none"
                                        title={b.fullTime}
                                    >
                                        {b.label}
                                    </div>
                                ))}
                            </div>
                        </div>
                    )}
                </div>

                {/* Model Distribution Panel */}
                <div className="p-6 rounded-2xl border border-border/80 bg-card/90 shadow-sm flex flex-col justify-between">
                    <div>
                        <h2 className="text-base font-bold text-foreground flex items-center gap-2">
                            <Layers className="w-4 h-4 text-purple-400" />
                            {t('overview.modelDistribution')}
                        </h2>
                        <p className="text-xs text-muted-foreground mt-0.5">
                            Tokens consumed by requested LLM model ({timeRange !== 'all' ? t(`overview.range${timeRange.charAt(0).toUpperCase() + timeRange.slice(1)}`) || timeRange : t('overview.allTime')})
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
                                                <span className="font-semibold text-foreground font-mono truncate max-w-[150px]">{m.name}</span>
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
                                    {t('overview.noRequestsInRange')}
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
                                                ? "bg-card text-foreground shadow-xs"
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
                                    const ts = parseItemTimestamp(item.created_at)
                                    const timeStr = ts
                                        ? new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
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
                        <span>Showing {recentStream.length} {timeRange !== 'all' ? `filtered` : `latest`} requests</span>
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
