import { setupPopovers } from './popover.js';
import { setupMasonries } from './masonry.js';
import { throttledDebounce, isElementVisible, openURLInNewTab } from './utils.js';
import { elem, find, findAll } from './templating.js';

async function fetchPageContent(pageData) {
    // TODO: handle non 200 status codes/time outs
    // TODO: add retries
    const response = await fetch(`${pageData.baseURL}/api/pages/${pageData.slug}/content/`);
    const content = await response.text();

    return content;
}

function setupCarousels() {
    const carouselElements = document.getElementsByClassName("carousel-container");

    if (carouselElements.length == 0) {
        return;
    }

    for (let i = 0; i < carouselElements.length; i++) {
        const carousel = carouselElements[i];

        if (carousel.dataset.initialized) continue;
        carousel.dataset.initialized = "true";

        carousel.classList.add("show-right-cutoff");
        const itemsContainer = carousel.getElementsByClassName("carousel-items-container")[0];

        const determineSideCutoffs = () => {
            if (itemsContainer.scrollLeft != 0) {
                carousel.classList.add("show-left-cutoff");
            } else {
                carousel.classList.remove("show-left-cutoff");
            }

            if (Math.ceil(itemsContainer.scrollLeft) + itemsContainer.clientWidth < itemsContainer.scrollWidth) {
                carousel.classList.add("show-right-cutoff");
            } else {
                carousel.classList.remove("show-right-cutoff");
            }
        }

        const determineSideCutoffsRateLimited = throttledDebounce(determineSideCutoffs, 20, 100);

        itemsContainer.addEventListener("scroll", determineSideCutoffsRateLimited);
        window.addEventListener("resize", determineSideCutoffsRateLimited);

        afterContentReady(determineSideCutoffs);
    }
}

const minuteInSeconds = 60;
const hourInSeconds = minuteInSeconds * 60;
const dayInSeconds = hourInSeconds * 24;
const monthInSeconds = dayInSeconds * 30.4;
const yearInSeconds = dayInSeconds * 365;

function timestampToRelativeTime(timestamp) {
    let delta = Math.round((Date.now() / 1000) - timestamp);
    let prefix = "";

    if (delta < 0) {
        delta = -delta;
        prefix = "in ";
    }

    if (delta < minuteInSeconds) {
        return prefix + "1m";
    }
    if (delta < hourInSeconds) {
        return prefix + Math.floor(delta / minuteInSeconds) + "m";
    }
    if (delta < dayInSeconds) {
        return prefix + Math.floor(delta / hourInSeconds) + "h";
    }
    if (delta < monthInSeconds) {
        return prefix + Math.floor(delta / dayInSeconds) + "d";
    }
    if (delta < yearInSeconds) {
        return prefix + Math.floor(delta / monthInSeconds) + "mo";
    }

    return prefix + Math.floor(delta / yearInSeconds) + "y";
}

function updateRelativeTimeForElements(elements)
{
    for (let i = 0; i < elements.length; i++)
    {
        const element = elements[i];
        const timestamp = element.dataset.dynamicRelativeTime;

        if (timestamp === undefined)
            continue

        element.textContent = timestampToRelativeTime(timestamp);
    }
}

function setupSearchBoxes() {
    const searchWidgets = document.getElementsByClassName("search");

    if (searchWidgets.length == 0) {
        return;
    }

    for (let i = 0; i < searchWidgets.length; i++) {
        const widget = searchWidgets[i];
        const defaultSearchUrl = widget.dataset.defaultSearchUrl;
        const target = widget.dataset.target || "_blank";
        const newTab = widget.dataset.newTab === "true";
        const inputElement = widget.getElementsByClassName("search-input")[0];
        const bangElement = widget.getElementsByClassName("search-bang")[0];
        const bangs = widget.querySelectorAll(".search-bangs > input");
        const bangsMap = {};
        const kbdElement = widget.getElementsByTagName("kbd")[0];
        let currentBang = null;
        let lastQuery = "";

        for (let j = 0; j < bangs.length; j++) {
            const bang = bangs[j];
            bangsMap[bang.dataset.shortcut] = bang;
        }

        const handleKeyDown = (event) => {
            if (event.key == "Escape") {
                inputElement.blur();
                return;
            }

            if (event.key == "Enter") {
                const input = inputElement.value.trim();
                let query;
                let searchUrlTemplate;

                if (currentBang != null) {
                    query = input.slice(currentBang.dataset.shortcut.length + 1);
                    searchUrlTemplate = currentBang.dataset.url;
                } else {
                    query = input;
                    searchUrlTemplate = defaultSearchUrl;
                }
                if (query.length == 0 && currentBang == null) {
                    return;
                }

                const url = searchUrlTemplate.replace("!QUERY!", encodeURIComponent(query));

                if (newTab && !event.ctrlKey || !newTab && event.ctrlKey) {
                    window.open(url, target).focus();
                } else {
                    window.location.href = url;
                }

                lastQuery = query;
                inputElement.value = "";

                return;
            }

            if (event.key == "ArrowUp" && lastQuery.length > 0) {
                inputElement.value = lastQuery;
                return;
            }
        };

        const changeCurrentBang = (bang) => {
            currentBang = bang;
            bangElement.textContent = bang != null ? bang.dataset.title : "";
        }

        const handleInput = (event) => {
            const value = event.target.value.trim();
            if (value in bangsMap) {
                changeCurrentBang(bangsMap[value]);
                return;
            }

            const words = value.split(" ");
            if (words.length >= 2 && words[0] in bangsMap) {
                changeCurrentBang(bangsMap[words[0]]);
                return;
            }

            changeCurrentBang(null);
        };

        inputElement.addEventListener("focus", () => {
            document.addEventListener("keydown", handleKeyDown);
            document.addEventListener("input", handleInput);
        });
        inputElement.addEventListener("blur", () => {
            document.removeEventListener("keydown", handleKeyDown);
            document.removeEventListener("input", handleInput);
        });

        document.addEventListener("keydown", (event) => {
            if (['INPUT', 'TEXTAREA'].includes(document.activeElement.tagName)) return;
            if (event.code != "KeyS") return;

            inputElement.focus();
            event.preventDefault();
        });

        kbdElement.addEventListener("mousedown", () => {
            requestAnimationFrame(() => inputElement.focus());
        });
    }
}

function setupDynamicRelativeTime() {
    const updateInterval = 60 * 1000;
    let lastUpdateTime = Date.now();

    updateRelativeTimeForElements(document.querySelectorAll("[data-dynamic-relative-time]"));

    const updateElementsAndTimestamp = () => {
        updateRelativeTimeForElements(document.querySelectorAll("[data-dynamic-relative-time]"));
        lastUpdateTime = Date.now();
    };

    const scheduleRepeatingUpdate = () => setInterval(updateElementsAndTimestamp, updateInterval);

    if (document.hidden === undefined) {
        scheduleRepeatingUpdate();
        return;
    }

    let timeout = scheduleRepeatingUpdate();

    document.addEventListener("visibilitychange", () => {
        if (document.hidden) {
            clearTimeout(timeout);
            return;
        }

        const delta = Date.now() - lastUpdateTime;

        if (delta >= updateInterval) {
            updateElementsAndTimestamp();
            timeout = scheduleRepeatingUpdate();
            return;
        }

        timeout = setTimeout(() => {
            updateElementsAndTimestamp();
            timeout = scheduleRepeatingUpdate();
        }, updateInterval - delta);
    });
}

function setupGroups() {
    const groups = document.getElementsByClassName("widget-type-group");

    if (groups.length == 0) {
        return;
    }

    for (let g = 0; g < groups.length; g++) {
        const group = groups[g];

        if (group.dataset.initialized) continue;
        group.dataset.initialized = "true";

        const titles = group.getElementsByClassName("widget-header")[0].children;
        const tabs = group.getElementsByClassName("widget-group-contents")[0].children;
        let current = 0;

        for (let t = 0; t < titles.length; t++) {
            const title = titles[t];

            if (title.dataset.titleUrl !== undefined) {
                title.addEventListener("mousedown", (event) => {
                    if (event.button != 1) {
                        return;
                    }

                    openURLInNewTab(title.dataset.titleUrl, false);
                    event.preventDefault();
                });
            }

            title.addEventListener("click", () => {
                if (t == current) {
                    if (title.dataset.titleUrl !== undefined) {
                        openURLInNewTab(title.dataset.titleUrl);
                    }

                    return;
                }

                for (let i = 0; i < titles.length; i++) {
                    titles[i].classList.remove("widget-group-title-current");
                    titles[i].setAttribute("aria-selected", "false");
                    tabs[i].classList.remove("widget-group-content-current");
                    tabs[i].setAttribute("aria-hidden", "true");
                }

                if (current < t) {
                    tabs[t].dataset.direction = "right";
                } else {
                    tabs[t].dataset.direction = "left";
                }

                current = t;

                title.classList.add("widget-group-title-current");
                title.setAttribute("aria-selected", "true");
                tabs[t].classList.add("widget-group-content-current");
                tabs[t].setAttribute("aria-hidden", "false");
            });
        }
    }
}

function setupLazyImages() {
    const images = document.querySelectorAll("img[loading=lazy]");

    if (images.length == 0) {
        return;
    }

    function imageFinishedTransition(image) {
        image.classList.add("finished-transition");
    }

    const processImages = () => {
        setTimeout(() => {
            for (let i = 0; i < images.length; i++) {
                const image = images[i];

                if (image.dataset.lazyInitialized) continue;
                image.dataset.lazyInitialized = "true";

                if (image.complete) {
                    image.classList.add("cached");
                    setTimeout(() => imageFinishedTransition(image), 1);
                } else {
                    // TODO: also handle error event
                    image.addEventListener("load", () => {
                        image.classList.add("loaded");
                        setTimeout(() => imageFinishedTransition(image), 400);
                    });
                }
            }
        }, 1);
    };

    if (pageSetupComplete) {
        processImages();
    } else {
        afterContentReady(processImages);
    }
}

function getExpandedCollapsibleIndices(element) {
    const allContainers = [...element.querySelectorAll('.collapsible-container')];
    return allContainers
        .map((c, i) => c.classList.contains('container-expanded') ? i : -1)
        .filter(i => i !== -1);
}

function restoreExpandedCollapsibles(element, expandedIndices) {
    if (!expandedIndices.length) return;
    const allContainers = [...element.querySelectorAll('.collapsible-container')];
    for (const index of expandedIndices) {
        const container = allContainers[index];
        if (!container) continue;
        const button = container.nextElementSibling;
        if (button && button.classList.contains('expand-toggle-button')) {
            container.classList.add('no-reveal-animation');
            button.click();
        }
    }
}

function attachExpandToggleButton(collapsibleContainer) {
    const showMoreText = "Show more";
    const showLessText = "Show less";

    let expanded = false;
    const button = document.createElement("button");
    const icon = document.createElement("span");
    icon.classList.add("expand-toggle-button-icon");
    const textNode = document.createTextNode(showMoreText);
    button.classList.add("expand-toggle-button");
    button.append(textNode, icon);
    button.addEventListener("click", () => {
        expanded = !expanded;

        if (expanded) {
            collapsibleContainer.classList.add("container-expanded");
            button.classList.add("container-expanded");
            textNode.nodeValue = showLessText;
            return;
        }

        const topBefore = button.getClientRects()[0].top;

        collapsibleContainer.classList.remove("no-reveal-animation");
        collapsibleContainer.classList.remove("container-expanded");
        button.classList.remove("container-expanded");
        textNode.nodeValue = showMoreText;

        const topAfter = button.getClientRects()[0].top;

        if (topAfter > 0)
            return;

        window.scrollBy({
            top: topAfter - topBefore,
            behavior: "instant"
        });
    });

    collapsibleContainer.after(button);

    return button;
};


function setupCollapsibleLists() {
    const collapsibleLists = document.querySelectorAll(".list.collapsible-container");

    if (collapsibleLists.length == 0) {
        return;
    }

    for (let i = 0; i < collapsibleLists.length; i++) {
        const list = collapsibleLists[i];

        if (list.dataset.collapseAfter === undefined) {
            continue;
        }

        const collapseAfter = parseInt(list.dataset.collapseAfter);

        if (collapseAfter == -1) {
            continue;
        }

        if (list.children.length <= collapseAfter) {
            continue;
        }

        if (list.nextElementSibling && list.nextElementSibling.classList.contains("expand-toggle-button")) continue;

        attachExpandToggleButton(list);

        for (let c = collapseAfter; c < list.children.length; c++) {
            const child = list.children[c];
            child.classList.add("collapsible-item");
            child.style.animationDelay = ((c - collapseAfter) * 20).toString() + "ms";
        }
    }
}

function setupCollapsibleGrids() {
    const collapsibleGridElements = document.querySelectorAll(".cards-grid.collapsible-container");

    if (collapsibleGridElements.length == 0) {
        return;
    }

    for (let i = 0; i < collapsibleGridElements.length; i++) {
        const gridElement = collapsibleGridElements[i];

        if (gridElement.dataset.collapseAfterRows === undefined) {
            continue;
        }

        const collapseAfterRows = parseInt(gridElement.dataset.collapseAfterRows);

        if (collapseAfterRows == -1) {
            continue;
        }

        if (gridElement.nextElementSibling && gridElement.nextElementSibling.classList.contains("expand-toggle-button")) continue;

        const getCardsPerRow = () => {
            return parseInt(getComputedStyle(gridElement).getPropertyValue('--cards-per-row'));
        };

        const button = attachExpandToggleButton(gridElement);

        let cardsPerRow;

        const resolveCollapsibleItems = () => requestAnimationFrame(() => {
            const hideItemsAfterIndex = cardsPerRow * collapseAfterRows;

            if (hideItemsAfterIndex >= gridElement.children.length) {
                button.style.display = "none";
            } else {
                button.style.removeProperty("display");
            }

            let row = 0;

            for (let i = 0; i < gridElement.children.length; i++) {
                const child = gridElement.children[i];

                if (i >= hideItemsAfterIndex) {
                    child.classList.add("collapsible-item");
                    child.style.animationDelay = (row * 40).toString() + "ms";

                    if (i % cardsPerRow + 1 == cardsPerRow) {
                        row++;
                    }
                } else {
                    child.classList.remove("collapsible-item");
                    child.style.removeProperty("animation-delay");
                }
            }
        });

        const observer = new ResizeObserver(() => {
            if (!isElementVisible(gridElement)) {
                return;
            }

            const newCardsPerRow = getCardsPerRow();

            if (cardsPerRow == newCardsPerRow) {
                return;
            }

            cardsPerRow = newCardsPerRow;
            resolveCollapsibleItems();
        });

        afterContentReady(() => observer.observe(gridElement));
    }
}

const contentReadyCallbacks = [];
let pageSetupComplete = false;

function afterContentReady(callback) {
    contentReadyCallbacks.push(callback);
}

const weekDayNames = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const monthNames = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];

function makeSettableTimeElement(element, hourFormat) {
    const fragment = document.createDocumentFragment();
    const hour = document.createElement('span');
    const minute = document.createElement('span');
    const amPm = document.createElement('span');
    fragment.append(hour, document.createTextNode(':'), minute);

    if (hourFormat == '12h') {
        fragment.append(document.createTextNode(' '), amPm);
    }

    element.append(fragment);

    return (date) => {
        const hours = date.getHours();

        if (hourFormat == '12h') {
            amPm.textContent = hours < 12 ? 'AM' : 'PM';
            hour.textContent = hours % 12 || 12;
        } else {
            hour.textContent = hours < 10 ? '0' + hours : hours;
        }

        const minutes = date.getMinutes();
        minute.textContent = minutes < 10 ? '0' + minutes : minutes;
    };
};

function timeInZone(now, zone) {
    let timeInZone;

    try {
        timeInZone = new Date(now.toLocaleString('en-US', { timeZone: zone }));
    } catch (e) {
        // TODO: indicate to the user that this is an invalid timezone
        console.error(e);
        timeInZone = now
    }

    const diffInMinutes = Math.round((timeInZone.getTime() - now.getTime()) / 1000 / 60);

    return { time: timeInZone, diffInMinutes: diffInMinutes };
}

function zoneDiffText(diffInMinutes) {
    if (diffInMinutes == 0) {
        return "";
    }

    const sign = diffInMinutes < 0 ? "-" : "+";
    const signText = diffInMinutes < 0 ? "behind" : "ahead";

    diffInMinutes = Math.abs(diffInMinutes);

    const hours = Math.floor(diffInMinutes / 60);
    const minutes = diffInMinutes % 60;
    const hourSuffix = hours == 1 ? "" : "s";

    if (minutes == 0) {
        return { text: `${sign}${hours}h`, title: `${hours} hour${hourSuffix} ${signText}` };
    }

    if (hours == 0) {
        return { text: `${sign}${minutes}m`, title: `${minutes} minutes ${signText}` };
    }

    return { text: `${sign}${hours}h~`, title: `${hours} hour${hourSuffix} and ${minutes} minutes ${signText}` };
}

function setupClocks() {
    const clocks = document.getElementsByClassName('clock');

    if (clocks.length == 0) {
        return;
    }

    const updateCallbacks = [];

    for (var i = 0; i < clocks.length; i++) {
        const clock = clocks[i];
        const hourFormat = clock.dataset.hourFormat;
        const localTimeContainer = clock.querySelector('[data-local-time]');
        const localDateElement = localTimeContainer.querySelector('[data-date]');
        const localWeekdayElement = localTimeContainer.querySelector('[data-weekday]');
        const localYearElement = localTimeContainer.querySelector('[data-year]');
        const timeZoneContainers = clock.querySelectorAll('[data-time-in-zone]');

        const setLocalTime = makeSettableTimeElement(
            localTimeContainer.querySelector('[data-time]'),
            hourFormat
        );

        updateCallbacks.push((now) => {
            setLocalTime(now);
            localDateElement.textContent = now.getDate() + ' ' + monthNames[now.getMonth()];
            localWeekdayElement.textContent = weekDayNames[now.getDay()];
            localYearElement.textContent = now.getFullYear();
        });

        for (var z = 0; z < timeZoneContainers.length; z++) {
            const timeZoneContainer = timeZoneContainers[z];
            const diffElement = timeZoneContainer.querySelector('[data-time-diff]');

            const setZoneTime = makeSettableTimeElement(
                timeZoneContainer.querySelector('[data-time]'),
                hourFormat
            );

            updateCallbacks.push((now) => {
                const { time, diffInMinutes } = timeInZone(now, timeZoneContainer.dataset.timeInZone);
                setZoneTime(time);
                const { text, title } = zoneDiffText(diffInMinutes);
                diffElement.textContent = text;
                diffElement.title = title;
            });
        }
    }

    const updateClocks = () => {
        const now = new Date();

        for (var i = 0; i < updateCallbacks.length; i++)
            updateCallbacks[i](now);

        setTimeout(updateClocks, (60 - now.getSeconds()) * 1000);
    };

    updateClocks();
}

async function setupCalendars() {
    const elems = document.getElementsByClassName("calendar");
    if (elems.length == 0) return;

    // TODO: implement prefetching, currently loads as a nasty waterfall of requests
    const calendar = await import ('./calendar.js');

    for (let i = 0; i < elems.length; i++)
        calendar.default(elems[i]);
}

async function setupTodos() {
    const elems = Array.from(document.getElementsByClassName("todo"));
    if (elems.length == 0) return;

    const todo = await import ('./todo.js');

    for (let i = 0; i < elems.length; i++){
        todo.default(elems[i]);
    }
}

function setupTruncatedElementTitles() {
    const elements = document.querySelectorAll(".text-truncate, .single-line-titles .title, .text-truncate-2-lines, .text-truncate-3-lines");

    if (elements.length == 0) {
        return;
    }

    for (let i = 0; i < elements.length; i++) {
        const element = elements[i];
        if (element.getAttribute("title") === null)
            element.title = element.innerText.trim().replace(/\s+/g, " ");
    }
}

async function changeTheme(key, onChanged) {
    const themeStyleElem = find("#theme-style");

    const response = await fetch(`${pageData.baseURL}/api/set-theme/${key}`, {
        method: "POST",
    });

    if (response.status != 200) {
        alert("Failed to set theme: " + response.statusText);
        return;
    }
    const newThemeStyle = await response.text();

    const tempStyle = elem("style")
        .html("* { transition: none !important; }")
        .appendTo(document.head);

    themeStyleElem.html(newThemeStyle);
    document.documentElement.setAttribute("data-theme", key);
    document.documentElement.setAttribute("data-scheme", response.headers.get("X-Scheme"));
    typeof onChanged == "function" && onChanged();
    setTimeout(() => { tempStyle.remove(); }, 10);
}

function initThemePicker() {
    const themeChoicesInMobileNav = find(".mobile-navigation .theme-choices");
    if (!themeChoicesInMobileNav) return;

    const themeChoicesInHeader = find(".header-container .theme-choices");

    if (themeChoicesInHeader) {
        themeChoicesInHeader.replaceWith(
            themeChoicesInMobileNav.cloneNode(true)
        );
    }

    const presetElems = findAll(".theme-choices .theme-preset");
    let themePreviewElems = document.getElementsByClassName("current-theme-preview");
    let isLoading = false;

    presetElems.forEach((presetElement) => {
        const themeKey = presetElement.dataset.key;

        if (themeKey === undefined) {
            return;
        }

        if (themeKey == pageData.theme) {
            presetElement.classList.add("current");
        }

        presetElement.addEventListener("click", () => {
            if (themeKey == pageData.theme) return;
            if (isLoading) return;

            isLoading = true;
            changeTheme(themeKey, function() {
                isLoading = false;
                pageData.theme = themeKey;
                presetElems.forEach((e) => { e.classList.remove("current"); });

                Array.from(themePreviewElems).forEach((preview) => {
                    preview.querySelector(".theme-preset").replaceWith(
                        presetElement.cloneNode(true)
                    );
                })

                presetElems.forEach((e) => {
                    if (e.dataset.key != themeKey) return;
                    e.classList.add("current");
                });
            });
        });
    })
}

async function setupPage() {
    initThemePicker();

    const pageElement = document.getElementById("page");
    const pageContentElement = document.getElementById("page-content");
    const pageContent = await fetchPageContent(pageData);

    pageContentElement.innerHTML = pageContent;
    htmx.process(pageContentElement);

    try {
        setupPopovers();
        setupClocks()
        await setupCalendars();
        await setupTodos();
        setupCarousels();
        setupSearchBoxes();
        setupCollapsibleLists();
        setupCollapsibleGrids();
        setupGroups();
        setupMasonries();
        setupDynamicRelativeTime();
        setupLazyImages();
        setupPlayingProgressUpdater();
    } finally {
        pageElement.classList.add("content-ready");
        pageElement.setAttribute("aria-busy", "false");

        for (let i = 0; i < contentReadyCallbacks.length; i++) {
            contentReadyCallbacks[i]();
        }

        pageSetupComplete = true;

        setTimeout(() => {
            setupTruncatedElementTitles();
        }, 50);

        setTimeout(() => {
            document.body.classList.add("page-columns-transitioned");
        }, 300);
    }
}

function nowMs() {
    return Date.now();
}



// Local playing progress updaters keyed by widget element
const playingUpdaters = new Map();

function clearPlayingUpdater(widget) {
    const state = playingUpdaters.get(widget);
    if (!state) return;
    if (state.intervalId != null) {
        clearInterval(state.intervalId);
    }
    playingUpdaters.delete(widget);
}

function formatDurationMs(ms) {
    ms = Math.max(0, Math.floor(ms));
    const totalSeconds = Math.floor(ms / 1000);
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;

    if (hours >= 1) {
        return `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
    }

    return `${minutes}:${String(seconds).padStart(2, '0')}`;
}

// Wall-clock state for each session, keyed by data-session-key.
// Survives DOM replacements so the clock never jumps on a widget refresh.
const playingSessionStates = new Map();

function _playingEstimate(state) {
    if (!state.isPlaying) return state.anchorOffset;
    return state.anchorOffset + (Date.now() - state.anchorTime);
}

function _playingRender(sess, offsetMs, durationMs) {
    const pct = durationMs > 0 ? Math.min(100, (offsetMs / durationMs) * 100) : 0;
    const fill = sess.querySelector('.playing-progress-fill');
    if (fill) fill.style.width = pct + '%';
    sess.querySelectorAll('.playing-time-pos').forEach(el => {
        el.textContent = formatDurationMs(offsetMs);
    });
}

// Called on every setupPlayingProgressUpdater invocation (initial load + after each widget refresh).
// Re-anchors the clock only when the server reports a meaningful change.
function _playingSyncWidget(widget) {
    const DRIFT_TOLERANCE_MS = 3000;
    const sessions = widget.querySelectorAll('.playing-session[data-duration][data-offset]');
    sessions.forEach((sess, idx) => {
        const duration = Number(sess.dataset.duration || 0);
        if (!duration) return;

        const serverOffset = Number(sess.dataset.offset || 0);
        const isPlaying = sess.dataset.playing === 'true';
        // Use session-key when available; fall back to widget-id + position index.
        const key = sess.dataset.sessionKey || (widget.dataset.widgetId + ':' + idx);

        let state = playingSessionStates.get(key);
        if (state) {
            const estimated = _playingEstimate(state);
            const drift = Math.abs(serverOffset - estimated);
            const stateChanged = state.isPlaying !== isPlaying;
            if (stateChanged || drift > DRIFT_TOLERANCE_MS) {
                // Playback state flipped or we drifted too far — re-anchor to server value.
                state.anchorOffset = serverOffset;
                state.anchorTime = Date.now();
                state.isPlaying = isPlaying;
            }
            // else: clock is running fine, leave it alone (no jump).
        } else {
            state = { anchorOffset: serverOffset, anchorTime: Date.now(), isPlaying };
            playingSessionStates.set(key, state);
        }

        _playingRender(sess, Math.min(_playingEstimate(state), duration), duration);
    });
}

// Called every second by the per-widget interval.
function _playingTickWidget(widget) {
    const sessions = widget.querySelectorAll('.playing-session[data-duration][data-offset]');
    sessions.forEach((sess, idx) => {
        const duration = Number(sess.dataset.duration || 0);
        if (!duration) return;
        const key = sess.dataset.sessionKey || (widget.dataset.widgetId + ':' + idx);
        const state = playingSessionStates.get(key);
        if (!state) return;
        _playingRender(sess, Math.min(_playingEstimate(state), duration), duration);
    });
}

function setupPlayingProgressUpdater() {
    const widgets = document.querySelectorAll('.widget[data-update-interval]');
    const seen = new Set();

    widgets.forEach(widget => {
        seen.add(widget);

        // Always sync so that paused/resumed/seeked state is picked up from the latest DOM.
        _playingSyncWidget(widget);

        if (playingUpdaters.has(widget)) return;

        // First time seeing this widget element — start the 1 s tick.
        const intervalId = setInterval(() => _playingTickWidget(widget), 1000);
        playingUpdaters.set(widget, { intervalId });
    });

    // Tear down intervals for widget elements that are no longer in the DOM.
    Array.from(playingUpdaters.keys()).forEach(widget => {
        if (!seen.has(widget)) clearPlayingUpdater(widget);
    });

    setupPlayingThumbnailCropping();
}

function setupPlayingThumbnailCropping() {
    const imgs = document.querySelectorAll('.playing-thumbnail-img');

    imgs.forEach(img => {
        const container = img.closest('.playing-thumbnail');
        const apply = () => {
            // Guard in case image has no natural sizes
            if (!img.naturalWidth || !img.naturalHeight) {
                // keep default (contain)
                img.classList.remove('playing-crop');
                if (container) container.classList.remove('playing-portrait');
                return;
            }

            if (img.naturalWidth > img.naturalHeight) {
                img.classList.add('playing-crop');
                if (container) container.classList.remove('playing-portrait');
            } else if (img.naturalHeight > img.naturalWidth) {
                // portrait: make container taller and remove background
                img.classList.remove('playing-crop');
                if (container) container.classList.add('playing-portrait');
            } else {
                // square
                img.classList.remove('playing-crop');
                if (container) container.classList.remove('playing-portrait');
            }
        };

        if (img.complete) {
            apply();
        } else {
            img.addEventListener('load', apply, { once: true });
            img.addEventListener('error', () => {
                img.classList.remove('playing-crop');
                if (container) container.classList.remove('playing-portrait');
            }, { once: true });
        }
    });
}

window.dynacatRefreshWidget = function(widgetId) {
    const widget = document.querySelector(`.widget[data-widget-id="${widgetId}"]`);
    if (widget) htmx.trigger(widget, 'refresh');
};

window.dynacatSetupPopovers = setupPopovers;

setupPage();

// Pause HTMX polls when tab is hidden
document.body.addEventListener('htmx:beforeRequest', function(event) {
    if (document.hidden) event.preventDefault();
});

// Save collapsible state before idiomorph morphs a widget.
document.body.addEventListener('htmx:beforeSwap', function(event) {
    const target = event.detail.target;
    if (!target?.classList?.contains('widget')) return;
    target._expandedCollapsibleIndices = getExpandedCollapsibleIndices(target);
});

// Restore collapsible state immediately after the swap, in the same synchronous
// JS task as the morph. The browser only paints between tasks, so it never sees
// the intermediate collapsed state — no flicker, no scroll jump.
document.body.addEventListener('htmx:afterSwap', function(event) {
    let target = event.detail.target;
    if (!target?.classList?.contains('widget')) return;

    // Always re-attach toggle buttons after morph (they aren't in server HTML).
    setupCollapsibleLists();
    setupCollapsibleGrids();

    let indices = target._expandedCollapsibleIndices;
    if (!indices?.length) return;

    // Disable scroll-anchor to prevent browser from scrolling when widget height changes.
    const htmlElem = document.documentElement;
    const prevAnchor = htmlElem.style.overflowAnchor;
    htmlElem.style.overflowAnchor = 'none';

    try {
        // After outerHTML morph, the target reference may point to a detached old element.
        // Re-find the widget in the DOM by ID to ensure we operate on the live element.
        const widgetId = target.dataset.widgetId;
        if (widgetId) {
            const liveTarget = document.querySelector(`.widget[data-widget-id="${widgetId}"]`);
            if (liveTarget) {
                target = liveTarget;
                target._expandedCollapsibleIndices = indices;
            }
        }

        restoreExpandedCollapsibles(target, indices);
    } finally {
        htmlElem.style.overflowAnchor = prevAnchor;
    }
});

// Re-run the remaining widget setup after morph settles.
document.body.addEventListener('htmx:afterSettle', function(event) {
    let target = event.detail.target;
    if (!target?.classList?.contains('widget')) return;

    // Disable scroll-anchor to prevent browser from scrolling when setup causes layout changes.
    const htmlElem = document.documentElement;
    const prevAnchor = htmlElem.style.overflowAnchor;
    htmlElem.style.overflowAnchor = 'none';

    try {
        // Re-find widget by ID (same reason as in htmx:afterSwap).
        const widgetId = target.dataset.widgetId;
        if (widgetId) {
            const liveTarget = document.querySelector(`.widget[data-widget-id="${widgetId}"]`);
            if (liveTarget) target = liveTarget;
        }

        setupPopovers();
        setupCarousels();
        setupGroups();
        setupMasonries();
        setupLazyImages();
        setupTruncatedElementTitles();
        setupPlayingProgressUpdater();
        setupPlayingThumbnailCropping();

        delete target._expandedCollapsibleIndices;
    } finally {
        htmlElem.style.overflowAnchor = prevAnchor;
    }
});
