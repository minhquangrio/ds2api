import { useEffect, useState } from 'react'

export function useAccountsData({ apiFetch }) {
    const [queueStatus, setQueueStatus] = useState(null)
    const [keysExpanded, setKeysExpanded] = useState(false)

    const [accounts, setAccounts] = useState([])
    const [page, setPage] = useState(1)
    const [pageSize, setPageSize] = useState(10)
    const [totalPages, setTotalPages] = useState(1)
    const [totalAccounts, setTotalAccounts] = useState(0)
    const [loadingAccounts, setLoadingAccounts] = useState(false)
    const [accountStats, setAccountStats] = useState({
        total: 0,
        active: 0,
        deepseek: 0,
        gemini: 0,
        disabled: 0,
        issues: 0,
    })

    const resolveAccountIdentifier = (acc) => {
        if (!acc || typeof acc !== 'object') return ''
        return String(acc.identifier || acc.email || acc.mobile || acc.name || '').trim()
    }

    const [searchQuery, setSearchQuery] = useState('')
    const [filterProvider, setFilterProvider] = useState('all')
    const [filterPoolType, setFilterPoolType] = useState('all')
    const [filterStatus, setFilterStatus] = useState('all')
    const [filterProxy, setFilterProxy] = useState('all')

    const fetchAccounts = async (
        targetPage = page,
        targetPageSize = pageSize,
        targetQuery = searchQuery,
        targetProvider = filterProvider,
        targetPoolType = filterPoolType,
        targetStatus = filterStatus,
        targetProxy = filterProxy
    ) => {
        setLoadingAccounts(true)
        try {
            let url = `/admin/accounts?page=${targetPage}&page_size=${targetPageSize}`
            if (targetQuery && targetQuery.trim()) url += `&q=${encodeURIComponent(targetQuery.trim())}`
            if (targetProvider && targetProvider !== 'all') url += `&provider=${encodeURIComponent(targetProvider)}`
            if (targetPoolType && targetPoolType !== 'all') url += `&pool_type=${encodeURIComponent(targetPoolType)}`
            if (targetStatus && targetStatus !== 'all') url += `&status=${encodeURIComponent(targetStatus)}`
            if (targetProxy && targetProxy !== 'all') url += `&proxy_id=${encodeURIComponent(targetProxy)}`

            const res = await apiFetch(url)
            if (res.ok) {
                const data = await res.json()
                setAccounts(data.items || [])
                setTotalPages(data.total_pages || 1)
                setTotalAccounts(data.total || 0)
                setPage(data.page || 1)
                if (data.stats) {
                    setAccountStats(data.stats)
                }
            }
        } catch (e) {
            console.error('Failed to fetch accounts:', e)
        } finally {
            setLoadingAccounts(false)
        }
    }

    const changePageSize = (newSize) => {
        setPageSize(newSize)
        fetchAccounts(1, newSize, searchQuery, filterProvider, filterPoolType, filterStatus, filterProxy)
    }

    const handleSearchChange = (query) => {
        setSearchQuery(query)
        fetchAccounts(1, pageSize, query, filterProvider, filterPoolType, filterStatus, filterProxy)
    }

    const handleFilterProviderChange = (provider) => {
        setFilterProvider(provider)
        fetchAccounts(1, pageSize, searchQuery, provider, filterPoolType, filterStatus, filterProxy)
    }

    const handleFilterPoolTypeChange = (poolType) => {
        setFilterPoolType(poolType)
        fetchAccounts(1, pageSize, searchQuery, filterProvider, poolType, filterStatus, filterProxy)
    }

    const handleFilterStatusChange = (status) => {
        setFilterStatus(status)
        fetchAccounts(1, pageSize, searchQuery, filterProvider, filterPoolType, status, filterProxy)
    }

    const handleFilterProxyChange = (proxyId) => {
        setFilterProxy(proxyId)
        fetchAccounts(1, pageSize, searchQuery, filterProvider, filterPoolType, filterStatus, proxyId)
    }

    const handleResetFilters = () => {
        setSearchQuery('')
        setFilterProvider('all')
        setFilterPoolType('all')
        setFilterStatus('all')
        setFilterProxy('all')
        fetchAccounts(1, pageSize, '', 'all', 'all', 'all', 'all')
    }

    const fetchQueueStatus = async () => {
        try {
            const res = await apiFetch('/admin/queue/status')
            if (res.ok) {
                const data = await res.json()
                setQueueStatus(data)
            }
        } catch (e) {
            console.error('Failed to fetch queue status:', e)
        }
    }

    useEffect(() => {
        fetchAccounts()
        fetchQueueStatus()
        const interval = setInterval(fetchQueueStatus, 5000)
        return () => clearInterval(interval)
    }, [])

    return {
        queueStatus,
        keysExpanded,
        setKeysExpanded,
        accounts,
        page,
        pageSize,
        totalPages,
        totalAccounts,
        loadingAccounts,
        fetchAccounts,
        changePageSize,
        resolveAccountIdentifier,
        searchQuery,
        handleSearchChange,
        filterProvider,
        handleFilterProviderChange,
        filterPoolType,
        handleFilterPoolTypeChange,
        filterStatus,
        handleFilterStatusChange,
        filterProxy,
        handleFilterProxyChange,
        handleResetFilters,
        accountStats,
    }
}

