// napi-go: stand-in for node-addon-api's index.js, which test/napi_child.js reads to know whether
// child processes need --napi-modules (only for Node.js versions without built-in Node-API).
'use strict'

module.exports = { needsFlag: false }
