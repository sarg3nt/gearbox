/**
 * info-tooltip.js — client-side counterpart of the InfoTooltip templ
 * component (internal/framework/templates/components/info_tooltip.templ).
 *
 * Use this anywhere a chart, KPI card, or other widget is built in JS
 * and you want to attach the canonical info-circle + hover-tooltip
 * affordance. The DOM it produces matches the templ component, so both
 * server-rendered and client-built widgets look and behave the same.
 *
 * Behaviour matches the HAProxy overview pattern verbatim: pure CSS
 * via Tailwind `group` / `group-hover:visible`, no JS event wiring,
 * no cursor change (cursor stays as the default arrow), no transition
 * delay.
 *
 * Two surfaces are exposed:
 *
 *   window.createInfoTooltip(text)
 *     Returns a detached <span> element you can appendChild into a
 *     container. Safe under CSP — uses textContent for the user-
 *     supplied tooltip body, so the text can't introduce HTML.
 *
 *   window.appendInfoTooltipTo(parentEl, text)
 *     Convenience wrapper — creates the tooltip and appends it.
 */
(function () {
    'use strict';

    var SVG_NS = 'http://www.w3.org/2000/svg';
    var INFO_PATH = 'M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z';

    function createInfoTooltip(text) {
        var wrapper = document.createElement('span');
        wrapper.className = 'relative group inline-flex items-center';

        var svg = document.createElementNS(SVG_NS, 'svg');
        svg.setAttribute('class', 'w-4 h-4 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300');
        svg.setAttribute('fill', 'currentColor');
        svg.setAttribute('viewBox', '0 0 20 20');
        svg.setAttribute('aria-hidden', 'true');
        var path = document.createElementNS(SVG_NS, 'path');
        path.setAttribute('fill-rule', 'evenodd');
        path.setAttribute('clip-rule', 'evenodd');
        path.setAttribute('d', INFO_PATH);
        svg.appendChild(path);
        wrapper.appendChild(svg);

        var bubble = document.createElement('span');
        bubble.className = 'invisible group-hover:visible absolute left-0 top-6 w-64 p-3 bg-gray-900 dark:bg-gray-800 text-white text-xs rounded-lg shadow-lg z-50 normal-case tracking-normal font-normal';
        // textContent — escapes everything; user-supplied text can't
        // introduce HTML into the bubble.
        bubble.textContent = text == null ? '' : String(text);
        var arrow = document.createElement('span');
        arrow.className = 'absolute -top-1 left-2 w-2 h-2 bg-gray-900 dark:bg-gray-800 transform rotate-45';
        bubble.appendChild(arrow);
        wrapper.appendChild(bubble);

        return wrapper;
    }

    function appendInfoTooltipTo(parent, text) {
        if (!parent) return null;
        var node = createInfoTooltip(text);
        parent.appendChild(node);
        return node;
    }

    window.createInfoTooltip = createInfoTooltip;
    window.appendInfoTooltipTo = appendInfoTooltipTo;
})();
