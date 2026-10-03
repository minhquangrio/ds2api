import { useState } from 'react'
import {
    Check,
    Copy,
    Pencil,
    Play,
    Trash2,
    FolderX,
    Gauge,
    Bot,
    Sparkles,
    AlertCircle,
    XCircle,
    ShieldAlert,
    Clock,
    Globe,
    FileText,
    Wrench,
    Layers,
} from 'lucide-react'
import clsx from 'clsx'

export default function AccountItemRow({
    acc,
    t,
    resolveAccountIdentifier,
    proxies = [],
    sessionCounts,
    deletingSessions,
    updatingProxy,
    togglingEnabled,
    testing,
    elasticPoolEnabled,
    envBacked,
    onToggleAccountEnabled,
    onUpdateAccountProxy,
    onEditAccount,
    onTestAccount,
    onViewQuota,
    onDeleteAccount,
    onDeleteAllSessions,
}) {
    const [copiedId, setCopiedId] = useState(false)
    const id = resolveAccountIdentifier(acc)
    const isGemini = acc.provider === 'gemini'

    const assignedProxy = proxies.find(proxy => proxy.id === acc.proxy_id)
    const runtimeUnknown = envBacked && !acc.test_status
    const isDisabled = acc.enabled === false
    const isBanned = acc.banned === true
    const isMuted = acc.muted === true
    const mutedRecoverAt = formatMuteUntil(acc.muted_until)
    const isTestFailed = acc.test_status === 'failed'
    const isActive = !isDisabled && !isBanned && !isMuted && !isTestFailed && (acc.test_status === 'ok' || acc.has_token)

    const rawName = String(acc.name || '').trim()
    const rawRemark = String(acc.remark || '').trim()
    const isCustomName = rawName !== '' && rawName.toLowerCase() !== id.toLowerCase()
    const isCustomRemark = rawRemark !== '' && rawRemark.toLowerCase() !== id.toLowerCase() && rawRemark.toLowerCase() !== rawName.toLowerCase()

    const copyId = () => {
        if (!id) return
        navigator.clipboard.writeText(id).then(() => {
            setCopiedId(true)
            setTimeout(() => setCopiedId(false), 1500)
        })
    }

    return (
        <div
            className={clsx(
                "p-4 sm:p-5 flex flex-col xl:flex-row xl:items-center justify-between gap-4 border-b border-border/70 transition-all duration-150",
                (isDisabled || isBanned || isMuted) ? "bg-muted/15 opacity-70" : "hover:bg-muted/25",
                isActive && "border-l-2 border-l-emerald-500/60"
            )}
        >
            {/* Left Block: Avatar, Identity & Metadata Badges */}
            <div className="flex items-start gap-3.5 min-w-0 flex-1">
                {/* Provider Avatar with Status Dot */}
                <div className="relative shrink-0 mt-0.5">
                    <div
                        className={clsx(
                            "w-10 h-10 rounded-xl flex items-center justify-center border shadow-xs transition-colors",
                            isGemini
                                ? "bg-blue-500/10 text-blue-400 border-blue-500/25"
                                : "bg-primary/10 text-primary border-primary/25"
                        )}
                        title={isGemini ? 'Google Gemini' : 'DeepSeek'}
                    >
                        {isGemini ? <Sparkles className="w-5 h-5" /> : <Bot className="w-5 h-5" />}
                    </div>
                    {/* Status Dot */}
                    <span
                        className={clsx(
                            "absolute -top-0.5 -right-0.5 w-3 h-3 rounded-full border-2 border-card shadow-xs",
                            isDisabled ? "bg-slate-400" :
                            isBanned ? "bg-rose-500 ring-2 ring-rose-500/20" :
                            isMuted ? "bg-orange-500 ring-2 ring-orange-500/20" :
                            isTestFailed ? "bg-rose-500 ring-2 ring-rose-500/20" :
                            isActive ? "bg-emerald-500 ring-2 ring-emerald-500/20" :
                            runtimeUnknown ? "bg-blue-400" : "bg-amber-400"
                        )}
                    />
                </div>

                {/* Identity & Badges Container */}
                <div className="min-w-0 flex-1">
                    {/* Primary Title Line */}
                    <div className="flex flex-wrap items-center gap-2">
                        {isCustomName ? (
                            <span className="text-sm font-semibold text-foreground truncate max-w-xs sm:max-w-md">
                                {rawName}
                            </span>
                        ) : (
                            <div
                                onClick={copyId}
                                className="group flex items-center gap-1.5 cursor-pointer"
                                title={t('accountManager.copyIdentifier')}
                            >
                                <span className="text-sm font-semibold font-mono text-foreground hover:text-primary transition-colors truncate max-w-xs sm:max-w-md">
                                    {id || '-'}
                                </span>
                                {copiedId ? (
                                    <Check className="w-3.5 h-3.5 text-emerald-500 shrink-0" />
                                ) : (
                                    <Copy className="w-3.5 h-3.5 opacity-0 group-hover:opacity-60 transition-opacity shrink-0 text-muted-foreground" />
                                )}
                            </div>
                        )}

                        {/* Provider Pill */}
                        {isGemini ? (
                            <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-indigo-500/15 text-indigo-400 border border-indigo-500/25">
                                Gemini
                            </span>
                        ) : (
                            <span className="px-2 py-0.5 rounded text-[10px] font-semibold bg-sky-500/15 text-sky-400 border border-sky-500/25">
                                DeepSeek
                            </span>
                        )}

                        {/* Status Pill */}
                        {isBanned ? (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-rose-500/15 text-rose-400 border border-rose-500/25">
                                <ShieldAlert className="w-3 h-3" />
                                <span>{acc.disabled_reason || t('accountManager.accountBanned')}</span>
                            </span>
                        ) : isDisabled ? (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-muted text-muted-foreground border border-border">
                                <span className="w-1.5 h-1.5 rounded-full bg-muted-foreground/60" />
                                <span>{t('accountManager.accountDisabled')}</span>
                            </span>
                        ) : isMuted ? (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-orange-500/15 text-orange-400 border border-orange-500/25">
                                <Clock className="w-3 h-3" />
                                <span>{t('accountManager.accountMutedRecoverAt', { time: mutedRecoverAt })}</span>
                            </span>
                        ) : isTestFailed ? (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-rose-500/15 text-rose-400 border border-rose-500/25">
                                <XCircle className="w-3 h-3" />
                                <span>{t('accountManager.testStatusFailed')}</span>
                            </span>
                        ) : isActive ? (
                            <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[11px] font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/25">
                                <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
                                <span>{t('accountManager.sessionActive')}</span>
                            </span>
                        ) : runtimeUnknown ? (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-blue-500/15 text-blue-400 border border-blue-500/25">
                                <span>{t('accountManager.runtimeStatusUnknown')}</span>
                            </span>
                        ) : (
                            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-amber-500/15 text-amber-400 border border-amber-500/25">
                                <AlertCircle className="w-3 h-3" />
                                <span>{t('accountManager.reauthRequired')}</span>
                            </span>
                        )}
                    </div>

                    {/* Secondary Line: Identifier with Copy (Only shown if custom name is present) */}
                    {isCustomName && (
                        <div className="flex items-center gap-1.5 mt-0.5 text-xs text-muted-foreground font-mono">
                            <span
                                onClick={copyId}
                                className="truncate hover:text-primary cursor-pointer transition-colors"
                                title={t('accountManager.copyIdentifier')}
                            >
                                {id}
                            </span>
                            <button
                                type="button"
                                onClick={copyId}
                                className="p-0.5 text-muted-foreground hover:text-foreground transition-colors"
                                title={t('accountManager.copyIdentifier')}
                            >
                                {copiedId ? (
                                    <Check className="w-3 h-3 text-emerald-500" />
                                ) : (
                                    <Copy className="w-3 h-3" />
                                )}
                            </button>
                        </div>
                    )}

                    {/* Remark Line (if custom remark exists) */}
                    {isCustomRemark && (
                        <div className="flex items-center gap-1.5 text-xs text-muted-foreground/90 mt-0.5">
                            <FileText className="w-3 h-3 shrink-0 opacity-60" />
                            <span className="truncate">{rawRemark}</span>
                        </div>
                    )}

                    {/* Metadata Badges Strip */}
                    <div className="flex flex-wrap items-center gap-1.5 mt-2">
                        {/* Pool Type Badge */}
                        {acc.pool_type === 'no_tools' ? (
                            <span className="inline-flex items-center gap-1 font-mono text-[10px] bg-blue-500/10 text-blue-400 border border-blue-500/20 px-1.5 py-0.5 rounded">
                                <Layers className="w-2.5 h-2.5" />
                                <span>{t('accountManager.poolBadgeNoTools')}</span>
                            </span>
                        ) : acc.pool_type === 'tools_only' ? (
                            <span className="inline-flex items-center gap-1 font-mono text-[10px] bg-purple-500/10 text-purple-400 border border-purple-500/20 px-1.5 py-0.5 rounded">
                                <Wrench className="w-2.5 h-2.5" />
                                <span>{t('accountManager.poolBadgeToolsOnly')}</span>
                            </span>
                        ) : (
                            <span className="font-mono text-[10px] bg-muted/60 text-muted-foreground border border-border/70 px-1.5 py-0.5 rounded">
                                {t('accountManager.filterPoolTypeDefault')}
                            </span>
                        )}

                        {/* Auth Mode Badge (DeepSeek) */}
                        {!isGemini && (
                            <span className={clsx(
                                "font-mono text-[10px] px-1.5 py-0.5 rounded border",
                                (acc.has_cookies || !acc.has_password)
                                    ? "bg-emerald-500/10 text-emerald-400 border-emerald-500/20"
                                    : "bg-slate-500/10 text-slate-400 border-slate-500/20"
                            )}>
                                {(acc.has_cookies || !acc.has_password) ? t('accountManager.cookieAuthBadge') : t('accountManager.passwordAuthBadge')}
                            </span>
                        )}

                        {/* Masked Secret Previews */}
                        {acc.token_preview && (
                            <span className="font-mono text-[10px] bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded border border-border/70">
                                {acc.token_preview}
                            </span>
                        )}
                        {acc.cookies_preview && (
                            <span className="font-mono text-[10px] bg-muted/80 text-muted-foreground px-1.5 py-0.5 rounded border border-border/70" title="Cookies">
                                {acc.cookies_preview}
                            </span>
                        )}

                        {/* Proxy Badge */}
                        {acc.proxy_id ? (
                            <span className="inline-flex items-center gap-1 font-mono text-[10px] bg-amber-500/10 text-amber-400 border border-amber-500/25 px-1.5 py-0.5 rounded">
                                <Globe className="w-2.5 h-2.5" />
                                <span>{assignedProxy ? (assignedProxy.name || `${assignedProxy.host}:${assignedProxy.port}`) : acc.proxy_id}</span>
                            </span>
                        ) : (
                            <span className="font-mono text-[10px] text-muted-foreground/60 px-1 py-0.5">
                                {t('accountManager.proxyNone')}
                            </span>
                        )}

                        {/* Session Count & Clear */}
                        {sessionCounts && sessionCounts[id] !== undefined && (
                            <span className="inline-flex items-center gap-1 font-mono text-[10px] bg-blue-500/10 text-blue-400 border border-blue-500/20 px-1.5 py-0.5 rounded">
                                <span>{t('accountManager.sessionCount', { count: sessionCounts[id] })}</span>
                                {sessionCounts[id] > 0 && (
                                    <button
                                        type="button"
                                        onClick={() => onDeleteAllSessions(id)}
                                        disabled={deletingSessions && deletingSessions[id]}
                                        className="hover:text-destructive transition-colors ml-0.5"
                                        title={t('accountManager.deleteAllSessions')}
                                    >
                                        {deletingSessions && deletingSessions[id] ? (
                                            <span className="animate-spin text-[9px]">⟳</span>
                                        ) : (
                                            <FolderX className="w-2.5 h-2.5" />
                                        )}
                                    </button>
                                )}
                            </span>
                        )}
                    </div>
                </div>
            </div>

            {/* Right Block: Action Controls Cluster */}
            <div className="flex flex-wrap items-center gap-2 self-start xl:self-center ml-13 xl:ml-0 shrink-0">
                {/* Enable / Disable Switch */}
                <button
                    type="button"
                    role="switch"
                    aria-checked={!isDisabled}
                    onClick={() => onToggleAccountEnabled(id, isDisabled)}
                    disabled={elasticPoolEnabled || togglingEnabled?.[id]}
                    title={isDisabled ? t('accountManager.enableAccount') : t('accountManager.disableAccount')}
                    className={clsx(
                        "relative inline-flex h-5.5 w-10 items-center rounded-full transition-colors disabled:opacity-50 disabled:cursor-not-allowed shrink-0",
                        isDisabled ? "bg-muted-foreground/30" : "bg-primary"
                    )}
                >
                    <span
                        className={clsx(
                            "inline-block h-4 w-4 transform rounded-full bg-white transition-transform shadow-xs",
                            isDisabled ? "translate-x-1" : "translate-x-[20px]"
                        )}
                    />
                </button>

                {/* Proxy Dropdown */}
                <select
                    value={acc.proxy_id || ''}
                    onChange={e => onUpdateAccountProxy(id, e.target.value)}
                    disabled={updatingProxy?.[id]}
                    className="max-w-[140px] sm:max-w-[170px] px-2 py-1 text-xs bg-muted/70 border border-border rounded-lg text-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50 truncate"
                >
                    <option value="">{t('accountManager.proxyNone')}</option>
                    {proxies.map(proxy => (
                        <option key={proxy.id} value={proxy.id}>
                            {proxy.name || `${proxy.host}:${proxy.port}`}
                        </option>
                    ))}
                </select>

                {/* Refresh / Test Token */}
                <button
                    type="button"
                    onClick={() => onTestAccount(id)}
                    disabled={testing[id]}
                    className="flex items-center gap-1 px-2.5 py-1 text-xs font-medium border border-border rounded-lg hover:bg-secondary transition-colors disabled:opacity-50"
                    title={t('accountManager.refreshSession')}
                >
                    {testing[id] ? (
                        <span className="animate-spin text-xs">⟳</span>
                    ) : (
                        <Play className="w-3 h-3 text-emerald-400" />
                    )}
                    <span className="hidden sm:inline">{testing[id] ? t('actions.testing') : t('accountManager.refreshSession')}</span>
                </button>

                {/* Quota Button (Gemini only) */}
                {isGemini && (
                    <button
                        type="button"
                        onClick={() => onViewQuota && onViewQuota(acc)}
                        className="flex items-center gap-1 px-2 py-1 text-xs font-medium border border-purple-500/30 bg-purple-500/10 text-purple-300 hover:bg-purple-500/20 rounded-lg transition-colors shadow-xs"
                        title={t('accountManager.quota.button')}
                    >
                        <Gauge className="w-3 h-3 text-purple-400" />
                        <span className="hidden sm:inline">{t('accountManager.quota.button')}</span>
                    </button>
                )}

                {/* Edit Button */}
                <button
                    type="button"
                    onClick={() => onEditAccount(acc)}
                    disabled={!id}
                    className="p-1.5 text-muted-foreground hover:text-primary hover:bg-primary/10 rounded-lg transition-colors disabled:opacity-40"
                    title={id ? t('accountManager.editAccountTitle') : t('accountManager.invalidIdentifier')}
                >
                    <Pencil className="w-3.5 h-3.5" />
                </button>

                {/* Delete Button */}
                <button
                    type="button"
                    onClick={() => onDeleteAccount(id)}
                    className="p-1.5 text-muted-foreground hover:text-destructive hover:bg-destructive/10 rounded-lg transition-colors"
                    title={t('accountManager.deleteAccount')}
                >
                    <Trash2 className="w-3.5 h-3.5" />
                </button>
            </div>
        </div>
    )
}

function formatMuteUntil(muteUntil) {
    if (!muteUntil || muteUntil <= 0) return '--'
    const d = new Date(muteUntil * 1000)
    const month = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    const hour = String(d.getHours()).padStart(2, '0')
    const min = String(d.getMinutes()).padStart(2, '0')
    return `${day}/${month} ${hour}:${min}`
}
