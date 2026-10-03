import { ChevronLeft, ChevronRight, Play, Plus, SlidersHorizontal, RefreshCw, Zap } from 'lucide-react'
import clsx from 'clsx'
import AccountFiltersBar from './AccountFiltersBar'
import AccountItemRow from './AccountItemRow'

export default function AccountsTable({
    t,
    accounts,
    loadingAccounts,
    testing,
    testingAll,
    batchProgress,
    sessionCounts,
    deletingSessions,
    updatingProxy,
    togglingEnabled,
    togglingAllEnabled,
    totalAccounts,
    page,
    pageSize,
    totalPages,
    resolveAccountIdentifier,
    proxies = [],
    elasticPoolEnabled,
    onOpenElasticPool,
    onTestAll,
    onShowAddAccount,
    onShowCodexLogin,
    onEditAccount,
    onTestAccount,
    onViewQuota,
    onDeleteAccount,
    onDeleteAllSessions,
    onUpdateAccountProxy,
    onToggleAccountEnabled,
    onToggleAllAccountsEnabled,
    onPrevPage,
    onNextPage,
    onPageSizeChange,
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
    envBacked = false,
}) {
    const isFiltered = (
        (filterProvider && filterProvider !== 'all') ||
        (filterPoolType && filterPoolType !== 'all') ||
        (filterStatus && filterStatus !== 'all') ||
        (filterProxy && filterProxy !== 'all') ||
        Boolean(searchQuery && searchQuery.trim())
    )

    return (
        <div className="bg-card border border-border rounded-xl overflow-hidden shadow-sm">
            {/* Header Section */}
            <div className="p-5 sm:p-6 border-b border-border flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <div className="flex items-center gap-2.5">
                        <h2 className="text-lg font-bold text-foreground tracking-tight">
                            {t('accountManager.accountsTitle')}
                        </h2>
                        <span className="px-2 py-0.5 rounded-full text-xs font-semibold bg-primary/10 text-primary border border-primary/20">
                            {totalAccounts}
                        </span>
                    </div>
                    <p className="text-xs sm:text-sm text-muted-foreground mt-0.5">
                        {t('accountManager.accountsDesc')}
                    </p>
                </div>
                <div className="flex flex-wrap items-center gap-2.5">
                    <button
                        type="button"
                        onClick={onTestAll}
                        disabled={testingAll || totalAccounts === 0}
                        className="flex items-center gap-1.5 px-3 py-2 bg-secondary text-secondary-foreground hover:bg-secondary/80 rounded-lg transition-colors text-xs font-medium border border-border disabled:opacity-50 shadow-xs"
                    >
                        {testingAll ? (
                            <RefreshCw className="w-3.5 h-3.5 animate-spin text-primary" />
                        ) : (
                            <Play className="w-3.5 h-3.5 text-emerald-400" />
                        )}
                        <span>{t('accountManager.testAll')}</span>
                    </button>
                    {onShowCodexLogin && (
                        <button
                            type="button"
                            onClick={onShowCodexLogin}
                            className="flex items-center gap-1.5 px-3.5 py-2 bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20 border border-emerald-500/25 rounded-lg transition-colors font-medium text-xs shadow-xs"
                            title="OAuth PKCE Login with OpenAI / ChatGPT"
                        >
                            <Zap className="w-3.5 h-3.5 text-emerald-400" />
                            <span>Codex OAuth</span>
                        </button>
                    )}
                    <button
                        type="button"
                        onClick={onShowAddAccount}
                        className="flex items-center gap-1.5 px-3.5 py-2 bg-primary text-primary-foreground hover:bg-primary/90 rounded-lg transition-colors font-medium text-xs shadow-xs"
                    >
                        <Plus className="w-4 h-4" />
                        <span>{t('accountManager.addAccount')}</span>
                    </button>
                </div>
            </div>

            {/* Interactive Stat Strip & Group Filter Toolbar */}
            <AccountFiltersBar
                t={t}
                searchQuery={searchQuery}
                onSearchChange={onSearchChange}
                filterProvider={filterProvider}
                onFilterProviderChange={onFilterProviderChange}
                filterPoolType={filterPoolType}
                onFilterPoolTypeChange={onFilterPoolTypeChange}
                filterStatus={filterStatus}
                onFilterStatusChange={onFilterStatusChange}
                filterProxy={filterProxy}
                onFilterProxyChange={onFilterProxyChange}
                onResetFilters={onResetFilters}
                accountStats={accountStats}
                totalAccounts={totalAccounts}
                proxies={proxies}
            />

            {/* Elastic Pool and Batch Operations Sub-bar */}
            <div className="px-5 py-2.5 border-b border-border bg-muted/20 flex flex-wrap items-center justify-between gap-3">
                <button
                    type="button"
                    onClick={onOpenElasticPool}
                    className={clsx(
                        "flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors shadow-xs",
                        elasticPoolEnabled
                            ? "bg-primary/15 text-primary border-primary/40 hover:bg-primary/25"
                            : "bg-muted text-muted-foreground border-border hover:bg-muted/80"
                    )}
                >
                    <span className={clsx(
                        "w-2 h-2 rounded-full",
                        elasticPoolEnabled ? "bg-emerald-500 animate-pulse" : "bg-muted-foreground/60"
                    )} />
                    <SlidersHorizontal className="w-3 h-3" />
                    <span>{t('accountManager.elasticPool')}</span>
                </button>

                <div className="flex items-center gap-2">
                    <button
                        type="button"
                        onClick={() => onToggleAllAccountsEnabled(false)}
                        disabled={elasticPoolEnabled || togglingAllEnabled || testingAll || totalAccounts === 0}
                        className="px-2.5 py-1.5 bg-destructive/10 text-destructive border border-destructive/20 rounded-lg hover:bg-destructive/20 transition-colors text-xs font-medium disabled:opacity-50"
                    >
                        {togglingAllEnabled ? <span className="animate-spin mr-1">⟳</span> : null}
                        {t('accountManager.disableAllAccounts')}
                    </button>
                    <button
                        type="button"
                        onClick={() => onToggleAllAccountsEnabled(true)}
                        disabled={elasticPoolEnabled || togglingAllEnabled || testingAll || totalAccounts === 0}
                        className="px-2.5 py-1.5 bg-secondary text-secondary-foreground hover:bg-secondary/80 transition-colors text-xs font-medium border border-border disabled:opacity-50"
                    >
                        {togglingAllEnabled ? <span className="animate-spin mr-1">⟳</span> : null}
                        {t('accountManager.enableAllAccounts')}
                    </button>
                </div>
            </div>

            {/* Testing All Progress Bar */}
            {testingAll && batchProgress && batchProgress.total > 0 && (
                <div className="p-4 border-b border-border bg-muted/40 animate-in fade-in duration-200">
                    <div className="flex items-center justify-between text-xs mb-2">
                        <span className="font-semibold text-foreground">{t('accountManager.testingAllAccounts')}</span>
                        <span className="text-muted-foreground font-mono">{batchProgress.current} / {batchProgress.total}</span>
                    </div>
                    <div className="w-full bg-muted rounded-full h-2 overflow-hidden mb-3">
                        <div
                            className="bg-primary h-full transition-all duration-300"
                            style={{ width: `${(batchProgress.current / batchProgress.total) * 100}%` }}
                        />
                    </div>
                    {batchProgress.results && batchProgress.results.length > 0 && (
                        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-1.5 max-h-28 overflow-y-auto custom-scrollbar">
                            {batchProgress.results.map((r, i) => (
                                <div
                                    key={i}
                                    className={clsx(
                                        "text-[11px] px-2 py-1 rounded border truncate font-mono",
                                        r.success
                                            ? "bg-emerald-500/10 border-emerald-500/20 text-emerald-400"
                                            : "bg-destructive/10 border-destructive/20 text-destructive"
                                    )}
                                >
                                    {r.success ? '✓' : '✗'} {r.id}
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            )}

            {/* Account List Rows */}
            <div className="divide-y divide-border">
                {loadingAccounts ? (
                    <div className="p-8 space-y-4">
                        {[1, 2, 3].map(i => (
                            <div key={i} className="animate-pulse flex items-center justify-between gap-4 p-4 rounded-lg bg-muted/20">
                                <div className="flex items-center gap-3">
                                    <div className="w-10 h-10 rounded-xl bg-muted" />
                                    <div className="space-y-2">
                                        <div className="h-4 w-48 bg-muted rounded" />
                                        <div className="h-3 w-32 bg-muted/70 rounded" />
                                    </div>
                                </div>
                                <div className="h-8 w-28 bg-muted rounded-lg" />
                            </div>
                        ))}
                    </div>
                ) : accounts.length > 0 ? (
                    accounts.map((acc, i) => (
                        <AccountItemRow
                            key={acc.identifier || acc.email || acc.mobile || acc.name || i}
                            acc={acc}
                            t={t}
                            resolveAccountIdentifier={resolveAccountIdentifier}
                            proxies={proxies}
                            sessionCounts={sessionCounts}
                            deletingSessions={deletingSessions}
                            updatingProxy={updatingProxy}
                            togglingEnabled={togglingEnabled}
                            testing={testing}
                            elasticPoolEnabled={elasticPoolEnabled}
                            envBacked={envBacked}
                            onToggleAccountEnabled={onToggleAccountEnabled}
                            onUpdateAccountProxy={onUpdateAccountProxy}
                            onEditAccount={onEditAccount}
                            onTestAccount={onTestAccount}
                            onViewQuota={onViewQuota}
                            onDeleteAccount={onDeleteAccount}
                            onDeleteAllSessions={onDeleteAllSessions}
                        />
                    ))
                ) : (
                    <div className="p-12 text-center space-y-3">
                        <div className="w-12 h-12 rounded-full bg-muted/60 text-muted-foreground flex items-center justify-center mx-auto">
                            <SlidersHorizontal className="w-6 h-6 opacity-60" />
                        </div>
                        <div className="text-sm font-medium text-foreground">
                            {isFiltered
                                ? (t('accountManager.noAccountsFiltered') || t('accountManager.searchNoResults'))
                                : t('accountManager.noAccounts')}
                        </div>
                        {isFiltered && (
                            <div>
                                <button
                                    type="button"
                                    onClick={onResetFilters}
                                    className="px-3 py-1.5 text-xs font-medium bg-secondary text-secondary-foreground hover:bg-secondary/80 border border-border rounded-lg transition-colors"
                                >
                                    {t('accountManager.clearFilterAction') || t('accountManager.filterReset')}
                                </button>
                            </div>
                        )}
                    </div>
                )}
            </div>

            {/* Pagination Section */}
            {totalPages > 1 && (
                <div className="p-4 border-t border-border bg-muted/10 flex flex-wrap items-center justify-between gap-3">
                    <div className="flex items-center gap-3">
                        <span className="text-xs text-muted-foreground">
                            {t('accountManager.pageInfo', { current: page, total: totalPages, count: totalAccounts })}
                        </span>
                        <select
                            value={pageSize}
                            onChange={e => onPageSizeChange(Number(e.target.value))}
                            className="text-xs border border-border rounded-md px-2 py-1 bg-background text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
                        >
                            {[10, 20, 50, 100, 500, 1000].map(s => (
                                <option key={s} value={s}>{s} / trang</option>
                            ))}
                        </select>
                    </div>
                    <div className="flex items-center gap-2">
                        <button
                            type="button"
                            onClick={onPrevPage}
                            disabled={page <= 1 || loadingAccounts}
                            className="p-1.5 border border-border rounded-lg hover:bg-secondary transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                            title="Trang trước"
                        >
                            <ChevronLeft className="w-4 h-4" />
                        </button>
                        <span className="text-xs font-medium px-2">{page} / {totalPages}</span>
                        <button
                            type="button"
                            onClick={onNextPage}
                            disabled={page >= totalPages || loadingAccounts}
                            className="p-1.5 border border-border rounded-lg hover:bg-secondary transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                            title="Trang sau"
                        >
                            <ChevronRight className="w-4 h-4" />
                        </button>
                    </div>
                </div>
            )}
        </div>
    )
}
