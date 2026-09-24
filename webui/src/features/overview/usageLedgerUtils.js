export function formatBucketLabel(startMs, endMs, bucketWidthMs) {
    const date = new Date(startMs);
    const isDateScale = bucketWidthMs >= 24 * 60 * 60 * 1000;
    if (isDateScale) {
        return `${date.getDate()}/${date.getMonth() + 1}`;
    }
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export function formatBucketFullTime(startMs, endMs) {
    const date = new Date(startMs);
    const endDate = new Date(endMs);
    return `${date.toLocaleDateString([], { month: '2-digit', day: '2-digit' })} ${date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} - ${endDate.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`;
}

export function isSuccessItem(item) {
    if (!item) return false;
    const status = String(item.status || '').toLowerCase();
    if (status === 'success' || status === 'ok') return true;
    if (status === 'error' || status === 'stopped') return false;
    if (typeof item.status_code === 'number' && item.status_code > 0) {
        return item.status_code >= 200 && item.status_code < 400;
    }
    if (typeof item.statusCode === 'number' && item.statusCode > 0) {
        return item.statusCode >= 200 && item.statusCode < 400;
    }
    return false;
}

export function usageQueryParams(timeRange, customStart, customEnd) {
    const params = new URLSearchParams();
    const range = timeRange || 'all';
    params.set('range', range);
    if (range === 'custom') {
        if (customStart) {
            const s = typeof customStart === 'number' ? customStart : new Date(customStart).getTime();
            if (!Number.isNaN(s) && s > 0) params.set('start', String(s));
        }
        if (customEnd) {
            const e = typeof customEnd === 'number' ? customEnd : new Date(customEnd).getTime();
            if (!Number.isNaN(e) && e > 0) params.set('end', String(e));
        }
    }
    try {
        const tz = new Date().getTimezoneOffset();
        params.set('tz', String(tz));
    } catch (_e) {}
    return params;
}

export function buildUsagePath(params) {
    const query = params ? params.toString() : '';
    return query ? `/admin/usage?${query}` : '/admin/usage';
}

export function mapUsageTotals(totals) {
    const t = totals || {};
    return {
        totalTokens: Number(t.total_tokens) || 0,
        totalPrompt: Number(t.prompt_tokens) || 0,
        totalCompletion: Number(t.completion_tokens) || 0,
        totalReasoning: Number(t.reasoning_tokens) || 0,
        totalReqs: Number(t.requests) || 0,
        successRequests: Number(t.success) || 0,
        errorRequests: Number(t.errors) || 0,
        stoppedRequests: Number(t.stopped) || 0,
        successRate: typeof t.success_rate === 'number' ? t.success_rate : 100,
        avgLatencyMs: Number(t.avg_latency_ms) || 0,
        tokensPerSec: Number(t.tokens_per_sec) || 0,
        tokensPerMin: Number(t.tokens_per_min) || 0,
        firstMs: Number(t.first_ms) || 0,
        lastMs: Number(t.last_ms) || 0,
    };
}

export function mapUsageRecent(recent) {
    if (!Array.isArray(recent)) return [];
    return recent.map((item, idx) => ({
        id: item.id || `usage-${idx}`,
        at: Number(item.at) || 0,
        created_at: item.at,
        model: item.model || 'unknown',
        status: item.status || 'unknown',
        status_code: item.status_code || 0,
        elapsed_ms: item.elapsed_ms || null,
        prompt_tokens: Number(item.prompt_tokens) || 0,
        completion_tokens: Number(item.completion_tokens) || 0,
        reasoning_tokens: Number(item.reasoning_tokens) || 0,
        total_tokens: Number(item.total_tokens) || 0,
        account_id: item.account_id || '',
        caller_id: item.caller_id || '',
        surface: item.surface || '',
        stream: Boolean(item.stream),
        finish_reason: item.finish_reason || '',
        backfilled: Boolean(item.backfilled),
    }));
}

export function mapUsageTimeline(timeline) {
    if (!Array.isArray(timeline)) return [];
    return timeline.map((bucket, idx) => {
        const startMs = Number(bucket.start_ms) || 0;
        const endMs = Number(bucket.end_ms) || startMs;
        const width = endMs - startMs;
        const label = formatBucketLabel(startMs, endMs, width);
        const fullTime = formatBucketFullTime(startMs, endMs);
        const models = Array.isArray(bucket.models) ? bucket.models : [];
        const modelStr = models.slice(0, 2).join(', ') + (models.length > 2 ? '...' : '');

        return {
            id: `bucket-${idx}`,
            startMs,
            endMs,
            bStart: startMs,
            bEnd: endMs,
            prompt: Number(bucket.prompt) || 0,
            completion: Number(bucket.completion) || 0,
            reasoning: Number(bucket.reasoning) || 0,
            total: Number(bucket.total) || 0,
            count: Number(bucket.count) || 0,
            elapsedMs: typeof bucket.avg_latency_ms === 'number' && bucket.avg_latency_ms > 0 ? bucket.avg_latency_ms : null,
            model: modelStr,
            models,
            label,
            fullTime,
        };
    });
}
