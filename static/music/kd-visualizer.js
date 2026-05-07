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

    // auto-cycle
    var AUTO_INTERVAL_OPTIONS_S = [5, 10, 15, 30, 60, 120, 300];
    var autoIntervalIdx = 3; // default 30s
    var autoCycle = false;
    var autoTimer = null;

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
                try { viz.setRendererSize(W, H); } catch (e) {}
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
        presetIdx = Math.floor(Math.random() * presetKeys.length);
        applyPreset(0);
        setStatus('');
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
    function openMenu() { /* Task 8 */ }
    function closeMenu() { /* Task 8 */ }
    function toggleMenu() { /* Task 8 */ }
    function prev() { /* Task 8 */ }
    function next() { /* Task 8 */ }
    function random() { /* Task 8 */ }
    function setAutoCycle(on) { /* Task 10 */ }
    function setIntervalSec(s) { /* Task 10 */ }
    function toggleFullscreen() { /* Task 12 */ }
    function onResize() { /* Task 13 */ }

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

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', findEls);
    } else {
        findEls();
    }
})();
