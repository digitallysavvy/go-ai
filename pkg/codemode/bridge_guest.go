package codemode

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/third_party/qjs"
)

// codeModeGuestSource is the in-sandbox half of the host tool bridge. It
// ports `run@2.1.8`'s BRIDGE_TRACKING_SOURCE (dist/runtime/guest-sources.js,
// createBridgePromise and __runAssertNoDetachedBridgeCalls) on top of the two
// Go host functions bindCodeModeDispatch installs:
//
//   - A `tools.<name>(input)` call serializes its input at once (run
//     serializes the arguments in the proxy's apply trap) and returns a
//     frozen thenable. Nothing reaches the host until the thenable is
//     observed: its first then/catch/finally marks the record observed and
//     pending and starts the host call one microtask later
//     (`Promise.resolve().then(start)`). A call the script never observes
//     never runs.
//   - The host settles a started call through settle(id, ok, value), one
//     settlement per drained job queue (driveCodeModeExecution).
//   - check() is run's __runAssertNoDetachedBridgeCalls: walking the records
//     in creation order, the first one never observed is an unawaited
//     request and the first one still pending a request the script returned
//     without waiting for. RunCodeMode calls it once the script's result is
//     serialized, as run's serializeValuePayload does.
//
// It also installs run's DETERMINISTIC_APIS_SOURCE: Date.now() (and Date()
// / new Date() without arguments) returns a guest clock that starts at the
// run's recorded start time, advances by 1 ms per read and is reset to the
// host time recorded for each settled call when its outcome is delivered,
// and Math.random() is run's xorshift128+ generator seeded from the run's
// recorded seed. A resumed run reuses both, so it computes the same values
// it computed before the interruption.
//
// The intrinsics it uses are captured before any user source runs.
// __CODE_MODE_DETERMINISM__ is replaced by the run's determinism (JSON).
const codeModeGuestSource = `(function () {
  "use strict";
  var resetDateNow = (function (config) {
    var OriginalDate = Date;
    var OriginalNumber = Number;
    var OriginalMath = Math;
    var OriginalReflect = Reflect;
    var dateNowMs = OriginalNumber(config && config.dateNowMs);
    if (!OriginalNumber.isFinite(dateNowMs)) dateNowMs = 0;

    function resetDateNow(value) {
      var next = OriginalNumber(value);
      if (OriginalNumber.isFinite(next)) dateNowMs = OriginalMath.trunc(next);
    }

    function nextDateMs() {
      var value = dateNowMs;
      dateNowMs += 1;
      return value;
    }

    function RunDate() {
      if (new.target) {
        if (arguments.length === 0) return new OriginalDate(nextDateMs());
        return OriginalReflect.construct(OriginalDate, arguments, new.target);
      }
      return new OriginalDate(nextDateMs()).toString();
    }

    Object.defineProperty(RunDate, 'prototype', {
      value: OriginalDate.prototype,
      writable: false,
      configurable: false,
    });
    Object.defineProperty(OriginalDate.prototype, 'constructor', {
      value: RunDate,
      writable: false,
      configurable: false,
    });
    Object.defineProperty(RunDate, 'now', {
      value: nextDateMs,
      writable: false,
      configurable: false,
    });
    Object.defineProperty(RunDate, 'parse', {
      value: OriginalDate.parse,
      writable: false,
      configurable: false,
    });
    Object.defineProperty(RunDate, 'UTC', {
      value: OriginalDate.UTC,
      writable: false,
      configurable: false,
    });
    Object.defineProperty(globalThis, 'Date', {
      value: RunDate,
      writable: false,
      configurable: false,
    });

    var randomSeed = String(config && config.randomSeed || '');
    var mask64 = (1n << 64n) - 1n;

    function readSeedPart(offset, fallback) {
      var part = randomSeed.slice(offset, offset + 16);
      return /^[0-9a-fA-F]{16}$/.test(part) ? BigInt('0x' + part) : fallback;
    }

    var randomState0 = readSeedPart(0, 0x243f6a8885a308d3n);
    var randomState1 = readSeedPart(16, 0x13198a2e03707344n);
    if ((randomState0 | randomState1) === 0n) randomState1 = 0x9e3779b97f4a7c15n;

    function nextRandom64() {
      var s1 = randomState0;
      var s0 = randomState1;
      randomState0 = s0;
      s1 = (s1 ^ ((s1 << 23n) & mask64)) & mask64;
      randomState1 = (s1 ^ s0 ^ (s1 >> 17n) ^ (s0 >> 26n)) & mask64;
      return (randomState1 + s0) & mask64;
    }

    Object.defineProperty(Math, 'random', {
      value: function () {
        return OriginalNumber(nextRandom64() >> 11n) / 9007199254740992;
      },
      writable: false,
      configurable: false,
    });
    return resetDateNow;
  })(__CODE_MODE_DETERMINISM__);
  var start = globalThis.__codeModeStart;
  var serialize = globalThis.__codeModeSerialize;
  delete globalThis.__codeModeStart;
  delete globalThis.__codeModeSerialize;
  var OriginalPromise = Promise;
  var promiseResolve = Promise.resolve;
  var promiseThen = Promise.prototype.then;
  var promiseCatch = Promise.prototype.catch;
  var promiseFinally = Promise.prototype.finally;
  var apply = Reflect.apply;
  var freeze = Object.freeze;
  var defineProperty = Object.defineProperty;
  var createObject = Object.create;
  var OriginalProxy = Proxy;
  var OriginalString = String;
  var toStringTag = Symbol.toStringTag;
  var stringify = JSON.stringify;
  var waiting = new Map();
  var mapGet = Map.prototype.get;
  var mapSet = Map.prototype.set;
  var mapDelete = Map.prototype.delete;
  var records = [];
  var nextRecordId = 0;

  function createBridgePromise(toolName, hasInput, input) {
    var inputJson = "";
    var serializeFailed = false;
    var serializeMessage = "";
    if (hasInput) {
      try {
        inputJson = serialize(input);
      } catch (error) {
        serializeFailed = true;
        serializeMessage = OriginalString(error && error.message);
      }
    }
    var record = { id: ++nextRecordId, toolName: toolName, observed: false, status: "idle" };
    records[records.length] = record;
    var promise;

    function getPromise() {
      record.observed = true;
      if (!promise) {
        record.status = "pending";
        var started = apply(promiseThen, apply(promiseResolve, OriginalPromise, []), [function () {
          return new OriginalPromise(function (resolve, reject) {
            var id = start(toolName, inputJson, serializeFailed, serializeMessage);
            apply(mapSet, waiting, [id, { resolve: resolve, reject: reject }]);
          });
        }]);
        promise = apply(promiseThen, started, [
          function (value) {
            record.status = "fulfilled";
            return value;
          },
          function (error) {
            record.status = "rejected";
            throw error;
          }
        ]);
      }
      return promise;
    }

    var thenable = {
      then: function (onFulfilled, onRejected) {
        return apply(promiseThen, getPromise(), [onFulfilled, onRejected]);
      },
      catch: function (onRejected) {
        return apply(promiseCatch, getPromise(), [onRejected]);
      },
      finally: function (onFinally) {
        return apply(promiseFinally, getPromise(), [onFinally]);
      }
    };
    defineProperty(thenable, toStringTag, {
      get: function () { return "Promise"; },
      enumerable: true,
      configurable: true
    });
    return freeze(thenable);
  }

  var tools = new OriginalProxy(createObject(null), {
    get: function (_target, name) {
      return (input) => createBridgePromise(OriginalString(name), input !== undefined, input);
    }
  });
  defineProperty(globalThis, "__codeModeTools", { value: tools, writable: false, configurable: false });

  return freeze({
    settle: function (id, ok, value, dateNowMs) {
      var waiter = apply(mapGet, waiting, [id]);
      if (waiter === undefined) return false;
      apply(mapDelete, waiting, [id]);
      resetDateNow(dateNowMs);
      if (ok) waiter.resolve(value); else waiter.reject(value);
      return true;
    },
    check: function () {
      for (var i = 0; i < records.length; i++) {
        var record = records[i];
        if (!record.observed) return stringify(["unawaited", record.id, record.toolName, record.status]);
        if (record.status === "pending") return stringify(["pending", record.id, record.toolName, record.status]);
      }
      return "";
    }
  });
})()`

// bindCodeModeDispatch installs the bridge in jsCtx: the Go host functions
// the guest source captures (__codeModeStart, __codeModeSerialize; both
// removed from the global object again before any user source runs) and the
// guest source itself, whose settle/check object is kept on the bridge.
func bindCodeModeDispatch(jsCtx *qjs.Context, bridge *toolBridge) error {
	serialize := jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) == 0 || args[0].IsUndefined() {
			return jsCtx.NewString(""), nil
		}
		js, err := args[0].JSONStringify()
		if err != nil {
			return nil, err
		}
		return jsCtx.NewString(js), nil
	})
	start := jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 4 {
			return nil, NewProtocolError("code-mode bridge start called with too few arguments.", nil)
		}
		var serializeErr error
		if args[2].Bool() {
			serializeErr = errors.New(args[3].String())
		}
		id := bridge.start(args[0].String(), args[1].String(), serializeErr)
		return jsCtx.NewFloat64(float64(id)), nil
	})
	global := jsCtx.Global()
	global.SetPropertyStr("__codeModeSerialize", serialize)
	global.SetPropertyStr("__codeModeStart", start)

	determinism, derr := json.Marshal(bridge.determinism)
	if derr != nil {
		return NewProtocolError("Failed to encode the code mode determinism state.", nil)
	}
	source := strings.Replace(codeModeGuestSource, "__CODE_MODE_DETERMINISM__", string(determinism), 1)
	guest, err := jsCtx.Eval("code-mode-bridge.js", qjs.Code(source))
	if err != nil {
		return err
	}
	bridge.guest = guest
	return nil
}

// settleGuest delivers one host call's outcome to the guest (settle).
func settleGuest(jsCtx *qjs.Context, guest *qjs.Value, resp bridgeResponse) error {
	var value *qjs.Value
	switch {
	case !resp.ok:
		value = jsCtx.NewError(errors.New(resp.errMessage))
	case resp.outputJSON == "":
		value = jsCtx.NewUndefined()
	default:
		value = jsCtx.ParseJSON(resp.outputJSON)
	}
	id := jsCtx.NewFloat64(float64(resp.id))
	ok := jsCtx.NewBool(resp.ok)
	// A record without a host time (older continuations) leaves the guest
	// clock as it is (run's resetDateNow ignores a non-finite value).
	dateNowMs := jsCtx.NewUndefined()
	if resp.dateNowMs != 0 {
		dateNowMs = jsCtx.NewFloat64(float64(resp.dateNowMs))
	}
	defer id.Free()
	defer ok.Free()
	defer value.Free()
	defer dateNowMs.Free()
	result, err := guest.InvokeJS("settle", id, ok, value, dateNowMs)
	if result != nil {
		result.Free()
	}
	return err
}

// guestDetachedCheck runs the guest's check() and, when it names a detached
// request, returns the *DetachedBridgeRequestError TS code mode reports:
// `run`'s message with the host function name replaced by the tool's
// property access (inserted literally), and run's details. A call to an
// unknown tool goes through TS's `__codeMode.missing` host function, whose
// name is left as it is.
func guestDetachedCheck(guest *qjs.Value, toolNames []string) error {
	result, err := guest.InvokeJS("check")
	if err != nil {
		return err
	}
	defer result.Free()
	encoded := result.String()
	if encoded == "" {
		return nil
	}
	var entry []interface{}
	if jerr := json.Unmarshal([]byte(encoded), &entry); jerr != nil || len(entry) != 4 {
		return NewProtocolError("Code mode bridge reported a malformed detached request.", nil)
	}
	kind, _ := entry[0].(string)
	id, _ := entry[1].(float64)
	toolName, _ := entry[2].(string)
	status, _ := entry[3].(string)

	hostName := "__codeMode.missing"
	display := hostName
	for i, name := range toolNames {
		if name == toolName {
			hostName = "__codeMode.tool" + strconv.Itoa(i)
			display = "tools" + formatPropertyAccess(toolName)
			break
		}
	}
	message := "JavaScript runtime created an unawaited hostFunction bridge request: " + display + "."
	if kind == "pending" {
		message = "JavaScript runtime returned while a hostFunction bridge request was still pending: " + display + "."
	}
	return NewDetachedBridgeRequestError(message, map[string]interface{}{
		"id":     int(id),
		"kind":   "hostFunction",
		"name":   hostName,
		"status": status,
	})
}
