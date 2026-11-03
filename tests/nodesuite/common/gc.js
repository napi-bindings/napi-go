// Replacement for Node.js's test/common/gc.js containing only the functions used by the ported
// tests (onGC, gcUntil). Semantics match Node.js: see LICENSE.node. Requires --expose-gc.
'use strict';

const assert = require('assert');
const { setImmediate: setImmediatePromisified } = require('timers/promises');
const common = require('./');

const gcTrackerMap = new WeakMap();
const gcTrackerTag = 'NODE_TEST_COMMON_GC_TRACKER';
// Number of GC cycles attempted by gcUntil before giving up, matching Node.js.
const DEFAULT_MAX_GC_COUNT = 10;

// onGC invokes gcListener.ongc() when obj is garbage-collected (via the destroy hook of an
// AsyncResource tied to obj).
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

// gcUntil runs global.gc() until condition() returns true, up to maxCount times.
async function gcUntil(name, condition, maxCount = DEFAULT_MAX_GC_COUNT, gcOptions) {
  for (let count = 0; count < maxCount; ++count) {
    await setImmediatePromisified();
    if (gcOptions) {
      await global.gc(gcOptions);
    } else {
      await global.gc();  // Passing undefined is not equivalent to passing no arguments.
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
