package codemode

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR87cintegTamperedBookkeepingStillDetached: a script that shadows the
// guest's records array index with an Array.prototype setter must not be
// able to return while a host call is pending or never observed. TS run
// freezes the intrinsics before user code (HARDENING_SOURCE toFreeze) and
// the host also rejects a result while bridge requests are in flight
// (manager.js handleReadyMessage).
func TestR87cintegTamperedBookkeepingStillDetached(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	defer close(release)
	tools := ToolSet{
		"slow": {Name: "slow", Parameters: r87ccarryObject,
			Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				calls.Add(1)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return map[string]interface{}{"ok": true}, nil
			}},
	}
	tamper := "try { Object.defineProperty(Array.prototype, '0', { set(v) {}, configurable: true }); } catch {}\n"
	for name, js := range map[string]string{
		"unawaited": tamper + "tools.slow({}); return 1;",
		"pending":   tamper + "const p = tools.slow({}); p.then(() => {}); await Promise.resolve(); return 1;",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools})
			var detached *DetachedBridgeRequestError
			if !errors.As(err, &detached) {
				t.Fatalf("err = %v, want *DetachedBridgeRequestError", err)
			}
		})
	}
}

// TestR87cintegIntrinsicsFrozen: run freezes the intrinsics and locks the
// intrinsic globals before user code runs (guest-sources.js
// HARDENING_SOURCE).
func TestR87cintegIntrinsicsFrozen(t *testing.T) {
	out, err := RunCodeMode(context.Background(), RunInput{JS: `
		"use strict";
		const r = [];
		for (const [name, o] of [["Array.prototype", Array.prototype], ["Object.prototype", Object.prototype], ["Promise.prototype", Promise.prototype], ["Math", Math], ["JSON", JSON], ["Date", Date], ["Date.prototype", Date.prototype], ["Map.prototype", Map.prototype], ["AsyncFunction.prototype", Object.getPrototypeOf(async function(){})], ["Function.prototype", Object.getPrototypeOf(function(){})]]) {
			r.push(name + ":" + Object.isFrozen(o));
		}
		try { globalThis.Array = 1; } catch (e) { r.push("Array global:" + e.constructor.name); }
		r.push("Math.random:" + (typeof Math.random()));
		return r.join(",");
	`, Tools: ToolSet{}})
	if err != nil {
		t.Fatal(err)
	}
	want := "Array.prototype:true,Object.prototype:true,Promise.prototype:true,Math:true,JSON:true,Date:true,Date.prototype:true,Map.prototype:true,AsyncFunction.prototype:true,Function.prototype:true,Array global:TypeError,Math.random:number"
	if out != want {
		t.Fatalf("got %v\nwant %v", out, want)
	}
}

// TestR87cintegReplayDivergenceOfThePendingToolName: a resumed approval
// whose call reaches another tool than the approved one (same input) is the
// TS run replay divergence (assertReplayMatches compares the host function
// name), never an approved run of the other tool.
func TestR87cintegReplayDivergenceOfThePendingToolName(t *testing.T) {
	err := r87ccarryResumeTamperedContinuation(t, "return await tools.guarded({ n: 1 });", func(c *Continuation) {
		c.PendingInterruptions[0].ToolName = "echo"
	})
	protocol := r87ccarryProtocolError(t, err, "Continuation replay diverged from the recorded host function ledger.")
	assertDeepEqual(t, protocol.Details, map[string]interface{}{"bridgeIndex": 1, "expectedHostFunctionName": "__codeMode.tool0", "receivedHostFunctionName": "__codeMode.tool1"})
}
