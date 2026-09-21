import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import {
    Activity,
    Key,
    Cpu,
    Globe,
    Server,
    History,
    Upload,
    Cloud,
    Settings as SettingsIcon,
    LogOut,
    Menu,
    X,
    Loader2,
    ChevronRight,
    Zap,
} from 'lucide-react'
import clsx from 'clsx'

import LanguageToggle from '../components/LanguageToggle'
import ThemeToggle from '../components/ThemeToggle'
import { useI18n } from '../i18n'

const TokenOverviewContainer = lazy(() => import('../features/overview/TokenOverviewContainer'))
const ApiKeysManagerContainer = lazy(() => import('../features/apiKeys/ApiKeysManagerContainer'))
const AccountManagerContainer = lazy(() => import('../features/account/AccountManagerContainer'))
const ApiTesterContainer = lazy(() => import('../features/apiTester/ApiTesterContainer'))
const ChatHistoryContainer = lazy(() => import('../features/chatHistory/ChatHistoryContainer'))
const BatchImport = lazy(() => import('../components/BatchImport'))
const VercelSyncContainer = lazy(() => import('../features/vercel/VercelSyncContainer'))
const SettingsContainer = lazy(() => import('../features/settings/SettingsContainer'))
const ProxyManagerContainer = lazy(() => import('../features/proxy/ProxyManagerContainer'))

function TabLoadingFallback({ label }) {
    return (
        <div className="min-h-[360px] rounded-2xl border border-border/80 bg-card/60 flex items-center justify-center">
            <div className="flex items-center gap-3 text-sm text-muted-foreground">
                <Loader2 className="w-5 h-5 animate-spin text-primary" />
                <span>{label}...</span>
            </div>
        </div>
    )
}

function BrandMark({ compact = false }) {
    return (
        <div className="flex items-center gap-2.5">
            <div className={clsx(
                "rounded-xl bg-gradient-to-br from-emerald-500 to-teal-600 flex items-center justify-center text-white shadow-lg shadow-emerald-500/20",
                compact ? "w-7 h-7" : "w-9 h-9"
            )}>
                <Zap className={compact ? "w-4 h-4" : "w-5 h-5"} />
            </div>
            {!compact && (
                <div className="leading-tight">
                    <div className="font-bold text-lg tracking-tight text-foreground flex items-center gap-1.5">
                        DS2API
                        <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-primary/10 text-primary font-semibold">GATEWAY</span>
                    </div>
                </div>
            )}
        </div>
    )
}

export default function DashboardShell({ token, onLogout, config, fetchConfig, showMessage, message, onForceLogout, isVercel }) {
    const { t } = useI18n()
    const location = useLocation()
    const navigate = useNavigate()
    const [sidebarOpen, setSidebarOpen] = useState(false)

    const navGroups = [
        {
            groupKey: 'gateway',
            title: t('nav.groups.gateway'),
            items: [
                { id: 'overview', label: t('nav.overview.label'), icon: Activity, description: t('nav.overview.desc') },
                { id: 'keys', label: t('nav.keys.label'), icon: Key, description: t('nav.keys.desc') },
                { id: 'history', label: t('nav.history.label'), icon: History, description: t('nav.history.desc') },
            ]
        },
        {
            groupKey: 'upstream',
            title: t('nav.groups.upstream'),
            items: [
                { id: 'accounts', label: t('nav.accounts.label'), icon: Cpu, description: t('nav.accounts.desc') },
                { id: 'proxies', label: t('nav.proxies.label'), icon: Globe, description: t('nav.proxies.desc') },
                { id: 'test', label: t('nav.test.label'), icon: Server, description: t('nav.test.desc') },
            ]
        },
        {
            groupKey: 'system',
            title: t('nav.groups.system'),
            items: [
                { id: 'import', label: t('nav.import.label'), icon: Upload, description: t('nav.import.desc') },
                { id: 'vercel', label: t('nav.vercel.label'), icon: Cloud, description: t('nav.vercel.desc') },
                { id: 'settings', label: t('nav.settings.label'), icon: SettingsIcon, description: t('nav.settings.desc') },
            ]
        }
    ]

    const allNavItems = navGroups.flatMap(g => g.items)
    const tabIds = new Set(allNavItems.map(item => item.id))
    const pathSegments = location.pathname.replace(/^\/+|\/+$/g, '').split('/').filter(Boolean)
    const routeSegments = pathSegments[0] === 'admin' ? pathSegments.slice(1) : pathSegments
    const pathTab = routeSegments[0] || ''
    const activeTab = tabIds.has(pathTab) ? pathTab : 'overview'
    const adminBasePath = pathSegments[0] === 'admin' ? '/admin' : ''
    const activeNavItem = allNavItems.find(n => n.id === activeTab)

    const navigateToTab = useCallback((tabID) => {
        const nextPath = tabID === 'overview'
            ? `${adminBasePath || ''}/`
            : `${adminBasePath}/${tabID}`
        navigate(nextPath)
        setSidebarOpen(false)
    }, [adminBasePath, navigate])

    const authFetch = useCallback(async (url, options = {}) => {
        const headers = {
            ...options.headers,
            'Authorization': `Bearer ${token}`
        }
        const res = await fetch(url, { ...options, headers })

        if (res.status === 401) {
            onLogout()
            throw new Error(t('auth.expired'))
        }
        return res
    }, [onLogout, t, token])

    const [versionInfo, setVersionInfo] = useState(null)

    useEffect(() => {
        let disposed = false
        async function loadVersion() {
            try {
                const res = await authFetch('/admin/version')
                const data = await res.json()
                if (!disposed) {
                    setVersionInfo(data)
                }
            } catch (_err) {
                if (!disposed) {
                    setVersionInfo(null)
                }
            }
        }
        loadVersion()
        return () => {
            disposed = true
        }
    }, [authFetch])

    const renderTab = () => {
        switch (activeTab) {
            case 'overview':
                return <TokenOverviewContainer config={config} authFetch={authFetch} onNavigate={navigateToTab} onMessage={showMessage} />
            case 'keys':
                return <ApiKeysManagerContainer config={config} onRefresh={fetchConfig} onMessage={showMessage} authFetch={authFetch} />
            case 'accounts':
                return <AccountManagerContainer config={config} onRefresh={fetchConfig} onMessage={showMessage} authFetch={authFetch} onNavigate={navigateToTab} />
            case 'proxies':
                return <ProxyManagerContainer config={config} onRefresh={fetchConfig} onMessage={showMessage} authFetch={authFetch} />
            case 'test':
                return <ApiTesterContainer config={config} onMessage={showMessage} authFetch={authFetch} />
            case 'history':
                return <ChatHistoryContainer onMessage={showMessage} authFetch={authFetch} />
            case 'import':
                return <BatchImport onRefresh={fetchConfig} onMessage={showMessage} authFetch={authFetch} />
            case 'vercel':
                return <VercelSyncContainer onMessage={showMessage} authFetch={authFetch} isVercel={isVercel} config={config} />
            case 'settings':
                return <SettingsContainer onRefresh={fetchConfig} onMessage={showMessage} authFetch={authFetch} onForceLogout={onForceLogout} isVercel={isVercel} />
            default:
                return null
        }
    }

    return (
        <div className="flex h-screen bg-background overflow-hidden text-foreground app-backdrop">
            {sidebarOpen && (
                <div
                    className="fixed inset-0 bg-background/70 backdrop-blur-sm z-40 lg:hidden"
                    onClick={() => setSidebarOpen(false)}
                />
            )}

            {/* Sidebar */}
            <aside className={clsx(
                "fixed lg:static inset-y-0 left-0 z-50 w-64 border-r border-border bg-card/80 backdrop-blur-xl transition-transform duration-300 ease-in-out lg:transform-none flex flex-col",
                sidebarOpen ? "translate-x-0 shadow-2xl" : "-translate-x-full"
            )}>
                <div className="px-5 pt-6 pb-5 border-b border-border/60">
                    <BrandMark />
                    <p className="mt-3 text-[10px] font-semibold tracking-[0.14em] uppercase text-muted-foreground">
                        {t('sidebar.onlineAdminConsole')}
                    </p>
                </div>

                <nav className="flex-1 px-3 py-4 space-y-5 overflow-y-auto custom-scrollbar">
                    {navGroups.map((group) => (
                        <div key={group.groupKey} className="space-y-1">
                            <div className="px-3 pb-1 text-[10px] font-bold uppercase tracking-wider text-muted-foreground/70">
                                {group.title}
                            </div>
                            {group.items.map((item) => {
                                const Icon = item.icon
                                const isActive = activeTab === item.id
                                return (
                                    <button
                                        key={item.id}
                                        onClick={() => navigateToTab(item.id)}
                                        className={clsx(
                                            "w-full flex items-center gap-3 px-3 py-2 rounded-xl text-xs font-medium transition-all duration-150 group relative",
                                            isActive
                                                ? "bg-primary/10 text-primary font-semibold"
                                                : "text-muted-foreground hover:bg-secondary/70 hover:text-foreground"
                                        )}
                                    >
                                        {isActive && (
                                            <span className="absolute left-0 top-1/2 -translate-y-1/2 h-5 w-[3px] rounded-full bg-primary" />
                                        )}
                                        <Icon className={clsx(
                                            "w-4 h-4 shrink-0 transition-colors",
                                            isActive ? "text-primary" : "text-muted-foreground group-hover:text-foreground"
                                        )} />
                                        <span className="flex-1 text-left">{item.label}</span>
                                        {isActive && <ChevronRight className="w-3.5 h-3.5 text-primary" />}
                                    </button>
                                )
                            })}
                        </div>
                    ))}
                </nav>

                <div className="p-4 border-t border-border/60 space-y-4">
                    <div className="flex items-center justify-between">
                        <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                            {t('sidebar.systemStatus')}
                        </span>
                        <span className="inline-flex items-center gap-1.5 text-[10px] font-semibold text-emerald-400 bg-emerald-500/10 px-2 py-0.5 rounded-full border border-emerald-500/20">
                            <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
                            {t('sidebar.statusOnline')}
                        </span>
                    </div>

                    <div className="grid grid-cols-2 gap-2">
                        <div className="rounded-xl border border-border/60 bg-background/60 px-3 py-2">
                            <div className="text-[9px] font-bold uppercase tracking-wider text-muted-foreground">
                                {t('sidebar.accounts')}
                            </div>
                            <div className="text-base font-bold font-mono text-foreground leading-tight mt-0.5">
                                {config.accounts?.length || 0}
                            </div>
                        </div>
                        <div className="rounded-xl border border-border/60 bg-background/60 px-3 py-2">
                            <div className="text-[9px] font-bold uppercase tracking-wider text-muted-foreground">
                                {t('sidebar.keys')}
                            </div>
                            <div className="text-base font-bold font-mono text-foreground leading-tight mt-0.5">
                                {config.keys?.length || 0}
                            </div>
                        </div>
                    </div>

                    <div className="rounded-xl border border-border/60 bg-background/60 px-3 py-2">
                        <div className="text-[9px] font-bold uppercase tracking-wider text-muted-foreground mb-0.5">
                            {t('sidebar.version')}
                        </div>
                        <div className="text-xs font-mono font-semibold text-foreground">
                            {versionInfo?.current_tag || '-'}
                        </div>
                        {versionInfo?.has_update && (
                            <a
                                className="inline-flex mt-1 text-[10px] font-medium text-primary hover:underline"
                                href={versionInfo?.release_url || 'https://github.com/CJackHwang/ds2api/releases/latest'}
                                target="_blank"
                                rel="noreferrer"
                            >
                                {t('sidebar.updateAvailable', { latest: versionInfo.latest_tag || '' })}
                            </a>
                        )}
                    </div>

                    <button
                        onClick={onLogout}
                        className="w-full h-8 flex items-center justify-center gap-2 rounded-xl border border-border text-xs font-medium text-muted-foreground hover:bg-destructive/10 hover:text-destructive hover:border-destructive/30 transition-all"
                    >
                        <LogOut className="w-3.5 h-3.5" />
                        {t('sidebar.signOut')}
                    </button>
                </div>
            </aside>

            {/* Main column */}
            <main className="flex-1 flex flex-col min-w-0 overflow-hidden relative">
                {/* Mobile top bar */}
                <header className="lg:hidden h-14 flex items-center justify-between px-4 border-b border-border bg-card/80 backdrop-blur-xl">
                    <BrandMark compact />
                    <div className="flex items-center gap-2">
                        <ThemeToggle compact />
                        <LanguageToggle compact />
                        <button
                            onClick={() => setSidebarOpen(true)}
                            className="p-2 -mr-1 rounded-lg text-muted-foreground hover:text-foreground hover:bg-secondary/70"
                            aria-label="Open navigation"
                        >
                            <Menu className="w-5 h-5" />
                        </button>
                    </div>
                </header>

                {/* Desktop top bar */}
                <header className="hidden lg:flex h-14 items-center justify-between px-8 lg:px-10 border-b border-border/60 bg-card/40 backdrop-blur-xl">
                    <div className="flex items-center gap-2.5 text-xs">
                        <span className="text-muted-foreground">Gateway</span>
                        <span className="text-muted-foreground/40">/</span>
                        <span className="font-semibold text-foreground">{activeNavItem?.label}</span>
                    </div>
                    <div className="flex items-center gap-4">
                        <div className="flex items-center gap-2 px-2.5 py-1 rounded-full bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 text-xs font-medium">
                            <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
                            <span>Gateway Online</span>
                        </div>
                        <div className="h-4 w-[1px] bg-border" />
                        <div className="flex items-center gap-2">
                            <ThemeToggle />
                            <LanguageToggle />
                        </div>
                    </div>
                </header>

                <div className="flex-1 overflow-auto">
                    <div className="max-w-7xl mx-auto px-4 py-6 lg:px-10 lg:py-8 space-y-6">
                        {message && (
                            <div className={clsx(
                                "px-4 py-3 rounded-xl border flex items-center gap-3 text-xs font-medium animate-in fade-in slide-in-from-top-2",
                                message.type === 'error'
                                    ? "bg-destructive/10 border-destructive/25 text-destructive"
                                    : "bg-emerald-500/10 border-emerald-500/25 text-emerald-400"
                            )}>
                                {message.type === 'error'
                                    ? <X className="w-4 h-4 shrink-0" />
                                    : <div className="w-4 h-4 rounded-full border border-emerald-400 flex items-center justify-center text-[9px] shrink-0">✓</div>}
                                <span>{message.text}</span>
                            </div>
                        )}

                        <div className="animate-in fade-in duration-300">
                            <Suspense fallback={<TabLoadingFallback label={activeNavItem?.label || 'DS2API'} />}>
                                {renderTab()}
                            </Suspense>
                        </div>
                    </div>
                </div>
            </main>
        </div>
    )
}
