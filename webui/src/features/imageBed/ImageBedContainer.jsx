import { useCallback, useEffect, useRef, useState } from 'react'
import {
    AlertCircle,
    Check,
    CheckCircle2,
    Copy,
    ExternalLink,
    FileImage,
    Github,
    Image as ImageIcon,
    Loader2,
    RefreshCw,
    Save,
    Settings,
    Trash2,
    UploadCloud,
    X,
} from 'lucide-react'
import clsx from 'clsx'

import { useI18n } from '../../i18n'

export default function ImageBedContainer({ authFetch, onMessage }) {
    const { t } = useI18n()
    const [config, setConfig] = useState({
        has_token: false,
        token_mask: '',
        token: '',
        owner: '',
        repository: '',
        path_prefix: 'images/',
    })
    const [history, setHistory] = useState([])
    const [loadingConfig, setLoadingConfig] = useState(false)
    const [loadingHistory, setLoadingHistory] = useState(false)
    const [savingConfig, setSavingConfig] = useState(false)
    const [validating, setValidating] = useState(false)
    const [showConfigModal, setShowConfigModal] = useState(false)

    // Upload states
    const [selectedFile, setSelectedFile] = useState(null)
    const [previewUrl, setPreviewUrl] = useState('')
    const [customFileName, setCustomFileName] = useState('')
    const [uploading, setUploading] = useState(false)
    const [copiedIndex, setCopiedIndex] = useState(null)
    const fileInputRef = useRef(null)

    const fetchConfig = useCallback(async () => {
        setLoadingConfig(true)
        try {
            const res = await authFetch('/admin/image-bed/config')
            if (res.ok) {
                const data = await res.json()
                setConfig(prev => ({
                    ...prev,
                    has_token: data.has_token,
                    token_mask: data.token_mask || '',
                    token: '',
                    owner: data.owner || '',
                    repository: data.repository || '',
                    path_prefix: data.path_prefix || 'images/',
                }))
            }
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setLoadingConfig(false)
        }
    }, [authFetch, onMessage])

    const fetchHistory = useCallback(async () => {
        setLoadingHistory(true)
        try {
            const res = await authFetch('/admin/image-bed/history')
            if (res.ok) {
                const data = await res.json()
                setHistory(Array.isArray(data?.items) ? data.items : [])
            }
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setLoadingHistory(false)
        }
    }, [authFetch, onMessage])

    useEffect(() => {
        fetchConfig()
        fetchHistory()
    }, [fetchConfig, fetchHistory])

    const handleSaveConfig = async (e) => {
        e?.preventDefault()
        setSavingConfig(true)
        try {
            const payload = {
                owner: config.owner,
                repository: config.repository,
                path_prefix: config.path_prefix,
            }
            if (config.token) {
                payload.token = config.token
            }
            const res = await authFetch('/admin/image-bed/config', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            })
            if (!res.ok) {
                const text = await res.text()
                throw new Error(text || 'Failed to save configuration')
            }
            onMessage?.({ type: 'success', text: t('imagebed.configSaved') })
            setShowConfigModal(false)
            fetchConfig()
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setSavingConfig(false)
        }
    }

    const handleValidateRepo = async () => {
        setValidating(true)
        try {
            const res = await authFetch('/admin/image-bed/validate', { method: 'POST' })
            const data = await res.json()
            if (data.valid) {
                onMessage?.({ type: 'success', text: t('imagebed.validateSuccess') })
            } else {
                throw new Error(data.error || 'Validation failed')
            }
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setValidating(false)
        }
    }

    const handleFileSelect = (file) => {
        if (!file || !file.type.startsWith('image/')) {
            onMessage?.({ type: 'error', text: t('imagebed.invalidImage') })
            return
        }
        setSelectedFile(file)
        setCustomFileName(file.name)
        const reader = new FileReader()
        reader.onload = (e) => setPreviewUrl(e.target.result)
        reader.readAsDataURL(file)
    }

    // Handle paste event (Ctrl+V) anywhere on page
    useEffect(() => {
        const handlePaste = (e) => {
            const items = e.clipboardData?.items
            if (!items) return
            for (let i = 0; i < items.length; i++) {
                if (items[i].type.indexOf('image') !== -1) {
                    const blob = items[i].getAsFile()
                    if (blob) {
                        handleFileSelect(blob)
                        break
                    }
                }
            }
        }
        window.addEventListener('paste', handlePaste)
        return () => window.removeEventListener('paste', handlePaste)
    }, [])

    const handleUpload = async () => {
        if (!previewUrl) return
        setUploading(true)
        try {
            const res = await authFetch('/admin/image-bed/upload', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    name: customFileName || selectedFile?.name || 'pasted-image.png',
                    data_url: previewUrl,
                }),
            })
            const data = await res.json()
            if (!res.ok) {
                throw new Error(data.error || 'Failed to upload image')
            }
            onMessage?.({ type: 'success', text: t('imagebed.uploadSuccess') })
            setSelectedFile(null)
            setPreviewUrl('')
            setCustomFileName('')
            fetchHistory()
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        } finally {
            setUploading(false)
        }
    }

    const copyToClipboard = (text, idxKey) => {
        navigator.clipboard.writeText(text)
        setCopiedIndex(idxKey)
        setTimeout(() => setCopiedIndex(null), 2000)
    }

    const handleDeleteItem = async (id, deleteRemote = false) => {
        if (!window.confirm(deleteRemote ? t('imagebed.confirmDeleteBoth') : t('imagebed.confirmDeleteRecord'))) {
            return
        }
        try {
            const url = `/admin/image-bed/history/${encodeURIComponent(id)}${deleteRemote ? '?delete_remote=true' : ''}`
            const res = await authFetch(url, { method: 'DELETE' })
            if (!res.ok) {
                const text = await res.text()
                throw new Error(text || 'Failed to delete')
            }
            onMessage?.({ type: 'success', text: t('imagebed.deletedSuccess') })
            fetchHistory()
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        }
    }

    const handleClearHistory = async () => {
        if (!window.confirm(t('imagebed.confirmClearAll'))) return
        try {
            const res = await authFetch('/admin/image-bed/history', { method: 'DELETE' })
            if (!res.ok) throw new Error('Failed to clear history')
            onMessage?.({ type: 'success', text: t('imagebed.historyCleared') })
            fetchHistory()
        } catch (err) {
            onMessage?.({ type: 'error', text: err.message })
        }
    }

    const isConfigured = config.has_token && config.owner && config.repository

    return (
        <div className="space-y-6 max-w-7xl mx-auto pb-12 animate-in fade-in duration-300">
            {/* Header */}
            <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm">
                <div className="flex items-center gap-3">
                    <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-violet-500 to-purple-600 flex items-center justify-center text-white shadow-lg shadow-purple-500/20">
                        <ImageIcon className="w-5 h-5" />
                    </div>
                    <div>
                        <h2 className="text-xl font-bold tracking-tight text-foreground flex items-center gap-2">
                            {t('imagebed.title')}
                            {isConfigured ? (
                                <span className="inline-flex items-center gap-1 text-[11px] font-semibold text-emerald-400 bg-emerald-500/10 px-2 py-0.5 rounded-full border border-emerald-500/20">
                                    <CheckCircle2 className="w-3 h-3" />
                                    {config.owner}/{config.repository}
                                </span>
                            ) : (
                                <span className="inline-flex items-center gap-1 text-[11px] font-semibold text-amber-400 bg-amber-500/10 px-2 py-0.5 rounded-full border border-amber-500/20">
                                    <AlertCircle className="w-3 h-3" />
                                    {t('imagebed.notConfigured')}
                                </span>
                            )}
                        </h2>
                        <p className="text-sm text-muted-foreground">{t('imagebed.subtitle')}</p>
                    </div>
                </div>

                <div className="flex items-center gap-2.5 w-full sm:w-auto justify-end">
                    {isConfigured && (
                        <button
                            onClick={handleValidateRepo}
                            disabled={validating}
                            className="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-secondary text-secondary-foreground text-xs font-medium hover:bg-secondary/80 transition"
                        >
                            <RefreshCw className={clsx("w-3.5 h-3.5", validating && "animate-spin")} />
                            <span>{validating ? t('imagebed.validating') : t('imagebed.validateRepo')}</span>
                        </button>
                    )}
                    <button
                        onClick={() => setShowConfigModal(true)}
                        className="inline-flex items-center gap-2 px-4 py-2 rounded-xl bg-primary text-primary-foreground font-medium text-sm hover:opacity-90 transition shadow-md shadow-primary/20"
                    >
                        <Settings className="w-4 h-4" />
                        <span>{t('imagebed.configureBtn')}</span>
                    </button>
                </div>
            </div>

            {/* Upload Box */}
            <div className="p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm space-y-4">
                <h3 className="font-bold text-base text-foreground flex items-center gap-2">
                    <UploadCloud className="w-5 h-5 text-primary" />
                    {t('imagebed.uploadAreaTitle')}
                </h3>

                <div
                    onDragOver={(e) => e.preventDefault()}
                    onDrop={(e) => {
                        e.preventDefault()
                        if (e.dataTransfer.files?.[0]) {
                            handleFileSelect(e.dataTransfer.files[0])
                        }
                    }}
                    onClick={() => fileInputRef.current?.click()}
                    className={clsx(
                        "border-2 border-dashed rounded-2xl p-8 flex flex-col items-center justify-center text-center cursor-pointer transition-all",
                        previewUrl ? "border-primary/50 bg-primary/5" : "border-border/80 hover:border-primary/40 hover:bg-card/40"
                    )}
                >
                    <input
                        ref={fileInputRef}
                        type="file"
                        accept="image/*"
                        className="hidden"
                        onChange={(e) => e.target.files?.[0] && handleFileSelect(e.target.files[0])}
                    />

                    {previewUrl ? (
                        <div className="flex flex-col items-center gap-4">
                            <img
                                src={previewUrl}
                                alt="Preview"
                                className="max-h-48 rounded-xl object-contain shadow-md border border-border/60"
                            />
                            <div className="text-xs text-muted-foreground flex items-center gap-2">
                                <FileImage className="w-4 h-4" />
                                <span>{selectedFile?.name || 'pasted-image.png'}</span>
                                {selectedFile && <span>({(selectedFile.size / 1024).toFixed(1)} KB)</span>}
                            </div>
                        </div>
                    ) : (
                        <div className="flex flex-col items-center gap-2 text-muted-foreground">
                            <div className="w-12 h-12 rounded-full bg-primary/10 flex items-center justify-center text-primary mb-1">
                                <UploadCloud className="w-6 h-6" />
                            </div>
                            <p className="text-sm font-medium text-foreground">{t('imagebed.dragOrClick')}</p>
                            <p className="text-xs text-muted-foreground">{t('imagebed.pasteSupport')}</p>
                        </div>
                    )}
                </div>

                {previewUrl && (
                    <div className="flex flex-col sm:flex-row items-center gap-3 pt-2">
                        <input
                            type="text"
                            value={customFileName}
                            onChange={(e) => setCustomFileName(e.target.value)}
                            placeholder={t('imagebed.customFileNamePlaceholder')}
                            className="w-full sm:flex-1 px-4 py-2 rounded-xl bg-background/50 border border-border/60 text-sm focus:outline-none focus:border-primary"
                        />
                        <div className="flex items-center gap-2 w-full sm:w-auto justify-end">
                            <button
                                onClick={() => {
                                    setSelectedFile(null)
                                    setPreviewUrl('')
                                }}
                                className="px-4 py-2 rounded-xl border border-border/60 hover:bg-secondary text-sm font-medium text-muted-foreground transition"
                            >
                                {t('common.cancel')}
                            </button>
                            <button
                                onClick={handleUpload}
                                disabled={uploading || !isConfigured}
                                className="inline-flex items-center gap-2 px-5 py-2 rounded-xl bg-primary text-primary-foreground font-medium text-sm hover:opacity-90 transition active:scale-95 disabled:opacity-50 shadow-md shadow-primary/20"
                            >
                                {uploading ? <Loader2 className="w-4 h-4 animate-spin" /> : <UploadCloud className="w-4 h-4" />}
                                <span>{uploading ? t('imagebed.uploading') : t('imagebed.uploadBtn')}</span>
                            </button>
                        </div>
                    </div>
                )}
            </div>

            {/* Gallery & History */}
            <div className="p-6 rounded-2xl bg-card/60 backdrop-blur-xl border border-border/80 shadow-sm space-y-4">
                <div className="flex items-center justify-between">
                    <div>
                        <h3 className="font-bold text-base text-foreground">{t('imagebed.historyTitle')}</h3>
                        <p className="text-xs text-muted-foreground">{t('imagebed.historyDesc')}</p>
                    </div>
                    {history.length > 0 && (
                        <button
                            onClick={handleClearHistory}
                            className="inline-flex items-center gap-1.5 text-xs text-destructive hover:underline px-2 py-1"
                        >
                            <Trash2 className="w-3.5 h-3.5" />
                            <span>{t('imagebed.clearAll')}</span>
                        </button>
                    )}
                </div>

                {loadingHistory ? (
                    <div className="min-h-[200px] flex items-center justify-center text-muted-foreground">
                        <Loader2 className="w-6 h-6 animate-spin text-primary" />
                    </div>
                ) : history.length === 0 ? (
                    <div className="py-12 text-center text-muted-foreground text-sm border border-dashed border-border/60 rounded-xl">
                        {t('imagebed.noHistory')}
                    </div>
                ) : (
                    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                        {history.map((item, idx) => (
                            <div
                                key={item.id || idx}
                                className="p-4 rounded-xl border border-border/60 bg-background/50 hover:bg-background/80 transition flex flex-col justify-between gap-3 group"
                            >
                                <div className="space-y-3">
                                    <div className="relative aspect-video rounded-lg overflow-hidden bg-black/20 flex items-center justify-center border border-border/40">
                                        <img
                                            src={item.url}
                                            alt={item.file_name}
                                            className="w-full h-full object-cover group-hover:scale-105 transition duration-300"
                                            loading="lazy"
                                        />
                                        <a
                                            href={item.url}
                                            target="_blank"
                                            rel="noreferrer"
                                            className="absolute top-2 right-2 p-1.5 rounded-lg bg-black/60 text-white hover:bg-black/80 transition opacity-0 group-hover:opacity-100"
                                        >
                                            <ExternalLink className="w-3.5 h-3.5" />
                                        </a>
                                    </div>
                                    <div className="space-y-1">
                                        <div className="font-semibold text-xs text-foreground truncate" title={item.file_name}>
                                            {item.file_name}
                                        </div>
                                        <div className="text-[11px] text-muted-foreground flex justify-between font-mono">
                                            <span>{((item.size || 0) / 1024).toFixed(1)} KB</span>
                                            <span>{new Date(item.created_at).toLocaleDateString()}</span>
                                        </div>
                                    </div>
                                </div>

                                <div className="space-y-2 pt-2 border-t border-border/40 text-xs">
                                    <div className="grid grid-cols-2 gap-1.5">
                                        <button
                                            onClick={() => copyToClipboard(item.url, `cdn_${idx}`)}
                                            className="inline-flex items-center justify-center gap-1 py-1.5 px-2 rounded-lg bg-secondary text-secondary-foreground hover:bg-secondary/80 font-mono text-[11px]"
                                        >
                                            {copiedIndex === `cdn_${idx}` ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                                            <span>CDN URL</span>
                                        </button>
                                        <button
                                            onClick={() => copyToClipboard(`![${item.file_name}](${item.url})`, `md_${idx}`)}
                                            className="inline-flex items-center justify-center gap-1 py-1.5 px-2 rounded-lg bg-primary/10 text-primary hover:bg-primary/20 font-mono text-[11px]"
                                        >
                                            {copiedIndex === `md_${idx}` ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                                            <span>Markdown</span>
                                        </button>
                                    </div>

                                    <div className="flex justify-end gap-2 pt-1">
                                        <button
                                            onClick={() => handleDeleteItem(item.id, false)}
                                            className="text-[11px] text-muted-foreground hover:text-foreground"
                                            title={t('imagebed.deleteHistoryOnly')}
                                        >
                                            {t('imagebed.delHistory')}
                                        </button>
                                        <button
                                            onClick={() => handleDeleteItem(item.id, true)}
                                            className="text-[11px] text-destructive hover:underline"
                                            title={t('imagebed.deleteFromGitHub')}
                                        >
                                            {t('imagebed.delGitHub')}
                                        </button>
                                    </div>
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {/* Modal Settings */}
            {showConfigModal && (
                <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-background/80 backdrop-blur-sm animate-in fade-in">
                    <div className="relative w-full max-w-lg p-6 rounded-2xl bg-card border border-border shadow-2xl space-y-5">
                        <div className="flex items-center justify-between pb-3 border-b border-border">
                            <div className="flex items-center gap-2">
                                <Github className="w-5 h-5 text-primary" />
                                <h3 className="font-bold text-base text-foreground">{t('imagebed.modalTitle')}</h3>
                            </div>
                            <button
                                onClick={() => setShowConfigModal(false)}
                                className="p-1 rounded-lg hover:bg-secondary text-muted-foreground"
                            >
                                <X className="w-5 h-5" />
                            </button>
                        </div>

                        <form onSubmit={handleSaveConfig} className="space-y-4 text-sm">
                            <div className="space-y-1">
                                <label className="text-xs font-semibold text-muted-foreground">{t('imagebed.tokenLabel')}</label>
                                <input
                                    type="password"
                                    value={config.token}
                                    onChange={(e) => setConfig({ ...config, token: e.target.value })}
                                    placeholder={config.has_token ? config.token_mask : 'ghp_xxxxxxxxxxxx'}
                                    className="w-full px-3 py-2 rounded-xl bg-background/50 border border-border focus:border-primary font-mono text-xs"
                                />
                                <p className="text-[11px] text-muted-foreground">{t('imagebed.tokenHint')}</p>
                            </div>

                            <div className="grid grid-cols-2 gap-3">
                                <div className="space-y-1">
                                    <label className="text-xs font-semibold text-muted-foreground">{t('imagebed.ownerLabel')}</label>
                                    <input
                                        type="text"
                                        required
                                        value={config.owner}
                                        onChange={(e) => setConfig({ ...config, owner: e.target.value })}
                                        placeholder="octocat"
                                        className="w-full px-3 py-2 rounded-xl bg-background/50 border border-border focus:border-primary text-xs"
                                    />
                                </div>
                                <div className="space-y-1">
                                    <label className="text-xs font-semibold text-muted-foreground">{t('imagebed.repoLabel')}</label>
                                    <input
                                        type="text"
                                        required
                                        value={config.repository}
                                        onChange={(e) => setConfig({ ...config, repository: e.target.value })}
                                        placeholder="image-bed"
                                        className="w-full px-3 py-2 rounded-xl bg-background/50 border border-border focus:border-primary text-xs"
                                    />
                                </div>
                            </div>

                            <div className="space-y-1">
                                <label className="text-xs font-semibold text-muted-foreground">{t('imagebed.pathPrefixLabel')}</label>
                                <input
                                    type="text"
                                    value={config.path_prefix}
                                    onChange={(e) => setConfig({ ...config, path_prefix: e.target.value })}
                                    placeholder="images/"
                                    className="w-full px-3 py-2 rounded-xl bg-background/50 border border-border focus:border-primary font-mono text-xs"
                                />
                            </div>

                            <div className="pt-3 flex justify-end gap-2 border-t border-border">
                                <button
                                    type="button"
                                    onClick={() => setShowConfigModal(false)}
                                    className="px-4 py-2 rounded-xl border border-border hover:bg-secondary text-xs font-medium"
                                >
                                    {t('common.cancel')}
                                </button>
                                <button
                                    type="submit"
                                    disabled={savingConfig}
                                    className="inline-flex items-center gap-1.5 px-4 py-2 rounded-xl bg-primary text-primary-foreground font-medium text-xs hover:opacity-90 disabled:opacity-50"
                                >
                                    {savingConfig && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                                    <span>{savingConfig ? t('common.saving') : t('common.save')}</span>
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            )}
        </div>
    )
}
