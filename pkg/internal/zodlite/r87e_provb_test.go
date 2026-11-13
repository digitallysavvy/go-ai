package zodlite

import "testing"

// zod v4 z.number().gt(0) reports inclusive: false (node run of the TS
// checkout's zod/v4 on googleVideoMetadataSchema.parse({fps: 0}), CR
// r87e prova -> provb 1); an inclusive bound keeps inclusive: true.
func TestR87eprovbExclusiveBoundRendersInclusiveFalse(t *testing.T) {
	issue := &Issue{Code: "too_small", Origin: "number", Minimum: 0, Exclusive: true, Message: "Too small: expected number to be >0", Path: []interface{}{"fps"}}
	if got, want := string(issue.json()), `{"origin":"number","code":"too_small","minimum":0,"inclusive":false,"path":["fps"],"message":"Too small: expected number to be >0"}`; got != want {
		t.Fatalf("json = %s\nwant %s", got, want)
	}
	issue.Exclusive = false
	if got, want := string(issue.json()), `{"origin":"number","code":"too_small","minimum":0,"inclusive":true,"path":["fps"],"message":"Too small: expected number to be >0"}`; got != want {
		t.Fatalf("json = %s\nwant %s", got, want)
	}
	big := &Issue{Code: "too_big", Origin: "number", Maximum: 24, Exclusive: true, Message: "Too big: expected number to be <24", Path: []interface{}{"fps"}}
	if got, want := string(big.json()), `{"origin":"number","code":"too_big","maximum":24,"inclusive":false,"path":["fps"],"message":"Too big: expected number to be <24"}`; got != want {
		t.Fatalf("json = %s\nwant %s", got, want)
	}
}
