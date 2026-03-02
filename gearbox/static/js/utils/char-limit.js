/**
 * Character limit utility for input fields and textareas.
 *
 * Usage:
 *   Static (template): Add data-charlimit="255" to any <input> or <textarea>.
 *   Dynamic (JS):      Call initCharLimit(element) after creating the element.
 *
 * A small counter ("200 of 255") appears when the user is within 20% of the limit.
 * The counter turns red when at the limit.
 */

function initCharLimit(el) {
    const limit = parseInt(el.getAttribute('data-charlimit'), 10);
    if (!limit || el._charLimitInit) return;
    el._charLimitInit = true;

    el.maxLength = limit;

    // Create the counter element
    const counter = document.createElement('span');
    counter.className = 'char-limit-counter text-xs text-gray-400 dark:text-gray-500 absolute right-2 bottom-1 pointer-events-none select-none hidden';
    counter.style.lineHeight = '1';

    // Ensure the parent has relative positioning for the counter
    const parent = el.parentElement;
    if (parent && getComputedStyle(parent).position === 'static') {
        parent.style.position = 'relative';
    }
    // Insert counter after the input
    el.insertAdjacentElement('afterend', counter);

    function update() {
        const len = el.value.length;
        const threshold = Math.floor(limit * 0.8);

        if (len >= threshold) {
            counter.textContent = len + ' of ' + limit;
            counter.classList.remove('hidden');
            if (len >= limit) {
                counter.classList.remove('text-gray-400', 'dark:text-gray-500');
                counter.classList.add('text-red-500', 'dark:text-red-400');
            } else {
                counter.classList.remove('text-red-500', 'dark:text-red-400');
                counter.classList.add('text-gray-400', 'dark:text-gray-500');
            }
        } else {
            counter.classList.add('hidden');
        }
    }

    el.addEventListener('input', update);
    el.addEventListener('focus', update);
    // Run once in case the field is pre-filled
    update();
}

function initAllCharLimits() {
    document.querySelectorAll('[data-charlimit]').forEach(initCharLimit);
}

document.addEventListener('DOMContentLoaded', initAllCharLimits);
