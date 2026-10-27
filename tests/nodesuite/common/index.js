// Sostituto minimo di test/common/index.js della suite di Node.js, con le sole funzioni usate dai
// test di js-native-api e node-api. La semantica (conteggio delle chiamate verificato all'uscita,
// codici di abort, messaggio di skip) è ripresa da Node.js: vedi LICENSE.node.
'use strict';

const assert = require('assert');
const path = require('path');
const { inspect } = require('util');

const isWindows = process.platform === 'win32';
const isMacOS = process.platform === 'darwin';
const isLinux = process.platform === 'linux';

// Gli addon napi-go vengono sempre messi in build/Release dal runner.
const buildType = 'Release';

// Exit code e segnali di un processo node terminato con abort (vedi nodeProcessAborted).
const ABORT_EXIT_CODES = [132, 133, 134];
const ABORT_SIGNALS = ['SIGILL', 'SIGTRAP', 'SIGABRT'];
const WINDOWS_BREAKPOINT_EXIT_CODE = 0x80000003;
const WINDOWS_ABORT_EXIT_CODE = 134;
const EXIT_FAILURE = 1;

function noop() {}

const mustCallChecks = [];

function runCallChecks(exitCode) {
  if (exitCode !== 0) return;

  const failed = mustCallChecks.filter((context) => {
    if ('minimum' in context) {
      context.messageSegment = `at least ${context.minimum}`;
      return context.actual < context.minimum;
    }
    context.messageSegment = `exactly ${context.exact}`;
    return context.actual !== context.exact;
  });

  for (const context of failed) {
    process.stderr.write(
      `Mismatched ${context.name} function calls. Expected ${context.messageSegment}, ` +
      `actual ${context.actual}.\n${context.stack.split('\n').slice(2).join('\n')}\n`);
  }

  if (failed.length) process.exit(EXIT_FAILURE);
}

function mustCallInner(fn, criteria = 1, field) {
  if (process._exiting) {
    throw new Error('Cannot use common.mustCall*() in process exit handler');
  }
  if (typeof fn === 'number') {
    criteria = fn;
    fn = noop;
  } else if (fn === undefined) {
    fn = noop;
  }
  if (typeof criteria !== 'number') {
    throw new TypeError(`Invalid ${field} value: ${criteria}`);
  }

  const context = {
    [field]: criteria,
    actual: 0,
    stack: inspect(new Error()),
    name: fn.name || '<anonymous>',
  };

  // Un solo listener di uscita, come in Node.js.
  if (mustCallChecks.length === 0) process.on('exit', runCallChecks);
  mustCallChecks.push(context);

  const wrapped = function() {
    context.actual++;
    return fn.apply(this, arguments);
  };
  Object.defineProperties(wrapped, {
    name: { value: fn.name, writable: false, enumerable: false, configurable: true },
    length: { value: fn.length, writable: false, enumerable: false, configurable: true },
  });
  return wrapped;
}

function mustCall(fn, exact) {
  return mustCallInner(fn, exact, 'exact');
}

function mustCallAtLeast(fn, minimum) {
  return mustCallInner(fn, minimum, 'minimum');
}

function mustSucceed(fn, exact) {
  return mustCall(function(err, ...args) {
    assert.ifError(err);
    if (typeof fn === 'function') return fn.apply(this, args);
  }, exact);
}

// callSite restituisce "file:riga" del chiamante di chi la invoca.
function callSite() {
  const frames = (new Error().stack || '').split('\n');
  // [0] "Error", [1] callSite, [2] mustNotCall, [3] il chiamante.
  const frame = frames[3] || '';
  const match = frame.match(/\(?([^()\s]+):(\d+):\d+\)?$/);
  return match ? `${match[1]}:${match[2]}` : '<unknown>';
}

function mustNotCall(msg) {
  const site = callSite();
  return function mustNotCall(...args) {
    const argsInfo = args.length > 0 ?
      `\ncalled with arguments: ${args.map((arg) => inspect(arg)).join(', ')}` : '';
    assert.fail(`${msg || 'function should not have been called'} at ${site}${argsInfo}`);
  };
}

function printSkipMessage(msg) {
  process.stdout.write(`1..0 # Skipped: ${msg}\n`);
}

function skip(msg) {
  printSkipMessage(msg);
  process.exit(0);
}

function platformTimeout(ms) {
  return ms;
}

// nodeProcessAborted riconosce l'uscita di un processo node terminato con abort.
function nodeProcessAborted(exitCode, signal) {
  if (signal !== null) return ABORT_SIGNALS.includes(signal);
  const expected = isWindows ?
    [WINDOWS_BREAKPOINT_EXIT_CODE, WINDOWS_ABORT_EXIT_CODE] :
    ABORT_EXIT_CODES;
  return expected.includes(exitCode);
}

module.exports = {
  buildType,
  isLinux,
  isMacOS,
  isWindows,
  mustCall,
  mustCallAtLeast,
  mustNotCall,
  mustSucceed,
  nodeProcessAborted,
  platformTimeout,
  printSkipMessage,
  skip,
  // Radice della suite, utile ai moduli di supporto (fixtures, tmpdir).
  suiteRoot: path.resolve(__dirname, '..'),
};
