import { Search, X, RotateCcw, Bot, Sparkles, Layers, CheckCircle2, AlertTriangle, Globe, Zap } from 'lucide-react'
import clsx from 'clsx'

export default function AccountFiltersBar({
    t,
    searchQuery,
    onSearchChange,
    filterProvider,
    onFilterProviderChange,
    filterPoolType,
    onFilterPoolTypeChange,
    filterStatus,
    onFilterStatusChange,
    filterProxy,
    onFilterProxyChange,
    onResetFilters,
    accountStats,
    totalAccounts,
    proxies = [],
}) {
    const total = accountStats?.total ?? totalAccounts ?? 0
    const active = accountStats?.active ?? 0
    const deepseek = accountStats?.deepseek ?? 0
    const gemini = accountStats?.gemini ?? 0
    const issues = accountStats?.issues ?? 0

    const isFiltered = (
        (filterProvider && filterProvider !== 'all') ||
        (filterPoolType && filterPoolType !== 'all') ||
        (filterStatus && filterStatus !== 'all') ||
        (filterProxy && filterProxy !== 'all') ||
        Boolean(searchQuery && searchQuery.trim())
    )

    return (
        <div className="border-b border-border bg-card/60 backdrop-blur-sm">
            {/* Quick Stat Strip / Interactive Filter Cards */}
            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-2 p-4 border-b border-border/60">
                <button
                    type="button"
                    onClick={() => {
                        onFilterProviderChange('all')
                        onFilterStatusChange('all')
                    }}
                    className={clsx(
                        "flex items-center gap-3 p-2.5 rounded-lg border text-left transition-all",
                        filterProvider === 'all' && filterStatus === 'all'
                            ? "bg-primary/10 border-primary/40 ring-1 ring-primary/30"
                            : "bg-muted/30 border-border/70 hover:bg-muted/60"
                    )}
                >
                    <div className="w-8 h-8 rounded-lg bg-primary/15 text-primary flex items-center justify-center shrink-0">
                        <Layers className="w-4 h-4" />
                    </div>
                    <div className="min-w-0">
                        <div className="text-[11px] font-medium text-muted-foreground truncate">{t('accountManager.statTotal')}</div>
                        <div className="text-base font-bold text-foreground leading-tight">{total}</div>
                    </div>
                </button>

                <button
                    type="button"
                    onClick={() => onFilterStatusChange(filterStatus === 'active' ? 'all' : 'active')}
                    className={clsx(
                        "flex items-center gap-3 p-2.5 rounded-lg border text-left transition-all",
                        filterStatus === 'active'
                            ? "bg-emerald-500/10 border-emerald-500/40 ring-1 ring-emerald-500/30"
                            : "bg-muted/30 border-border/70 hover:bg-muted/60"
                    )}
                >
                    <div className="w-8 h-8 rounded-lg bg-emerald-500/15 text-emerald-500 flex items-center justify-center shrink-0">
                        <CheckCircle2 className="w-4 h-4" />
                    </div>
                    <div className="min-w-0">
                        <div className="text-[11px] font-medium text-muted-foreground truncate">{t('accountManager.statActive')}</div>
                        <div className="text-base font-bold text-emerald-500 leading-tight">{active}</div>
                    </div>
                </button>

                <button
                    type="button"
                    onClick={() => onFilterProviderChange(filterProvider === 'deepseek' ? 'all' : 'deepseek')}
                    className={clsx(
                        "flex items-center gap-3 p-2.5 rounded-lg border text-left transition-all",
                        filterProvider === 'deepseek'
                            ? "bg-primary/10 border-primary/40 ring-1 ring-primary/30"
                            : "bg-muted/30 border-border/70 hover:bg-muted/60"
                    )}
                >
                    <div className="w-8 h-8 rounded-lg bg-primary/15 text-primary flex items-center justify-center shrink-0">
                        <Bot className="w-4 h-4" />
                    </div>
                    <div className="min-w-0">
                        <div className="text-[11px] font-medium text-muted-foreground truncate">{t('accountManager.statDeepSeek')}</div>
                        <div className="text-base font-bold text-primary leading-tight">{deepseek}</div>
                    </div>
                </button>

                <button
                    type="button"
                    onClick={() => onFilterProviderChange(filterProvider === 'gemini' ? 'all' : 'gemini')}
                    className={clsx(
                        "flex items-center gap-3 p-2.5 rounded-lg border text-left transition-all",
                        filterProvider === 'gemini'
                            ? "bg-blue-500/10 border-blue-500/40 ring-1 ring-blue-500/30"
                            : "bg-muted/30 border-border/70 hover:bg-muted/60"
                    )}
                >
                    <div className="w-8 h-8 rounded-lg bg-blue-500/15 text-blue-400 flex items-center justify-center shrink-0">
                        <Sparkles className="w-4 h-4" />
                    </div>
                    <div className="min-w-0">
                        <div className="text-[11px] font-medium text-muted-foreground truncate">{t('accountManager.statGemini')}</div>
                        <div className="text-base font-bold text-blue-400 leading-tight">{gemini}</div>
                    </div>
                </button>

                <button
                    type="button"
                    onClick={() => onFilterStatusChange(filterStatus === 'issues' ? 'all' : 'issues')}
                    className={clsx(
                        "col-span-2 sm:col-span-1 flex items-center gap-3 p-2.5 rounded-lg border text-left transition-all",
                        filterStatus === 'issues'
                            ? "bg-amber-500/10 border-amber-500/40 ring-1 ring-amber-500/30"
                            : "bg-muted/30 border-border/70 hover:bg-muted/60"
                    )}
                >
                    <div className="w-8 h-8 rounded-lg bg-amber-500/15 text-amber-500 flex items-center justify-center shrink-0">
                        <AlertTriangle className="w-4 h-4" />
                    </div>
                    <div className="min-w-0">
                        <div className="text-[11px] font-medium text-muted-foreground truncate">{t('accountManager.statIssues')}</div>
                        <div className="text-base font-bold text-amber-500 leading-tight">{issues}</div>
                    </div>
                </button>
            </div>

            {/* Filter Toolbar Controls */}
            <div className="p-4 flex flex-wrap items-center justify-between gap-3">
                <div className="flex flex-wrap items-center gap-2.5">
                    {/* Provider Group Pills */}
                    <div className="flex items-center bg-muted/60 p-1 rounded-lg border border-border/70 text-xs">
                        <button
                            type="button"
                            onClick={() => onFilterProviderChange('all')}
                            className={clsx(
                                "px-2.5 py-1 rounded-md font-medium transition-colors",
                                filterProvider === 'all'
                                    ? "bg-card text-foreground shadow-xs"
                                    : "text-muted-foreground hover:text-foreground"
                            )}
                        >
                            {t('accountManager.filterProviderAll')}
                        </button>
                        <button
                            type="button"
                            onClick={() => onFilterProviderChange('deepseek')}
                            className={clsx(
                                "flex items-center gap-1 px-2.5 py-1 rounded-md font-medium transition-colors",
                                filterProvider === 'deepseek'
                                    ? "bg-primary/15 text-primary shadow-xs"
                                    : "text-muted-foreground hover:text-foreground"
                            )}
                        >
                            <Bot className="w-3 h-3" />
                            <span>DeepSeek</span>
                        </button>
                        <button
                            type="button"
                            onClick={() => onFilterProviderChange('gemini')}
                            className={clsx(
                                "flex items-center gap-1 px-2.5 py-1 rounded-md font-medium transition-colors",
                                filterProvider === 'gemini'
                                    ? "bg-blue-500/15 text-blue-400 shadow-xs"
                                    : "text-muted-foreground hover:text-foreground"
                            )}
                        >
                            <Sparkles className="w-3 h-3" />
                            <span>Gemini</span>
                        </button>
                        <button
                            type="button"
                            onClick={() => onFilterProviderChange('codex')}
                            className={clsx(
                                "flex items-center gap-1 px-2.5 py-1 rounded-md font-medium transition-colors",
                                filterProvider === 'codex'
                                    ? "bg-emerald-500/15 text-emerald-400 shadow-xs"
                                    : "text-muted-foreground hover:text-foreground"
                            )}
                        >
                            <Zap className="w-3 h-3" />
                            <span>Codex</span>
                        </button>
                    </div>

                    {/* Pool Type Filter */}
                    <select
                        value={filterPoolType}
                        onChange={e => onFilterPoolTypeChange(e.target.value)}
                        className="px-2.5 py-1.5 text-xs bg-muted/60 border border-border rounded-lg text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
                    >
                        <option value="all">{t('accountManager.filterPoolTypeAll')}</option>
                        <option value="default">{t('accountManager.filterPoolTypeDefault')}</option>
                        <option value="no_tools">{t('accountManager.filterPoolTypeNoTools')}</option>
                        <option value="tools_only">{t('accountManager.filterPoolTypeToolsOnly')}</option>
                    </select>

                    {/* Status Filter */}
                    <select
                        value={filterStatus}
                        onChange={e => onFilterStatusChange(e.target.value)}
                        className="px-2.5 py-1.5 text-xs bg-muted/60 border border-border rounded-lg text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
                    >
                        <option value="all">{t('accountManager.filterStatusAll')}</option>
                        <option value="active">{t('accountManager.filterStatusActive')}</option>
                        <option value="disabled">{t('accountManager.filterStatusDisabled')}</option>
                        <option value="issues">{t('accountManager.filterStatusIssues')}</option>
                        <option value="failed">{t('accountManager.filterStatusFailed')}</option>
                        <option value="banned">{t('accountManager.filterStatusBanned')}</option>
                        <option value="muted">{t('accountManager.filterStatusMuted')}</option>
                    </select>

                    {/* Proxy Filter */}
                    {proxies.length > 0 && (
                        <div className="relative inline-flex items-center">
                            <select
                                value={filterProxy}
                                onChange={e => onFilterProxyChange(e.target.value)}
                                className="px-2.5 py-1.5 text-xs bg-muted/60 border border-border rounded-lg text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
                            >
                                <option value="all">{t('accountManager.filterProxyAll')}</option>
                                <option value="direct">{t('accountManager.filterProxyDirect')}</option>
                                {proxies.map(p => (
                                    <option key={p.id} value={p.id}>
                                        {p.name || `${p.host}:${p.port}`}
                                    </option>
                                ))}
                            </select>
                        </div>
                    )}

                    {/* Reset Button */}
                    {isFiltered && (
                        <button
                            type="button"
                            onClick={onResetFilters}
                            className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium text-destructive hover:bg-destructive/10 border border-destructive/20 rounded-lg transition-colors"
                        >
                            <RotateCcw className="w-3 h-3" />
                            <span>{t('accountManager.filterReset')}</span>
                        </button>
                    )}
                </div>

                {/* Search Box with Clear Button */}
                <div className="relative min-w-[240px] sm:w-72">
                    <Search className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
                    <input
                        type="text"
                        value={searchQuery}
                        onChange={e => onSearchChange(e.target.value)}
                        placeholder={t('accountManager.searchAccountsPlaceholder') || t('accountManager.searchPlaceholder')}
                        className="w-full pl-8.5 pr-8 py-1.5 text-xs bg-muted/60 border border-border rounded-lg text-foreground focus:outline-none focus:ring-1 focus:ring-ring placeholder:text-muted-foreground"
                    />
                    {searchQuery && (
                        <button
                            type="button"
                            onClick={() => onSearchChange('')}
                            className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-0.5"
                            title={t('accountManager.clearSearch')}
                        >
                            <X className="w-3 h-3" />
                        </button>
                    )}
                </div>
            </div>
        </div>
    )
}
