import { RefreshCw } from 'lucide-react'

const PROVIDER_OPTIONS = [
    { value: 'proxyxoay', labelKey: 'proxyManager.providerProxyXoay' },
    { value: 'bestproxy', labelKey: 'proxyManager.providerBestProxy' },
    { value: 'custom', labelKey: 'proxyManager.providerCustom' },
]

function Field({ label, hint, children }) {
    return (
        <div>
            <label className="block text-sm font-medium mb-1.5">{label}</label>
            {children}
            {hint && <p className="mt-1 text-[11px] text-muted-foreground">{hint}</p>}
        </div>
    )
}

export function createEmptyRotateForm() {
    return {
        provider: 'proxyxoay',
        endpoint: '',
        key: '',
        network: 'random',
        province: '0',
        protocol: 'http',
        whitelist: '',
        ttl_seconds: '',
        min_remaining_seconds: '',
    }
}

export function rotateFormFromProxy(proxy) {
    const rotate = proxy?.rotate
    if (!rotate) {
        return createEmptyRotateForm()
    }
    return {
        provider: rotate.provider || 'proxyxoay',
        endpoint: rotate.endpoint || '',
        // The stored key is never sent back by the API; an empty box means "keep".
        key: '',
        network: rotate.network || 'random',
        province: rotate.province ?? '0',
        protocol: rotate.protocol || 'http',
        whitelist: rotate.whitelist || '',
        ttl_seconds: rotate.ttl_seconds || '',
        min_remaining_seconds: rotate.min_remaining_seconds || '',
    }
}

export function buildRotatePayload(form) {
    if (form.mode !== 'rotate') {
        return null
    }
    const rotate = form.rotate
    const payload = {
        provider: rotate.provider,
        key: rotate.key,
        network: rotate.network,
        province: rotate.province,
        protocol: rotate.protocol,
        whitelist: rotate.whitelist,
    }
    if (rotate.endpoint) {
        payload.endpoint = rotate.endpoint
    }
    const ttl = Number(rotate.ttl_seconds)
    if (ttl > 0) {
        payload.ttl_seconds = ttl
    }
    const minRemaining = Number(rotate.min_remaining_seconds)
    if (minRemaining > 0) {
        payload.min_remaining_seconds = minRemaining
    }
    return payload
}

export function rotateSummary(t, proxy) {
    const lease = proxy?.rotate?.lease
    if (!lease || !lease.host) {
        return { label: t('proxyManager.leaseIdle'), tone: 'muted', detail: '' }
    }
    const remaining = Number(lease.remaining_seconds || 0)
    const detail = lease.location ? `${lease.network || ''} · ${lease.location}` : lease.network || ''
    const endpoint = `${lease.host}:${lease.port}`
    if (remaining <= 0) {
        return { label: t('proxyManager.leaseExpired'), tone: 'error', detail: endpoint }
    }
    return {
        label: `${endpoint} · ${t('proxyManager.leaseRemaining', { time: formatRemaining(t, remaining) })}`,
        tone: 'ok',
        detail,
    }
}

function formatRemaining(t, seconds) {
    if (seconds >= 60) {
        const minutes = Math.floor(seconds / 60)
        if (minutes >= 60) {
            return t('proxyManager.durationHoursMinutes', { hours: Math.floor(minutes / 60), minutes: minutes % 60 })
        }
        return t('proxyManager.durationMinutes', { minutes })
    }
    return t('proxyManager.durationSeconds', { seconds })
}

/**
 * RotateSourceFields is the "proxy xoay" half of the proxy modal.
 * It owns nothing: values live in the parent form so both modes share one save path.
 */
export default function ProxyRotateFields({ t, form, setForm }) {
    const setRotate = (patch) => setForm((prev) => ({ ...prev, rotate: { ...prev.rotate, ...patch } }))

    return (
        <div className="space-y-4 rounded-lg border border-primary/20 bg-primary/5 p-4">
            <div className="flex items-center gap-2 text-xs font-semibold text-primary">
                <RefreshCw className="w-3.5 h-3.5" />
                {t('proxyManager.rotateSectionTitle')}
            </div>

            <div className="grid md:grid-cols-2 gap-4">
                <Field label={t('proxyManager.rotateProviderLabel')}>
                    <select
                        className="input-field"
                        value={form.rotate.provider}
                        onChange={e => setRotate({ provider: e.target.value, endpoint: '' })}
                    >
                        {PROVIDER_OPTIONS.map(option => (
                            <option key={option.value} value={option.value}>{t(option.labelKey)}</option>
                        ))}
                    </select>
                </Field>
                <Field label={t('proxyManager.rotateProtocolLabel')}>
                    <select
                        className="input-field"
                        value={form.rotate.protocol}
                        onChange={e => setRotate({ protocol: e.target.value })}
                    >
                        <option value="http">http</option>
                        <option value="socks5">socks5</option>
                    </select>
                </Field>
            </div>

            <Field
                label={t('proxyManager.rotateKeyLabel')}
                hint={form.editingId ? t('proxyManager.rotateKeyKeepHint') : t('proxyManager.rotateKeyHelp')}
            >
                <input
                    type="password"
                    className="input-field bg-background font-mono"
                    placeholder={t('proxyManager.rotateKeyPlaceholder')}
                    value={form.rotate.key}
                    onChange={e => setRotate({ key: e.target.value })}
                />
            </Field>

            <Field label={t('proxyManager.rotateEndpointLabel')} hint={t('proxyManager.rotateEndpointHelp')}>
                <input
                    type="text"
                    className="input-field font-mono text-xs"
                    placeholder="https://proxyxoay.shop/api/get.php"
                    value={form.rotate.endpoint}
                    onChange={e => setRotate({ endpoint: e.target.value })}
                />
            </Field>

            <div className="grid md:grid-cols-2 gap-4">
                <Field label={t('proxyManager.rotateNetworkLabel')} hint={t('proxyManager.rotateNetworkHelp')}>
                    <input
                        type="text"
                        className="input-field"
                        placeholder="random"
                        value={form.rotate.network}
                        onChange={e => setRotate({ network: e.target.value })}
                    />
                </Field>
                <Field label={t('proxyManager.rotateProvinceLabel')} hint={t('proxyManager.rotateProvinceHelp')}>
                    <input
                        type="text"
                        className="input-field"
                        placeholder="0"
                        value={form.rotate.province}
                        onChange={e => setRotate({ province: e.target.value })}
                    />
                </Field>
            </div>

            <Field label={t('proxyManager.rotateWhitelistLabel')} hint={t('proxyManager.rotateWhitelistHelp')}>
                <input
                    type="text"
                    className="input-field font-mono text-xs"
                    placeholder="1.2.3.4"
                    value={form.rotate.whitelist}
                    onChange={e => setRotate({ whitelist: e.target.value })}
                />
            </Field>

            <div className="grid md:grid-cols-2 gap-4">
                <Field label={t('proxyManager.rotateTtlLabel')} hint={t('proxyManager.rotateTtlHelp')}>
                    <input
                        type="number"
                        min="1"
                        max="86400"
                        className="input-field"
                        placeholder="300"
                        value={form.rotate.ttl_seconds}
                        onChange={e => setRotate({ ttl_seconds: e.target.value })}
                    />
                </Field>
                <Field label={t('proxyManager.rotateMinRemainingLabel')} hint={t('proxyManager.rotateMinRemainingHelp')}>
                    <input
                        type="number"
                        min="1"
                        max="86400"
                        className="input-field"
                        placeholder="60"
                        value={form.rotate.min_remaining_seconds}
                        onChange={e => setRotate({ min_remaining_seconds: e.target.value })}
                    />
                </Field>
            </div>
        </div>
    )
}
