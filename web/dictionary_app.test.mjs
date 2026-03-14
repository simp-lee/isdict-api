import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import vm from 'node:vm';

const htmlPath = path.resolve('web/index.html');
const htmlSource = readFileSync(htmlPath, 'utf8');
const appSource = extractDictionaryAppScript(htmlSource);

function extractDictionaryAppScript(source) {
    const inlineScripts = Array.from(source.matchAll(/<script\b(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/gi));
    const scriptMatch = inlineScripts.find((match) => match[1].includes('function dictionaryApp()'));

    if (!scriptMatch) {
        throw new Error('dictionaryApp() script not found in web/index.html');
    }

    return scriptMatch[1];
}

function createResponse(status, payload) {
    return {
        ok: status >= 200 && status < 300,
        status,
        async json() {
            return payload;
        }
    };
}

function createAbortError() {
    const error = new Error('The operation was aborted');
    error.name = 'AbortError';
    return error;
}

function createTimerHarness() {
    let nextId = 1;
    const timers = [];

    return {
        setTimeout(callback, delay = 0) {
            const timer = {
                id: nextId++,
                callback,
                delay,
                cleared: false
            };
            timers.push(timer);
            return timer.id;
        },
        clearTimeout(timerId) {
            const timer = timers.find((item) => item.id === timerId);
            if (timer) {
                timer.cleared = true;
            }
        },
        runNextTimer() {
            while (timers.length > 0) {
                const timer = timers.shift();
                if (timer.cleared) {
                    continue;
                }
                timer.callback();
                return true;
            }
            return false;
        },
        runAllTimers() {
            let ran = false;
            while (this.runNextTimer()) {
                ran = true;
            }
            return ran;
        }
    };
}

function createDeferredFetch() {
    let settle;
    let fail;
    const responsePromise = new Promise((resolve, reject) => {
        settle = resolve;
        fail = reject;
    });

    return {
        handler(_url, options = {}) {
            const { signal } = options;
            if (signal?.aborted) {
                return Promise.reject(createAbortError());
            }

            return new Promise((resolve, reject) => {
                let finished = false;
                const onAbort = () => {
                    if (finished) {
                        return;
                    }
                    finished = true;
                    signal?.removeEventListener('abort', onAbort);
                    reject(createAbortError());
                };

                signal?.addEventListener('abort', onAbort, { once: true });

                responsePromise.then(
                    (value) => {
                        if (finished) {
                            return;
                        }
                        finished = true;
                        signal?.removeEventListener('abort', onAbort);
                        resolve(value);
                    },
                    (error) => {
                        if (finished) {
                            return;
                        }
                        finished = true;
                        signal?.removeEventListener('abort', onAbort);
                        reject(error);
                    }
                );
            });
        },
        resolve(value) {
            settle(value);
        },
        reject(error) {
            fail(error);
        }
    };
}

async function flushAsyncWork() {
    await new Promise((resolve) => setImmediate(resolve));
    await Promise.resolve();
}

function createHarness(options = {}) {
    const listeners = new Map();
    const fetchCalls = [];
    const fetchRequests = [];
    const fetchQueue = [];
    const timers = createTimerHarness();
    const location = {
        pathname: options.pathname || '/',
        search: options.search || ''
    };

    const windowObject = {
        innerWidth: 1280,
        location,
        history: {
            pushState(_state, _title, url) {
                applyURL(location, url);
            }
        },
        addEventListener(eventName, handler) {
            listeners.set(eventName, handler);
        }
    };

    const sandbox = {
        AbortController,
        URLSearchParams,
        console,
        document: { title: '易思词典' },
        clearTimeout(timerId) {
            timers.clearTimeout(timerId);
        },
        setTimeout(callback, delay) {
            return timers.setTimeout(callback, delay);
        },
        window: windowObject,
        fetch: async (url, options = {}) => {
            fetchCalls.push(url);
            fetchRequests.push({ url, options });
            if (fetchQueue.length === 0) {
                throw new Error(`unexpected fetch: ${url}`);
            }
            const next = fetchQueue.shift();
            if (options.signal?.aborted) {
                throw createAbortError();
            }
            return typeof next === 'function' ? next(url, options) : next;
        }
    };
    sandbox.globalThis = sandbox;

    vm.createContext(sandbox);
    vm.runInContext(appSource, sandbox, { filename: 'web/index.html' });

    if (typeof sandbox.dictionaryApp !== 'function') {
        throw new Error('dictionaryApp() was not defined by extracted script');
    }

    const app = sandbox.dictionaryApp();

    return {
        app,
        document: sandbox.document,
        fetchCalls,
        fetchRequests,
        listeners,
        location,
        queueFetch(...responses) {
            fetchQueue.push(...responses);
        },
        runNextTimer() {
            return timers.runNextTimer();
        },
        runAllTimers() {
            return timers.runAllTimers();
        },
        dispatchPopstate() {
            const handler = listeners.get('popstate');
            if (!handler) {
                throw new Error('popstate handler was not registered');
            }
            handler();
        }
    };
}

function applyURL(location, url) {
    if (!url) {
        location.search = '';
        return;
    }

    const [pathname, search = ''] = url.split('?');
    location.pathname = pathname || location.pathname;
    location.search = search ? `?${search}` : '';
}

test('variant cache hits do not overwrite canonical headword cache entries', async () => {
    const { app } = createHarness();
    const canonicalWord = { headword: 'give', translation_zh: '给' };
    const variantWord = {
        headword: 'give',
        translation_zh: '给',
        queried_variant: { text: 'gave' }
    };

    app.wordCache[app.buildWordQueryCacheKey('gave')] = app.cloneData(variantWord);
    app.wordCache[app.buildHeadwordCacheKey('give')] = app.cloneData(canonicalWord);
    app.fetchRelatedPhrases = () => {};

    app.searchQuery = 'gave';
    await app.performSearch();

    assert.equal(
        JSON.stringify(app.wordCache[app.buildHeadwordCacheKey('give')]),
        JSON.stringify(canonicalWord)
    );
    assert.equal(app.currentWord.queried_variant.text, 'gave');

    app.searchQuery = 'give';
    await app.performSearch();

    assert.equal(app.currentWord.headword, 'give');
    assert.equal(app.currentWord.queried_variant, undefined);
});

test('exact-first cache keeps case-distinct headwords separate', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(200, {
            success: true,
            data: { headword: 'polish', translation_zh: '擦亮' }
        }),
        createResponse(200, {
            success: true,
            data: { headword: 'Polish', translation_zh: '波兰语' }
        })
    );

    app.fetchRelatedPhrases = () => {};

    app.searchQuery = 'polish';
    await app.performSearch();

    assert.equal(app.currentWord.headword, 'polish');

    app.searchQuery = 'Polish';
    await app.performSearch();

    assert.equal(app.currentWord.headword, 'Polish');
    assert.deepEqual(fetchCalls, ['/api/v1/words/polish', '/api/v1/words/Polish']);
    assert.equal(app.wordCache[app.buildWordQueryCacheKey('polish')].headword, 'polish');
    assert.equal(app.wordCache[app.buildWordQueryCacheKey('Polish')].headword, 'Polish');
    assert.equal(app.wordCache[app.buildHeadwordCacheKey('polish')].headword, 'polish');
    assert.equal(app.wordCache[app.buildHeadwordCacheKey('Polish')].headword, 'Polish');
});

test('variant candidate selected state keeps case-distinct headwords separate', () => {
    const { app } = createHarness();

    app.variantCandidates = [
        { headword: 'polish', translation_zh: '擦亮' },
        { headword: 'Polish', translation_zh: '波兰语' }
    ];

    app.currentWord = { headword: 'polish' };
    assert.equal(app.variantCandidates.filter((candidate) => app.isCurrentVariantCandidate(candidate)).length, 1);
    assert.equal(app.isCurrentVariantCandidate(app.variantCandidates[0]), true);
    assert.equal(app.isCurrentVariantCandidate(app.variantCandidates[1]), false);

    app.currentWord = { headword: 'Polish' };
    assert.equal(app.variantCandidates.filter((candidate) => app.isCurrentVariantCandidate(candidate)).length, 1);
    assert.equal(app.isCurrentVariantCandidate(app.variantCandidates[0]), false);
    assert.equal(app.isCurrentVariantCandidate(app.variantCandidates[1]), true);
});

test('external target blank links include noopener noreferrer', () => {
    const externalBlankLinks = Array.from(
        htmlSource.matchAll(/<a\b[^>]*href="https:\/\/[^\"]+"[^>]*target="_blank"[^>]*>/g)
    );
    const apiExampleLinks = [
        '/api/v1/words/example',
        '/api/v1/words/by-variant/lit',
        '/api/v1/phrases?q=take&limit=10',
        '/api/v1/search?q=test&limit=10',
        '/api/v1/suggest?prefix=exa&limit=10'
    ];

    assert.ok(externalBlankLinks.length > 0);
    externalBlankLinks.forEach((match) => {
        assert.match(match[0], /rel="noopener noreferrer"/);
    });

    apiExampleLinks.forEach((href) => {
        const escapedHref = href.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        const link = htmlSource.match(new RegExp(`<a\\b[^>]*href="${escapedHref}"[^>]*target="_blank"[^>]*>`));

        assert.ok(link, `expected API example link for ${href}`);
        assert.match(link[0], /rel="noopener noreferrer"/);
    });
});

test('phrase misses with 1-2 normalized characters stay not-found and do not fall back to /search', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(404, {}),
        createResponse(404, {}),
        createResponse(200, { success: true, data: [] })
    );

    app.searchQuery = 'a b';
    await app.performSearch();

    assert.equal(app.errorType, 'not-found');
    assert.equal(app.error, '未找到 "a b"');
    assert.equal(fetchCalls.some((url) => url.includes('/search?')), false);
});

test('phrase fuzzy matches stay as suggestions and do not auto-open a different phrase detail', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(404, {}),
        createResponse(404, {}),
        createResponse(200, {
            success: true,
            data: [{ headword: 'take off', translation_zh: '起飞' }]
        })
    );

    app.searchQuery = 'take off now';
    await app.performSearch();

    assert.equal(app.errorType, 'not-found');
    assert.equal(app.error, '未找到 "take off now"');
    assert.equal(app.currentWord, null);
    assert.equal(app.searchResults.length, 1);
    assert.equal(app.searchResults[0].headword, 'take off');
    assert.deepEqual(fetchCalls, [
        '/api/v1/words/take%20off%20now',
        '/api/v1/words/by-variant/take%20off%20now',
        '/api/v1/phrases?q=take%20off%20now&limit=10'
    ]);
});

test('separator-distinct phrase suggestions do not count as exact hits', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(404, {}),
        createResponse(404, {}),
        createResponse(200, {
            success: true,
            data: [{ headword: 'resign', translation_zh: '辞职' }]
        })
    );

    app.searchQuery = 're sign';
    await app.performSearch();

    assert.equal(app.errorType, 'not-found');
    assert.equal(app.error, '未找到 "re sign"');
    assert.equal(app.currentWord, null);
    assert.equal(app.searchResults.length, 1);
    assert.equal(app.searchResults[0].headword, 'resign');
    assert.deepEqual(fetchCalls, [
        '/api/v1/words/re%20sign',
        '/api/v1/words/by-variant/re%20sign',
        '/api/v1/phrases?q=re%20sign&limit=10'
    ]);
});

test('init and popstate keep query state aligned with the current URL', () => {
    const harness = createHarness({ search: '?word=study' });
    const { app, location } = harness;
    const performCalls = [];
    const resetCalls = [];

    app.performSearch = (options = {}) => {
        performCalls.push({ searchQuery: app.searchQuery, options });
    };
    app.resetToHome = (options = {}) => {
        resetCalls.push(options);
    };

    app.init();

    assert.equal(performCalls.length, 1);
    assert.equal(JSON.stringify(performCalls[0]), JSON.stringify({ searchQuery: 'study', options: { pushHistory: false } }));

    location.search = '';
    harness.dispatchPopstate();

    assert.equal(resetCalls.length, 1);
    assert.equal(JSON.stringify(resetCalls[0]), JSON.stringify({ pushHistory: false }));
});

test('related phrase cache keeps separator-distinct queries isolated', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(200, {
            success: true,
            data: [{ headword: 're-sign', translation_zh: '重新签署' }]
        }),
        createResponse(200, {
            success: true,
            data: [{ headword: 'resign', translation_zh: '辞职' }]
        })
    );

    await app.fetchRelatedPhrases('re-sign');
    assert.equal(fetchCalls.length, 1);
    assert.equal(app.relatedPhrases[0].headword, 're-sign');

    await app.fetchRelatedPhrases('resign');
    assert.equal(fetchCalls.length, 2);
    assert.equal(fetchCalls[0], '/api/v1/phrases?q=re-sign&limit=10');
    assert.equal(fetchCalls[1], '/api/v1/phrases?q=resign&limit=10');
    assert.equal(app.relatedPhrases[0].headword, 'resign');
});

test('debounced suggestion requests abort superseded fetches and keep the latest results', async () => {
    const { app, fetchCalls, fetchRequests, queueFetch, runNextTimer } = createHarness();
    const firstRequest = createDeferredFetch();
    const secondRequest = createDeferredFetch();

    queueFetch(firstRequest.handler, secondRequest.handler);

    app.searchQuery = 'stu';
    app.onSearchInput();
    assert.equal(fetchCalls.length, 0);

    runNextTimer();
    assert.deepEqual(fetchCalls, ['/api/v1/suggest?prefix=stu&limit=10']);
    assert.equal(fetchRequests[0].options.signal.aborted, false);

    app.searchQuery = 'stud';
    app.onSearchInput();
    runNextTimer();
    await flushAsyncWork();

    assert.equal(fetchCalls[1], '/api/v1/suggest?prefix=stud&limit=10');
    assert.equal(fetchRequests[0].options.signal.aborted, true);
    assert.equal(fetchRequests[1].options.signal.aborted, false);

    secondRequest.resolve(createResponse(200, {
        success: true,
        data: [{ headword: 'study', translation_zh: '学习' }]
    }));
    await flushAsyncWork();

    assert.equal(app.suggestions.length, 1);
    assert.equal(app.suggestions[0].headword, 'study');

    firstRequest.resolve(createResponse(200, {
        success: true,
        data: [{ headword: 'sturdy', translation_zh: '结实的' }]
    }));
    await flushAsyncWork();

    assert.equal(app.suggestions.length, 1);
    assert.equal(app.suggestions[0].headword, 'study');
});

test('aborted related phrase requests do not overwrite the active search results', async () => {
    const { app, fetchCalls, fetchRequests, queueFetch } = createHarness();
    const firstRequest = createDeferredFetch();
    const secondRequest = createDeferredFetch();

    queueFetch(firstRequest.handler, secondRequest.handler);

    app.activeSearchToken = 1;
    const firstPromise = app.fetchRelatedPhrases('take', app.activeSearchToken);
    assert.equal(fetchCalls[0], '/api/v1/phrases?q=take&limit=10');

    app.activeSearchToken = 2;
    const secondPromise = app.fetchRelatedPhrases('give', app.activeSearchToken);
    await flushAsyncWork();

    assert.equal(fetchCalls[1], '/api/v1/phrases?q=give&limit=10');
    assert.equal(fetchRequests[0].options.signal.aborted, true);

    secondRequest.resolve(createResponse(200, {
        success: true,
        data: [{ headword: 'give up', translation_zh: '放弃' }]
    }));
    await secondPromise;

    assert.equal(app.relatedPhrases.length, 1);
    assert.equal(app.relatedPhrases[0].headword, 'give up');

    firstRequest.resolve(createResponse(200, {
        success: true,
        data: [{ headword: 'take off', translation_zh: '起飞' }]
    }));
    await firstPromise;

    assert.equal(app.relatedPhrases.length, 1);
    assert.equal(app.relatedPhrases[0].headword, 'give up');
});

test('API endpoint counts are derived from a single catalog source', () => {
    const { app } = createHarness();

    assert.equal(app.apiEndpointCatalog.business.length, 8);
    assert.equal(app.apiEndpointCatalog.health.length, 2);
    assert.equal(app.apiBusinessEndpointCount, app.apiEndpointCatalog.business.length);
    assert.equal(app.apiHealthEndpointCount, app.apiEndpointCatalog.health.length);
});

test('phrase contract keeps 400 validation separate from not-found misses', async () => {
    const { app, queueFetch } = createHarness();
    const longPhrase = `${'a'.repeat(25)} ${'b'.repeat(25)} ${'c'}`;

    queueFetch(
        createResponse(404, {}),
        createResponse(404, {}),
        createResponse(400, {
            error: {
                message: 'Keyword must not exceed 50 characters'
            }
        })
    );

    app.searchQuery = longPhrase;
    await app.performSearch();

    assert.equal(app.errorType, 'validation');
    assert.equal(app.error, 'Keyword must not exceed 50 characters');
});

test('variant lookup errors stop fallback and surface the backend failure', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(404, {}),
        createResponse(500, {
            error: {
                message: 'variant backend unavailable'
            }
        })
    );

    app.searchQuery = 'gave';
    await app.performSearch();

    assert.equal(app.errorType, 'server');
    assert.equal(app.getErrorTitle(), '服务暂时不可用');
    assert.equal(app.error, 'variant backend unavailable');
    assert.equal(fetchCalls.some((url) => url.includes('/search?')), false);
});

test('successful variant lookup fetches full detail before reusing the canonical cache', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(404, {}),
        createResponse(200, {
            success: true,
            data: [{
                headword: 'give',
                translation_zh: '给',
                pronunciations: [{ accent: 'us', ipa: 'gɪv' }],
                senses: [{ definition_en: 'to hand something to someone' }],
                variant_info: [{ variant_text: 'gave', frequency_rank: 120, frequency_count: 10 }]
            }]
        }),
        createResponse(200, {
            success: true,
            data: {
                headword: 'give',
                translation_zh: '给',
                pronunciations: [{ accent: 'us', ipa: 'gɪv' }],
                senses: [{ definition_en: 'to hand something to someone' }],
                variants: [{ kind: 'form', form_type: 'past', variant_text: 'gave' }]
            }
        })
    );

    app.fetchRelatedPhrases = () => {};
    app.searchQuery = 'gave';
    await app.performSearch();

    assert.equal(app.currentWord.headword, 'give');
    assert.equal(app.currentWord.queried_variant.text, 'gave');
    assert.equal(app.currentWord.senses.length, 1);
    assert.equal(app.currentWord.variants.length, 1);
    assert.equal(app.variantCandidates.length, 0);
    assert.deepEqual(fetchCalls, ['/api/v1/words/gave', '/api/v1/words/by-variant/gave', '/api/v1/words/give']);

    app.searchQuery = 'give';
    await app.performSearch();

    assert.equal(app.currentWord.headword, 'give');
    assert.equal(app.currentWord.queried_variant, undefined);
    assert.equal(app.currentWord.variants.length, 1);
    assert.deepEqual(fetchCalls, ['/api/v1/words/gave', '/api/v1/words/by-variant/gave', '/api/v1/words/give']);
});

test('successful variant lookup keeps multiple candidates visible and fetches full detail for the selected headword', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(404, {}),
        createResponse(200, {
            success: true,
            data: [
                {
                    headword: 'axe',
                    translation_zh: '斧头',
                    senses: [{ definition_en: 'a tool for chopping wood' }],
                    variant_info: [{ variant_text: 'axes', frequency_rank: 200, frequency_count: 12 }]
                },
                {
                    headword: 'axis',
                    translation_zh: '轴',
                    senses: [{ definition_en: 'a fixed reference line' }],
                    variant_info: [{ variant_text: 'axes', frequency_rank: 150, frequency_count: 8 }]
                }
            ]
        }),
        createResponse(200, {
            success: true,
            data: {
                headword: 'axe',
                translation_zh: '斧头',
                senses: [{ definition_en: 'a tool for chopping wood' }],
                variants: [{ kind: 'form', form_type: 'plural', variant_text: 'axes' }]
            }
        }),
        createResponse(200, {
            success: true,
            data: {
                headword: 'axis',
                translation_zh: '轴',
                senses: [{ definition_en: 'a fixed reference line' }],
                variants: [{ kind: 'form', form_type: 'plural', variant_text: 'axes' }]
            }
        })
    );

    app.fetchRelatedPhrases = () => {};
    app.searchQuery = 'axes';
    await app.performSearch();

    assert.equal(app.currentWord.headword, 'axe');
    assert.equal(app.currentWord.queried_variant.text, 'axes');
    assert.equal(
        JSON.stringify(app.variantCandidates.map((candidate) => candidate.headword)),
        JSON.stringify(['axe', 'axis'])
    );
    assert.equal(app.currentWord.variants.length, 1);
    assert.deepEqual(fetchCalls, ['/api/v1/words/axes', '/api/v1/words/by-variant/axes', '/api/v1/words/axe']);

    await app.selectVariantCandidate(app.variantCandidates[1]);

    assert.equal(app.currentWord.headword, 'axis');
    assert.equal(app.currentWord.queried_variant.text, 'axes');
    assert.equal(app.currentWord.variants.length, 1);
    assert.deepEqual(fetchCalls, ['/api/v1/words/axes', '/api/v1/words/by-variant/axes', '/api/v1/words/axe', '/api/v1/words/axis']);
});

test('suggestion request failures clear stale suggestions for the active query', async () => {
    const { app, queueFetch } = createHarness();

    app.suggestions = [{ headword: 'study', translation_zh: '学习' }];
    app.searchQuery = 'studi';
    queueFetch(createResponse(500, {
        error: {
            message: 'suggest backend unavailable'
        }
    }));

    await app.fetchSuggestions('studi');

    assert.equal(app.suggestions.length, 0);
});

test('CEFR helpers normalize valid levels and reject invalid values', () => {
    const { app } = createHarness();

    assert.equal(app.normalizeCEFRLevel(' a1 '), 'A1');
    assert.equal(app.normalizeCEFRLevel('b2'), 'B2');
    assert.equal(app.normalizeCEFRLevel('C3'), '');
    assert.equal(app.normalizeCEFRLevel(2), '');
    assert.equal(app.hasCEFRLevel(' c1 '), true);
    assert.equal(app.hasCEFRLevel('advanced'), false);
    assert.equal(app.getCEFRLevelText(' c2 '), 'C2');
});

test('CEFR badge classes map normalized levels and fall back for invalid values', () => {
    const { app } = createHarness();

    assert.equal(app.getCEFRBadgeClass('a1'), 'bg-green-100 text-green-800');
    assert.equal(app.getCEFRBadgeClass(' A2 '), 'bg-green-200 text-green-900');
    assert.equal(app.getCEFRBadgeClass('b1'), 'bg-yellow-100 text-yellow-800');
    assert.equal(app.getCEFRBadgeClass('B2'), 'bg-orange-100 text-orange-800');
    assert.equal(app.getCEFRBadgeClass('c1'), 'bg-red-100 text-red-800');
    assert.equal(app.getCEFRBadgeClass('C2'), 'bg-purple-100 text-purple-800');
    assert.equal(app.getCEFRBadgeClass('unknown'), 'bg-slate-100 text-slate-600');
});

test('search 400 responses stay validation errors in the UI', async () => {
    const { app, queueFetch } = createHarness();

    queueFetch(
        createResponse(404, {}),
        createResponse(404, {}),
        createResponse(400, {
            error: {
                message: 'q must contain at least 3 normalized characters'
            }
        })
    );

    app.searchQuery = 'study';
    await app.performSearch();

    assert.equal(app.errorType, 'validation');
    assert.equal(app.getErrorTitle(), '参数校验失败');
    assert.equal(app.error, 'q must contain at least 3 normalized characters');
});

test('failed searches replace the previous successful page title', async () => {
    const { app, document, queueFetch } = createHarness();

    queueFetch(
        createResponse(200, {
            success: true,
            data: { headword: 'study', translation_zh: '学习' }
        })
    );

    app.fetchRelatedPhrases = () => {};
    app.searchQuery = 'study';
    await app.performSearch();

    assert.equal(document.title, 'study - 易思词典 | 学习');

    queueFetch(
        createResponse(404, {}),
        createResponse(404, {}),
        createResponse(200, { success: true, data: [] })
    );

    app.searchQuery = 'missing';
    await app.performSearch();

    assert.equal(app.errorType, 'not-found');
    assert.equal(document.title, '搜索 missing - 易思词典');
});

test('search 500 responses are classified as server errors instead of network failures', async () => {
    const { app, queueFetch } = createHarness();

    queueFetch(
        createResponse(500, {
            error: {
                message: 'dictionary backend unavailable'
            }
        })
    );

    app.searchQuery = 'study';
    await app.performSearch();

    assert.equal(app.errorType, 'server');
    assert.equal(app.getErrorTitle(), '服务暂时不可用');
    assert.equal(app.error, 'dictionary backend unavailable');
});

test('malformed 2xx word payloads are classified as server errors instead of not-found', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(200, {
            success: true
        })
    );

    app.searchQuery = 'study';
    await app.performSearch();

    assert.equal(app.errorType, 'server');
    assert.equal(app.getErrorTitle(), '服务暂时不可用');
    assert.equal(app.error, '词条数据格式错误，请稍后重试');
    assert.deepEqual(fetchCalls, ['/api/v1/words/study']);
});

test('malformed 2xx search payloads are classified as server errors instead of not-found', async () => {
    const { app, fetchCalls, queueFetch } = createHarness();

    queueFetch(
        createResponse(404, {}),
        createResponse(404, {}),
        createResponse(200, {
            success: true,
            data: {
                headword: 'study'
            }
        })
    );

    app.searchQuery = 'study';
    await app.performSearch();

    assert.equal(app.errorType, 'server');
    assert.equal(app.getErrorTitle(), '服务暂时不可用');
    assert.equal(app.error, '搜索结果格式错误，请稍后重试');
    assert.deepEqual(fetchCalls, [
        '/api/v1/words/study',
        '/api/v1/words/by-variant/study',
        '/api/v1/search?q=study&limit=10'
    ]);
});

test('timeout responses are not classified as not-found', async () => {
    const { app, queueFetch } = createHarness();

    queueFetch(
        createResponse(408, {
            error: {
                message: 'Request timed out'
            }
        })
    );

    app.searchQuery = 'study';
    await app.performSearch();

    assert.equal(app.errorType, 'timeout');
    assert.equal(app.getErrorTitle(), '请求超时');
    assert.equal(app.error, 'Request timed out');
});

test('rate-limit responses are not classified as not-found', async () => {
    const { app, queueFetch } = createHarness();

    queueFetch(
        createResponse(429, {
            error: {
                message: 'Rate limit exceeded'
            }
        })
    );

    app.searchQuery = 'study';
    await app.performSearch();

    assert.equal(app.errorType, 'rate-limit');
    assert.equal(app.getErrorTitle(), '请求过于频繁');
    assert.equal(app.error, 'Rate limit exceeded');
});

test('real fetch failures stay network errors in the UI', async () => {
    const { app, queueFetch } = createHarness();

    queueFetch(() => {
        throw new TypeError('fetch failed');
    });

    app.searchQuery = 'study';
    await app.performSearch();

    assert.equal(app.errorType, 'network');
    assert.equal(app.getErrorTitle(), '网络连接失败');
    assert.equal(app.error, '网络连接失败，请检查连接后重试');
});

// AC-R027: highlightWord 必须先转义 HTML 再包裹高亮匹配词
test('highlightWord escapes HTML in example text before wrapping matches', () => {
    // REG-027
    const { app } = createHarness();

    app.currentWord = { headword: 'test', variants: [], queried_variant: null };

    // Script tag in example sentence — must be escaped before highlighting
    const scriptInput = 'This is a <script>alert("xss")</script> test sentence.';
    const scriptResult = app.highlightWord(scriptInput, 'test');
    assert.ok(
        !scriptResult.includes('<script>'),
        `expected <script> to be escaped, got: ${scriptResult}`
    );
    assert.ok(
        scriptResult.includes('&lt;script&gt;'),
        `expected &lt;script&gt; in output, got: ${scriptResult}`
    );
    assert.ok(
        scriptResult.includes('<span class="highlight">test</span>'),
        `expected highlighted "test", got: ${scriptResult}`
    );

    // Event handler attribute — the <img> tag must be escaped so it cannot execute
    const eventInput = '<img src=x onerror="alert(1)"> test image';
    const eventResult = app.highlightWord(eventInput, 'test');
    assert.ok(
        !eventResult.includes('<img'),
        `expected <img tag to be escaped, got: ${eventResult}`
    );
    assert.ok(
        eventResult.includes('&lt;img'),
        `expected &lt;img in output, got: ${eventResult}`
    );
    assert.ok(
        eventResult.includes('<span class="highlight">test</span>'),
        `expected highlighted "test", got: ${eventResult}`
    );

    // Ampersand and quotes — must be escaped
    const entityInput = 'test with "quotes" & <angles>';
    const entityResult = app.highlightWord(entityInput, 'test');
    assert.ok(
        entityResult.includes('&amp;'),
        `expected &amp; in output, got: ${entityResult}`
    );
    assert.ok(
        entityResult.includes('&quot;'),
        `expected &quot; in output, got: ${entityResult}`
    );
    assert.ok(
        entityResult.includes('&lt;angles&gt;'),
        `expected escaped angles, got: ${entityResult}`
    );

    // No word to highlight — should still escape
    const noWordResult = app.highlightWord(scriptInput, null);
    assert.ok(
        !noWordResult.includes('<script>'),
        `expected <script> to be escaped even without highlight word, got: ${noWordResult}`
    );
    assert.ok(
        noWordResult.includes('&lt;script&gt;'),
        `expected &lt;script&gt; with null word, got: ${noWordResult}`
    );
});