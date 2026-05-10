// kd-visualizer.js — Milkdrop visualizer for the home-page chiptune
// player. Wraps butterchurn (the JS port of Milkdrop, same library
// Webamp uses). Adapted from the kopyparty integration.
//
// Consumes window.kdMusic (defined in index.html's music IIFE) for
// the AudioContext and current source node. Exposes window.kdVisualizer
// for the player to call on track change.

(function () {
    'use strict';

    var DEPS_BASE = '/music/deps/';

    // ---- module state ----
    var depsLoaded = false;
    var depsLoading = null;
    var viz = null;
    var canvas = null;
    var statusEl = null;
    var menuEl = null;
    var nameEl = null;
    var presets = null;
    var presetKeys = null;
    var presetIdx = 0;
    var connectedSrc = null;
    var rafId = null;
    var menuHideTimer = null;

    // auto-cycle. Default: ON, every 15s. Users can toggle off via the
    // visualizer control panel; the choice persists in localStorage.
    var AUTO_INTERVAL_OPTIONS_S = [5, 10, 15, 30, 60, 120, 300];
    var autoIntervalIdx = 2; // default 15s (index of 15 in AUTO_INTERVAL_OPTIONS_S)
    var autoCycle = true;
    var autoTimer = null;

    // Default preset shown on first load. Picked because the oscilloscope
    // pairs cleanly with tracker chiptune waveforms and isn't visually noisy.
    var DEFAULT_PRESET_KEY = '_Mig_Oscilloscope008';

    try {
        if (window.localStorage) {
            var savedAuto = window.localStorage.getItem('kd_music_viz_auto');
            if (savedAuto === '1') autoCycle = true;
            else if (savedAuto === '0') autoCycle = false;
            var savedIv = window.localStorage.getItem('kd_music_viz_interval');
            if (savedIv) {
                var n = parseInt(savedIv, 10);
                var idx = AUTO_INTERVAL_OPTIONS_S.indexOf(n);
                if (idx >= 0) autoIntervalIdx = idx;
            }
        }
    } catch (e) {}

    // ---- deps loader ----
    function loadScript(src) {
        return new Promise(function (resolve, reject) {
            var s = document.createElement('script');
            s.src = src;
            s.onload = function () { resolve(); };
            s.onerror = function () { reject(new Error('failed to load ' + src)); };
            document.head.appendChild(s);
        });
    }

    function loadDeps() {
        if (depsLoaded) return Promise.resolve();
        if (depsLoading) return depsLoading;
        // Engine first, then both preset packs in parallel. Tolerate
        // a single pack failing — we just have fewer presets.
        depsLoading = loadScript(DEPS_BASE + 'butterchurn.min.js').then(function () {
            return Promise.all([
                loadScript(DEPS_BASE + 'butterchurnPresets.min.js')
                    .catch(function (e) { console.warn(e); }),
                loadScript(DEPS_BASE + 'butterchurnPresetsExtra.min.js')
                    .catch(function (e) { console.warn(e); })
            ]);
        }).then(function () {
            if (!window.butterchurnPresets) throw new Error('butterchurnPresets unavailable');
            depsLoaded = true;
        });
        return depsLoading;
    }

    // ---- DOM lookups ----
    function findEls() {
        canvas = document.getElementById('music-viz-canvas');
        statusEl = document.getElementById('music-viz-status');
        menuEl = document.getElementById('music-viz-menu');
        nameEl = document.getElementById('mvm-name');
    }

    function setStatus(text) {
        if (statusEl) statusEl.textContent = text || '';
    }

    function connectAudioSource(src) {
        if (!viz || !src || src === connectedSrc) return;
        try {
            viz.connectAudio(src);
            connectedSrc = src;
        } catch (e) {
            console.warn('kdVisualizer connectAudio failed:', e);
        }
    }

    function resizeCanvas() {
        if (!canvas) return;
        var r = canvas.getBoundingClientRect();
        var dpr = window.devicePixelRatio || 1;
        var w = Math.max(64, Math.floor(r.width));
        var h = Math.max(64, Math.floor(r.height));
        var W = Math.floor(w * dpr);
        var H = Math.floor(h * dpr);
        if (canvas.width !== W || canvas.height !== H) {
            canvas.width = W;
            canvas.height = H;
            if (viz && typeof viz.setRendererSize === 'function') {
                // butterchurn's setRendererSize is (w, h, opts) and dereferences
                // opts.meshWidth/etc unconditionally — passing two args throws
                // TypeError, the catch swallowed it, and the renderer kept the
                // initial small viewport. That manifested as the visualization
                // filling only the upper-left of the fullscreen canvas.
                try { viz.setRendererSize(W, H, {}); } catch (e) {}
            }
        }
    }

    function ensureViz() {
        if (viz) return true;
        if (!window.kdMusic) return false;
        var ctx = window.kdMusic.getContext();
        if (!ctx) return false;
        if (!window.butterchurn || !window.butterchurnPresets) return false;
        if (!canvas) return false;

        resizeCanvas();

        try {
            var bc = window.butterchurn.default || window.butterchurn;
            viz = bc.createVisualizer(ctx, canvas, {
                width: canvas.width,
                height: canvas.height,
                pixelRatio: window.devicePixelRatio || 1,
                textureRatio: 1
            });
        } catch (e) {
            console.warn('kdVisualizer createVisualizer failed:', e);
            setStatus('webgl unavailable');
            return false;
        }

        // Merge the two bundled packs into a single dict.
        var packs = [window.butterchurnPresets, window.butterchurnPresetsExtra];
        presets = {};
        for (var i = 0; i < packs.length; i++) {
            var pack = packs[i];
            if (!pack) continue;
            var p = pack.default || pack;
            var dict = (typeof p.getPresets === 'function') ? p.getPresets() : p;
            if (dict && typeof dict === 'object') {
                for (var k in dict) {
                    if (Object.prototype.hasOwnProperty.call(dict, k))
                        presets[k] = dict[k];
                }
            }
        }
        presetKeys = Object.keys(presets).sort();
        if (!presetKeys.length) {
            setStatus('no presets');
            return false;
        }
        // Always start on the configured default preset; if the pack
        // doesn't ship that exact key, fall back to a random pick.
        var defaultIdx = presetKeys.indexOf(DEFAULT_PRESET_KEY);
        presetIdx = defaultIdx >= 0 ? defaultIdx : Math.floor(Math.random() * presetKeys.length);
        applyPreset(0);
        setStatus('');
        scheduleAutoCycle();
        return true;
    }

    function applyPreset(blendSec) {
        if (!viz || !presetKeys || !presetKeys.length) return;
        var key = presetKeys[presetIdx];
        var blend = typeof blendSec === 'number' ? blendSec : 1.5;
        try { viz.loadPreset(presets[key], blend); }
        catch (e) { console.warn('kdVisualizer applyPreset failed:', e); }
        if (nameEl) {
            var pretty = key.replace(/^[^-]+ - /, '');
            nameEl.textContent = pretty.length > 70 ? pretty.slice(0, 67) + '…' : pretty;
        }
    }

    function renderLoop() {
        rafId = requestAnimationFrame(renderLoop);
        if (!viz) return;
        if (!window.kdMusic) return;
        if (!window.kdMusic.isWindowVisible()) return;
        if (!window.kdMusic.isPlaying()) return;
        if (document.visibilityState !== 'visible') return;
        try { viz.render(); } catch (e) {}
    }

    // ---- preset stepping ----
    function prev() {
        if (!presetKeys || !presetKeys.length) return;
        presetIdx = (presetIdx - 1 + presetKeys.length) % presetKeys.length;
        applyPreset();
        scheduleAutoCycle();
    }
    function next() {
        if (!presetKeys || !presetKeys.length) return;
        presetIdx = (presetIdx + 1) % presetKeys.length;
        applyPreset();
        scheduleAutoCycle();
    }
    function random() {
        if (!presetKeys || presetKeys.length < 2) {
            if (presetKeys && presetKeys.length === 1) applyPreset();
            return;
        }
        var prevIdx = presetIdx;
        do {
            presetIdx = Math.floor(Math.random() * presetKeys.length);
        } while (presetIdx === prevIdx);
        applyPreset();
        scheduleAutoCycle();
    }

    // Always tears the timer down and rebuilds. Manual nudges
    // (prev/next/random) call this so the user's input doesn't get
    // immediately overridden by the timer firing.
    function scheduleAutoCycle() {
        if (autoTimer) { clearInterval(autoTimer); autoTimer = null; }
        if (!autoCycle) return;
        if (!viz) return;
        if (window.kdMusic && !window.kdMusic.isWindowVisible()) return;
        var ms = AUTO_INTERVAL_OPTIONS_S[autoIntervalIdx] * 1000;
        autoTimer = setInterval(function () {
            if (!viz) return;
            if (window.kdMusic && !window.kdMusic.isWindowVisible()) return;
            if (presetKeys && presetKeys.length > 1) {
                var prevIdx = presetIdx;
                do {
                    presetIdx = Math.floor(Math.random() * presetKeys.length);
                } while (presetIdx === prevIdx);
                applyPreset(2.5);
            }
        }, ms);
    }

    function fmtIntervalLabel(s) {
        return s < 60 ? (s + 's') : ((s / 60) + 'm');
    }

    function updateAutoUI() {
        var btn = document.getElementById('mvm-auto');
        var iv = document.getElementById('mvm-iv');
        if (btn) {
            if (autoCycle) btn.classList.add('on'); else btn.classList.remove('on');
        }
        if (iv) {
            iv.hidden = !autoCycle;
            iv.textContent = fmtIntervalLabel(AUTO_INTERVAL_OPTIONS_S[autoIntervalIdx]);
        }
    }

    // ---- preset menu ----
    function openMenu() {
        if (!menuEl) return;
        menuEl.hidden = false;
        resetMenuHideTimer();
    }
    function closeMenu() {
        if (!menuEl) return;
        menuEl.hidden = true;
        if (menuHideTimer) { clearTimeout(menuHideTimer); menuHideTimer = null; }
        var pop = document.getElementById('mvm-iv-pop');
        if (pop) pop.hidden = true;
    }
    function toggleMenu() {
        if (!menuEl) return;
        if (menuEl.hidden) openMenu(); else closeMenu();
    }
    function resetMenuHideTimer() {
        if (menuHideTimer) clearTimeout(menuHideTimer);
        menuHideTimer = setTimeout(function () {
            if (menuEl && !menuEl.hidden) closeMenu();
        }, 4000);
    }
    function isMenuOpen() {
        return !!(menuEl && !menuEl.hidden);
    }

    function bindMenuEvents() {
        if (!canvas || !menuEl) return;

        // click on canvas → toggle menu
        canvas.addEventListener('click', function (e) {
            if (!viz) return; // viz not initialized — ignore
            e.stopPropagation();
            toggleMenu();
        });

        // click anywhere outside the menu (and not on the canvas which already toggled) → close
        document.addEventListener('click', function (e) {
            if (!isMenuOpen()) return;
            if (menuEl.contains(e.target)) return;
            if (e.target === canvas) return;
            closeMenu();
        }, true);

        // hover/click inside menu refreshes the auto-hide timer
        menuEl.addEventListener('mousemove', resetMenuHideTimer);
        menuEl.addEventListener('click', resetMenuHideTimer);

        var bindBtn = function (id, fn) {
            var el = document.getElementById(id);
            if (el) el.addEventListener('click', function (e) {
                e.preventDefault();
                e.stopPropagation();
                fn();
                resetMenuHideTimer();
            });
        };
        bindBtn('mvm-prev', prev);
        bindBtn('mvm-next', next);
        bindBtn('mvm-rand', random);
        bindBtn('mvm-fs', toggleFullscreen);
        document.addEventListener('fullscreenchange', onFsChange);
        document.addEventListener('webkitfullscreenchange', onFsChange);

        var autoBtn = document.getElementById('mvm-auto');
        var ivPop = document.getElementById('mvm-iv-pop');
        if (autoBtn && ivPop) {
            var pressTimer = null;
            var longPress = false;

            function openIvPop() {
                ivPop.hidden = false;
                var opts = ivPop.querySelectorAll('.mvm-iv-opt');
                for (var i = 0; i < opts.length; i++) {
                    var s = parseInt(opts[i].getAttribute('data-s'), 10);
                    if (s === AUTO_INTERVAL_OPTIONS_S[autoIntervalIdx]) opts[i].classList.add('on');
                    else opts[i].classList.remove('on');
                }
                resetMenuHideTimer();
            }

            autoBtn.addEventListener('mousedown', function () {
                longPress = false;
                pressTimer = setTimeout(function () { longPress = true; openIvPop(); }, 400);
            });
            autoBtn.addEventListener('mouseup', function () {
                if (pressTimer) { clearTimeout(pressTimer); pressTimer = null; }
            });
            autoBtn.addEventListener('mouseleave', function () {
                if (pressTimer) { clearTimeout(pressTimer); pressTimer = null; }
            });
            autoBtn.addEventListener('contextmenu', function (e) {
                e.preventDefault();
                openIvPop();
            });
            autoBtn.addEventListener('click', function (e) {
                e.preventDefault();
                e.stopPropagation();
                if (longPress) { longPress = false; return; }
                setAutoCycle(!autoCycle);
                resetMenuHideTimer();
            });

            var opts = ivPop.querySelectorAll('.mvm-iv-opt');
            for (var i = 0; i < opts.length; i++) {
                opts[i].addEventListener('click', function (e) {
                    e.preventDefault();
                    e.stopPropagation();
                    var s = parseInt(this.getAttribute('data-s'), 10);
                    if (!isNaN(s)) setIntervalSec(s);
                    ivPop.hidden = true;
                    resetMenuHideTimer();
                });
            }
        }
    }

    // ---- public API ----
    function onAudioChanged() {
        // Called from index.html on track-load and on stop.
        // First call (with a non-null source) triggers dep load + viz init.
        // Subsequent calls just rewire the audio source.
        if (!window.kdMusic) return;
        var src = window.kdMusic.getSourceNode();
        if (!src) {
            // stop()'d — leave viz alone, the render loop's gate handles silence
            return;
        }
        if (depsLoaded && viz) {
            connectAudioSource(src);
            return;
        }
        setStatus('loading viz…');
        loadDeps().then(function () {
            if (!ensureViz()) return; // setStatus already called by ensureViz on failure
            connectAudioSource(src);
            if (rafId === null) renderLoop();
        }).catch(function (e) {
            console.warn('kdVisualizer load failed:', e && e.message || e);
            setStatus('viz unavailable');
        });
    }
    function setAutoCycle(on) {
        autoCycle = !!on;
        try {
            if (window.localStorage)
                window.localStorage.setItem('kd_music_viz_auto', autoCycle ? '1' : '0');
        } catch (e) {}
        updateAutoUI();
        scheduleAutoCycle();
    }

    function setIntervalSec(s) {
        var idx = AUTO_INTERVAL_OPTIONS_S.indexOf(s);
        if (idx < 0) return;
        autoIntervalIdx = idx;
        try {
            if (window.localStorage)
                window.localStorage.setItem('kd_music_viz_interval', String(s));
        } catch (e) {}
        // changing interval implicitly turns auto on
        if (!autoCycle) {
            setAutoCycle(true);
        } else {
            updateAutoUI();
            scheduleAutoCycle();
        }
    }
    function toggleFullscreen() {
        var wrap = document.getElementById('music-viz-wrap');
        if (!wrap) return;
        var fs = document.fullscreenElement || document.webkitFullscreenElement;
        if (fs) {
            try { (document.exitFullscreen || document.webkitExitFullscreen).call(document); } catch (e) {}
        } else {
            var req = wrap.requestFullscreen || wrap.webkitRequestFullscreen;
            if (!req) return;
            req.call(wrap).then(function () {
                setTimeout(resizeCanvas, 200);
            }).catch(function (e) { console.warn('fullscreen denied:', e); });
        }
    }

    function onFsChange() {
        var wrap = document.getElementById('music-viz-wrap');
        if (!wrap) return;
        var fs = document.fullscreenElement || document.webkitFullscreenElement;
        if (fs === wrap) wrap.classList.add('fullscreen');
        else wrap.classList.remove('fullscreen');
        setTimeout(resizeCanvas, 60);
    }
    function onResize() {
        resizeCanvas();
    }

    var resizeT = 0;
    window.addEventListener('resize', function () {
        if (!viz) return;
        clearTimeout(resizeT);
        resizeT = setTimeout(resizeCanvas, 120);
    });

    window.kdVisualizer = {
        onAudioChanged: onAudioChanged,
        openMenu: openMenu,
        closeMenu: closeMenu,
        toggleMenu: toggleMenu,
        prev: prev,
        next: next,
        random: random,
        setAutoCycle: setAutoCycle,
        setInterval: setIntervalSec,
        toggleFullscreen: toggleFullscreen,
        onResize: onResize,
    };

    function bindKeyboard() {
        document.addEventListener('keydown', function (e) {
            // Only when the music window is the focused window. The
            // existing window manager in index.html sets
            // window.__focusedWindow on click/focus.
            var win = document.getElementById('music-window');
            if (!win) return;
            if (window.__focusedWindow !== win) return;
            // Ignore typing in inputs.
            if (e.target && /^(input|textarea|select)$/i.test(e.target.tagName)) return;
            if (!viz) return;

            if (e.key === 'Escape') {
                if (document.fullscreenElement) {
                    try { document.exitFullscreen(); } catch (_) {}
                } else if (isMenuOpen()) {
                    closeMenu();
                } else {
                    return; // don't preventDefault if we did nothing
                }
            } else if (e.key === 'ArrowLeft') {
                prev();
            } else if (e.key === 'ArrowRight') {
                next();
            } else if (e.key === 'r' || e.key === 'R') {
                random();
            } else if (e.key === 'a' || e.key === 'A') {
                setAutoCycle(!autoCycle);
            } else if (e.key === 'f' || e.key === 'F') {
                toggleFullscreen();
            } else {
                return;
            }
            e.preventDefault();
        });
    }

    function init() {
        findEls();
        bindMenuEvents();
        bindKeyboard();
        updateAutoUI();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
