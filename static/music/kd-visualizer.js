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

    // ---- public API stubs (real impls land in later tasks) ----
    function onAudioChanged() { /* Task 6 */ }
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
})();
