package prodia

import "testing"

func TestProdiaErrorMessageFollowsTSHandler(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		status           int
	}{
		{"detail string", `{"detail":"bad","error":"e","message":"m"}`, "bad", 400},
		{"detail object is compact JSON in source order", `{"detail": {"z": 1, "a": [1, 2]}}`, `{"z":1,"a":[1,2]}`, 422},
		{"error beats message", `{"error":"e","message":"m"}`, "e", 400},
		{"message", `{"message":"m"}`, "m", 400},
		{"null detail falls through", `{"detail":null,"message":"m"}`, "m", 400},
		{"nothing", `{}`, "Unknown Prodia error", 500},
		{"not json", `oops`, "Bad Gateway", 502},
		{"empty", ``, "Service Unavailable", 503},
		{"message wrong type is off schema", `{"message":5}`, "Bad Request", 400},
		{"null message is off schema", `{"message":null}`, "Bad Request", 400},
		{"array body", `[]`, "Bad Gateway", 502},
	} {
		if got := prodiaErrorMessage(c.status, []byte(c.body)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
