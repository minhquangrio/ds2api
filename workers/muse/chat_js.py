"""Composer/thread DOM contracts for the Muse chat and media flows.

Moved verbatim out of ``driver_engine.py`` so the image and video flows can share
the same constants without importing the chat driver (which would be circular).
``driver_engine`` re-exports these names, and the offline fixtures dispatch on
constant identity, so the strings must stay byte-for-byte identical.

Upstream provenance for the selectors: see UPSTREAM.md and UPSTREAM_LICENSE.
"""

from __future__ import annotations

READY_JS = """() => {
    const ta = document.querySelector('textarea');
    const shell = document.querySelector('[data-hatch-shell-hydration-state]');
    return document.readyState === 'complete' && ta && !ta.disabled &&
        (!shell || shell.getAttribute('data-hatch-shell-hydration-state') === 'hydrated') &&
        !document.body.innerText.includes('Connecting...');
}"""

SET_PROMPT_JS = """text => {
    const ta = document.querySelector('textarea');
    ta.focus();
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(ta, text);
    ta.dispatchEvent(new Event('input', {bubbles:true}));
    ta.dispatchEvent(new Event('change', {bubbles:true}));
    return ta.value === text;
}"""

SEND_JS = """() => {
    const button = [...document.querySelectorAll('button,[role=button]')]
        .filter(b => b.offsetParent !== null)
        .find(b => /发送|send/i.test(b.getAttribute('aria-label') || '') ||
            /发送|send/i.test(b.getAttribute('data-testid') || '') || /send/i.test(b.innerText || ''));
    if (!button || button.disabled) return false;
    button.click(); return true;
}"""

POLL_CHAT_JS = """() => {
    const scope = document.querySelector('main,[class*="chat-scroll"],[class*="hatch-chat-scroll"]') || document.body;
    const bubbles = [...scope.querySelectorAll('div[class*="hatch-chat-groupable-bubble"]')]
        .filter(b => /hatch-agent-bubble-bg/.test(b.className || ''));
    const last = bubbles.length ? bubbles[bubbles.length-1] : null;
    if (last) last.scrollIntoView({block:'end'});
    const stop = !!document.querySelector('[data-testid="hatch-composer-stop-button"],button[aria-label*="Stop" i],button[aria-label*="停止"]');
    return {count:bubbles.length, text:last ? (last.innerText || '').trim() : '', stop};
}"""

CHAT_MODELS = {"muse-spark", "muse-chat", "spark", "chat"}
IMAGE_MODELS = {"muse-image", "image", "muse/image"}
VIDEO_MODELS = {"muse-video", "video", "muse/video"}
