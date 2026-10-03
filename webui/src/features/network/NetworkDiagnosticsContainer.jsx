import { useCallback, useEffect, useState } from 'react'
import {
    Activity,
    AlertCircle,
    CheckCircle2,
    Clock,
    Cpu,
    ExternalLink,
    Globe,
    Layers,
    Loader2,
    RefreshCw,
    Server,
    Shield,
    Wifi,
    XCircle,
} from 'lucide-react'
import clsx from 'clsx'

import { useI18n } from '../../i18n'

export default function NetworkDiagnosticsContainer({ authFetch, onMessage }) {
    const { t } = useI18n()
    const [loading, setLoading] = useState(false)
    const [report, setReport] = useState(null)
    const [clientInfo, setClientInfo] = useState({
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Unknown',
        userAgent: navigator.userAgent,
        screen: `${window.screen.width}x${window.screen.height}`,
        language: navigator.language,
    })
    const [lastUpdated, setLastUpdated] = useState(null)

    const fetchDiagnostics = useCallback(async () => {
        setLoading(true)
        const start = performance.now()
        try {
            const res = await authFetch('/admin/network-detect')
            if (!res.ok) {
                const text = await res.text()
                throw new Error(text || 'Failed to detect network')
            }
            const data = await res.json()
            setReport(data)
            setLastUpdated(new Date())
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setLoading(false)
        }
    }, [authFetch, onMessage])

    useEffect(() => {
        fetchDiagnostics()
    }, [fetchDiagnostics])

    const getStatusBadge = (status, code) => {
        const isSuccess = status === 'reachable'
        const isDegraded = status === 'limited'
        return (
            <span className={clsx(
                "inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border",
                isSuccess
                    ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/20"
                    : isDegraded
                        ? "bg-amber-500/10 text-amber-400 border-amber-500/20"
                        : "bg-rose-500/10 text-rose-400 border-rose-500/20"
            )}>
                {isSuccess ? (
                    <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
                ) : isDegraded ? (
                    <AlertCircle className="w-3.5 h-3.5 text-amber-400" />
                ) : (
                    <XCircle className="w-3.5 h-3.5 text-rose-400" />
                )}
                {isSuccess ? t('netdiag.status.online') : isDegraded ? t('netdiag.status.degraded') : t('netdiag.status.offline')}
                {code > 0 && <span className="font-mono text-[11px] opacity-75">({code})</span>}
            </span>
        )
    }

    return (
        <div className="space-y-6 max-w-7xl mx-auto pb-12 animate-in fade-in duration-300">
            {/* Header */}
            <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm">
                <div className="space-y-1">
                    <div className="flex items-center gap-3">
                        <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-cyan-500 to-blue-600 flex items-center justify-center text-white shadow-lg shadow-blue-500/20">
                            <Activity className="w-5 h-5" />
                        </div>
                        <div>
                            <h2 className="text-xl font-bold tracking-tight text-foreground">{t('netdiag.title')}</h2>
                            <p className="text-sm text-muted-foreground">{t('netdiag.subtitle')}</p>
                        </div>
                    </div>
                </div>

                <div className="flex items-center gap-3 w-full sm:w-auto justify-end">
                    {lastUpdated && (
                        <div className="text-xs text-muted-foreground flex items-center gap-1.5 hidden md:flex">
                            <Clock className="w-3.5 h-3.5" />
                            <span>{t('netdiag.lastCheck')}: {lastUpdated.toLocaleTimeString()}</span>
                        </div>
                    )}
                    <button
                        onClick={fetchDiagnostics}
                        disabled={loading}
                        className="inline-flex items-center gap-2 px-4 py-2 rounded-xl bg-primary text-primary-foreground font-medium text-sm hover:opacity-90 transition active:scale-95 disabled:opacity-50 shadow-md shadow-primary/20"
                    >
                        <RefreshCw className={clsx("w-4 h-4", loading && "animate-spin")} />
                        <span>{loading ? t('netdiag.probing') : t('netdiag.refresh')}</span>
                    </button>
                </div>
            </div>

            {loading && !report ? (
                <div className="min-h-[400px] rounded-2xl border border-border/80 bg-card/60 flex flex-col items-center justify-center gap-3 text-muted-foreground">
                    <Loader2 className="w-8 h-8 animate-spin text-primary" />
                    <p className="text-sm animate-pulse">{t('netdiag.probingDetail')}</p>
                </div>
            ) : report ? (
                <>
                    {/* Top Grid: IP & DNS */}
                    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                        {/* IPv4 Card */}
                        <div className="p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm relative overflow-hidden group hover:border-primary/40 transition">
                            <div className="flex items-center justify-between mb-4">
                                <span className="text-xs font-semibold tracking-wider uppercase text-muted-foreground flex items-center gap-2">
                                    <Globe className="w-4 h-4 text-cyan-400" />
                                    {t('netdiag.ipv4')}
                                </span>
                                <span className={clsx(
                                    "w-2.5 h-2.5 rounded-full",
                                    report.ipv4?.ip ? "bg-emerald-400 shadow-lg shadow-emerald-400/50" : "bg-muted-foreground/30"
                                )} />
                            </div>
                            <div className="text-2xl font-mono font-bold tracking-tight text-foreground truncate">
                                {report.ipv4?.ip || t('netdiag.unreachable')}
                            </div>
                            <div className="mt-4 pt-4 border-t border-border/60 space-y-2 text-xs">
                                <div className="flex justify-between text-muted-foreground">
                                    <span>{t('netdiag.latency')}:</span>
                                    <span className="font-mono text-foreground font-medium">{report.ipv4?.latency_ms ? `${report.ipv4.latency_ms} ms` : '—'}</span>
                                </div>
                                <div className="flex justify-between text-muted-foreground">
                                    <span>{t('netdiag.source')}:</span>
                                    <span className="font-mono text-foreground">{report.ipv4?.provider || '—'}</span>
                                </div>
                            </div>
                        </div>

                        {/* IPv6 Card */}
                        <div className="p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm relative overflow-hidden group hover:border-primary/40 transition">
                            <div className="flex items-center justify-between mb-4">
                                <span className="text-xs font-semibold tracking-wider uppercase text-muted-foreground flex items-center gap-2">
                                    <Wifi className="w-4 h-4 text-purple-400" />
                                    {t('netdiag.ipv6')}
                                </span>
                                <span className={clsx(
                                    "w-2.5 h-2.5 rounded-full",
                                    report.ipv6?.ip ? "bg-emerald-400 shadow-lg shadow-emerald-400/50" : "bg-amber-400/50"
                                )} />
                            </div>
                            <div className="text-lg font-mono font-bold tracking-tight text-foreground break-all line-clamp-1">
                                {report.ipv6?.ip || t('netdiag.unreachable')}
                            </div>
                            <div className="mt-4 pt-4 border-t border-border/60 space-y-2 text-xs">
                                <div className="flex justify-between text-muted-foreground">
                                    <span>{t('netdiag.latency')}:</span>
                                    <span className="font-mono text-foreground font-medium">{report.ipv6?.latency_ms ? `${report.ipv6.latency_ms} ms` : '—'}</span>
                                </div>
                                <div className="flex justify-between text-muted-foreground">
                                    <span>{t('netdiag.source')}:</span>
                                    <span className="font-mono text-foreground">{report.ipv6?.provider || '—'}</span>
                                </div>
                            </div>
                        </div>

                        {/* Cloudflare Datacenter & Location Card */}
                        <div className="p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm relative overflow-hidden group hover:border-primary/40 transition">
                            <div className="flex items-center justify-between mb-4">
                                <span className="text-xs font-semibold tracking-wider uppercase text-muted-foreground flex items-center gap-2">
                                    <Server className="w-4 h-4 text-emerald-400" />
                                    {t('netdiag.routing')}
                                </span>
                                {report.ipv4?.colo && (
                                    <span className="font-mono text-xs px-2 py-0.5 rounded bg-primary/10 text-primary font-bold">
                                        {report.ipv4.colo}
                                    </span>
                                )}
                            </div>
                            <div className="space-y-2 text-sm">
                                <div className="flex justify-between">
                                    <span className="text-muted-foreground">{t('netdiag.country')}:</span>
                                    <span className="font-medium text-foreground">{report.ipv4?.country || '—'}</span>
                                </div>
                                <div className="flex justify-between">
                                    <span className="text-muted-foreground">{t('netdiag.gatewayOS')}:</span>
                                    <span className="font-mono text-foreground uppercase">{report.os || 'server'}</span>
                                </div>
                                <div className="flex justify-between">
                                    <span className="text-muted-foreground">{t('netdiag.proxyUsed')}:</span>
                                    <span className="font-mono text-foreground">{report.proxy_used ? t('netdiag.yes') : t('netdiag.no')}</span>
                                </div>
                            </div>
                        </div>
                    </div>

                    {/* Platforms Reachability Matrix */}
                    <div className="p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm space-y-5">
                        <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2.5">
                                <Layers className="w-5 h-5 text-primary" />
                                <h3 className="font-bold text-base text-foreground">{t('netdiag.platforms.matrixTitle')}</h3>
                            </div>
                            <span className="text-xs text-muted-foreground">{t('netdiag.platforms.matrixDesc')}</span>
                        </div>

                        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                            {Array.isArray(report.platforms) && report.platforms.map((item) => (
                                <div
                                    key={item.platform}
                                    className="p-4 rounded-xl border border-border/60 bg-background/50 hover:bg-background/80 transition flex flex-col justify-between gap-3"
                                >
                                    <div className="flex items-start justify-between gap-2">
                                        <div>
                                            <div className="font-semibold text-sm capitalize text-foreground">{item.platform}</div>
                                            <div className="text-[11px] text-muted-foreground truncate max-w-[180px] font-mono mt-0.5">
                                                {item.url}
                                            </div>
                                        </div>
                                        {getStatusBadge(item.status, item.http_status)}
                                    </div>

                                    <div className="pt-2 border-t border-border/40 flex items-center justify-between text-xs">
                                        <span className="text-muted-foreground">{t('netdiag.latency')}</span>
                                        <span className={clsx(
                                            "font-mono font-medium",
                                            item.elapsed_ms < 300 ? "text-emerald-400" : item.elapsed_ms < 1000 ? "text-amber-400" : "text-rose-400"
                                        )}>
                                            {item.elapsed_ms ? `${item.elapsed_ms} ms` : '—'}
                                        </span>
                                    </div>
                                    {item.error && (
                                        <div className="text-[11px] text-destructive bg-destructive/10 p-2 rounded border border-destructive/20 break-words font-mono">
                                            {item.error}
                                        </div>
                                    )}
                                </div>
                            ))}
                        </div>
                    </div>

                    {/* Bottom Split: Server DNS & Client Context */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                        {/* Server DNS Card */}
                        <div className="p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm space-y-4">
                            <div className="flex items-center gap-2.5">
                                <Cpu className="w-5 h-5 text-indigo-400" />
                                <h3 className="font-bold text-base text-foreground">{t('netdiag.dns.title')}</h3>
                            </div>
                            <div className="space-y-2">
                                {Array.isArray(report.dns) && report.dns.length > 0 ? (
                                    <div className="space-y-1.5">
                                        {report.dns.map((srv, idx) => (
                                            <div key={idx} className="flex items-center justify-between px-3 py-2 rounded-lg bg-background/50 border border-border/40 font-mono text-xs">
                                                <span className="text-foreground">{srv}</span>
                                                <span className="text-muted-foreground text-[10px] uppercase">Resolver #{idx + 1}</span>
                                            </div>
                                        ))}
                                    </div>
                                ) : (
                                    <div className="text-xs text-muted-foreground italic py-3 text-center">
                                        {t('netdiag.dns.noServers')}
                                    </div>
                                )}
                            </div>
                        </div>

                        {/* Client Browser Context Card */}
                        <div className="p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm space-y-4">
                            <div className="flex items-center gap-2.5">
                                <Shield className="w-5 h-5 text-emerald-400" />
                                <h3 className="font-bold text-base text-foreground">{t('netdiag.client.title')}</h3>
                            </div>
                            <div className="space-y-2 text-xs">
                                <div className="flex justify-between py-1.5 border-b border-border/40">
                                    <span className="text-muted-foreground">{t('netdiag.client.timezone')}:</span>
                                    <span className="font-mono text-foreground font-medium">{clientInfo.timezone}</span>
                                </div>
                                <div className="flex justify-between py-1.5 border-b border-border/40">
                                    <span className="text-muted-foreground">{t('netdiag.client.screen')}:</span>
                                    <span className="font-mono text-foreground">{clientInfo.screen}</span>
                                </div>
                                <div className="flex justify-between py-1.5 border-b border-border/40">
                                    <span className="text-muted-foreground">{t('netdiag.client.language')}:</span>
                                    <span className="font-mono text-foreground uppercase">{clientInfo.language}</span>
                                </div>
                                <div className="pt-1">
                                    <span className="text-muted-foreground block mb-1">{t('netdiag.client.ua')}:</span>
                                    <p className="font-mono text-[11px] text-muted-foreground bg-background/50 p-2 rounded border border-border/40 break-all">
                                        {clientInfo.userAgent}
                                    </p>
                                </div>
                            </div>
                        </div>
                    </div>
                </>
            ) : null}
        </div>
    )
}
