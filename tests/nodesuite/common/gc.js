// Sostituto di test/common/gc.js della suite di Node.js con le sole funzioni usate dai test
// portati (onGC, gcUntil). Semantica ripresa da Node.js: vedi LICENSE.node. Richiede
// --expose-gc.
'use strict';

const assert = require('assert');
const { setImmediate: setImmediatePromisified } = require('timers/promises');
const common = require('./');

const gcTrackerMap = new WeakMap();
const gcTrackerTag = 'NODE_TEST_COMMON_GC_TRACKER';
// Numero di raccolte tentate da gcUntil prima di arrendersi, come in Node.js.
const DEFAULT_MAX_GC_COUNT = 10;

// onGC chiama gcListener.ongc() quando obj viene raccolto (tramite il destroy di un
// AsyncResource legato a obj).
function onGC(obj, gcListener) {
  const asyncHooks = require('async_hooks');

  const onGcAsyncHook = asyncHooks.createHook({
    init: common.mustCallAtLeast(function(id, type) {
      if (this.trackedId === undefined) {
        assert.strictEqual(type, gcTrackerTag);
        this.trackedId = id;
      }
    }),
    destroy(id) {
      assert.notStrictEqual(this.trackedId, -1);
      if (id === this.trackedId) {
        this.gcListener.ongc();
        onGcAsyncHook.disable();
      }
    },
  }).enable();
  onGcAsyncHook.gcListener = gcListener;

  gcTrackerMap.set(obj, new asyncHooks.AsyncResource(gcTrackerTag));
  obj = null;
}

// gcUntil esegue global.gc() finché condition() diventa vera, al massimo maxCount volte.
async function gcUntil(name, condition, maxCount = DEFAULT_MAX_GC_COUNT, gcOptions) {
  for (let count = 0; count < maxCount; ++count) {
    await setImmediatePromisified();
    if (gcOptions) {
      await global.gc(gcOptions);
    } else {
      await global.gc();  // undefined non equivale a nessun argomento.
    }
    if (condition()) {
      return;
    }
  }
  throw new Error(`Test ${name} failed`);
}

module.exports = {
  gcUntil,
  onGC,
};
