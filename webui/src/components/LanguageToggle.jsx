import { Languages } from 'lucide-react'

import { useI18n } from '../i18n'

const LANGS = ['zh', 'en', 'vi']
const LANG_BADGES = {
    zh: '中',
    en: 'EN',
    vi: 'VI',
}

export default function LanguageToggle({ className = '', compact = false }) {
    const { lang, setLang, t } = useI18n()
    const currentIndex = LANGS.indexOf(lang)
    const nextLang = LANGS[currentIndex === -1 ? 0 : (currentIndex + 1) % LANGS.length]

    const getLabel = (l) => {
        if (l === 'zh') return t('language.chinese')
        if (l === 'vi') return t('language.vietnamese')
        return t('language.english')
    }

    const getTitle = (l) => {
        if (l === 'zh') return t('language.switchToChinese')
        if (l === 'vi') return t('language.switchToVietnamese')
        return t('language.switchToEnglish')
    }

    const label = getLabel(nextLang)
    const title = getTitle(nextLang)

    return (
        <button
            type="button"
            onClick={() => setLang(nextLang)}
            className={`inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-card/80 text-xs font-medium text-muted-foreground shadow-sm backdrop-blur transition-colors hover:border-primary/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background ${compact ? 'h-8 px-2' : 'h-9 px-3'} ${className}`}
            title={title}
            aria-label={title}
        >
            <Languages className="h-3.5 w-3.5" />
            <span>{LANG_BADGES[nextLang] || nextLang.toUpperCase()}</span>
            {!compact && <span className="hidden sm:inline text-muted-foreground/70">· {label}</span>}
        </button>
    )
}
