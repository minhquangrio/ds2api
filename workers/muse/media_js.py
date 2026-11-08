"""Browser-side JS for Muse media flows.

Provenance and status
---------------------
The chat selectors in ``driver_engine.py`` come from the pinned upstream engine
(see UPSTREAM.md). The selectors below are DS2API-original: they were written
from the DOM contracts the chat flow already relies on (agent bubbles carrying
``hatch-agent-bubble-bg``, the composer ``textarea``, Stop button test ids) plus
generic HTML upload elements.

THEY ARE NOT VERIFIED AGAINST A LIVE Muse.ai ACCOUNT. The offline fixtures in
``test_engine.py`` prove the control flow (upload gate, only-new-media rule,
completion rules, download validation), not that muse.ai renders this DOM today.
Run ``scripts/muse_media_live_check.py`` with real credentials before publishing
this capability. Every failure path raises ``DriverFailure`` so the caller gets
an explicit error instead of a wrong image.
"""

from __future__ import annotations

# Locate the first file input that can accept an image, and express it as a CSS
# selector Playwright can target. Returns null when the composer exposes none.
FILE_INPUT_SELECTOR_JS = """() => {
    const inputs = [...document.querySelectorAll('input[type=file]')];
    const pick = inputs.find(i => (i.accept || '').toLowerCase().includes('image')
        || (i.accept || '').toLowerCase().includes('*')) || inputs[0];
    if (!pick) return null;
    if (pick.id) return '#' + CSS.escape(pick.id);
    const named = pick.getAttribute('name');
    if (named) return 'input[type=file][name="' + named + '"]';
    const box = pick.closest('form,div');
    if (!box) return null;
    const all = [...(box.querySelectorAll ? box.querySelectorAll('input[type=file]') : [])];
    const idx = all.indexOf(pick);
    if (idx < 0) return null;
    pick.setAttribute('data-ds2-file-input', String(idx));
    return 'input[type=file][data-ds2-file-input="' + idx + '"]';
}"""

# Count attach previews inside the composer region only. Used before and after
# the upload so an unconfirmed upload is never turned into a generation call.
UPLOAD_PREVIEW_JS = """() => {
    const scope = document.querySelector('form,footer,[class*="composer"]') || document.body;
    const nodes = scope.querySelectorAll('img[src^="blob:"],img[src^="data:"],[class*="attach"] img,[class*="upload"] img,[class*="preview"] img');
    const files = [...scope.querySelectorAll('input[type=file]')].reduce((n, i) => n + ((i.files || []).length), 0);
    return {previews: nodes.length, files};
}"""

# Media in the LAST agent bubble only. The user bubble holding the reference
# preview must never match, otherwise an edit would return its own input.
POLL_IMAGE_JS = """() => {
    const scope = document.querySelector('main,[class*="chat-scroll"],[class*="hatch-chat-scroll"]') || document.body;
    const bubbles = [...scope.querySelectorAll('div[class*="hatch-chat-groupable-bubble"]')]
        .filter(b => /hatch-agent-bubble-bg/.test(b.className || ''));
    const last = bubbles.length ? bubbles[bubbles.length-1] : null;
    if (last) last.scrollIntoView({block:'end'});
    const stop = !!document.querySelector('[data-testid="hatch-composer-stop-button"],button[aria-label*="Stop" i],button[aria-label*="停止"]');
    const imgs = last ? [...last.querySelectorAll('img')] : [];
    return {
        count: bubbles.length,
        stop,
        text: last ? (last.innerText || '').trim() : '',
        items: imgs.map(im => ({
            src: im.currentSrc || im.src || '',
            complete: !!im.complete,
            width: im.naturalWidth || 0,
            height: im.naturalHeight || 0,
            alt: im.alt || ''
        })).filter(i => i.src && !i.src.startsWith('data:image/svg'))
    };
}"""

POLL_VIDEO_JS = """() => {
    const scope = document.querySelector('main,[class*="chat-scroll"],[class*="hatch-chat-scroll"]') || document.body;
    const bubbles = [...scope.querySelectorAll('div[class*="hatch-chat-groupable-bubble"]')]
        .filter(b => /hatch-agent-bubble-bg/.test(b.className || ''));
    const last = bubbles.length ? bubbles[bubbles.length-1] : null;
    const stop = !!document.querySelector('[data-testid="hatch-composer-stop-button"],button[aria-label*="Stop" i],button[aria-label*="停止"]');
    const vids = last ? [...last.querySelectorAll('video')] : [];
    const sources = vids.map(v => v.currentSrc || v.src || (v.querySelector('source') ? v.querySelector('source').src : ''))
        .filter(s => !!s);
    return {count: bubbles.length, stop, sources, text: last ? (last.innerText || '').trim() : ''};
}"""

# blob: and data: URLs only resolve inside the page that created them.
FETCH_MEDIA_B64_JS = """async (src) => {
    const response = await fetch(src);
    if (!response.ok) return {ok: false, status: response.status};
    const blob = await response.blob();
    const buffer = await blob.arrayBuffer();
    const bytes = new Uint8Array(buffer);
    let binary = '';
    const step = 0x8000;
    for (let i = 0; i < bytes.length; i += step) {
        binary += String.fromCharCode.apply(null, bytes.subarray(i, i + step));
    }
    return {ok: true, status: 200, contentType: blob.type || '', base64: btoa(binary), size: bytes.length};
}"""
