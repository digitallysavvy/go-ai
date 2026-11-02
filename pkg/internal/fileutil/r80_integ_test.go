package fileutil

import (
	"bytes"
	"testing"
)

// TS convertBase64ToUint8Array: base64url mapped to the standard alphabet,
// then atob (whitespace ignored, padding optional).
func TestR80integConvertBase64ToBytes(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []byte
		err  bool
	}{
		{in: "aGk=", want: []byte("hi")},
		{in: "aGk", want: []byte("hi")},
		{in: "-_8", want: []byte{0xfb, 0xff}},
		{in: "+/8=", want: []byte{0xfb, 0xff}},
		{in: " aG\nk ", want: []byte("hi")},
		{in: "", want: []byte{}},
		{in: "a", err: true},
		{in: "a=Gk", err: true},
		{in: "a*Gk", err: true},
	} {
		got, err := ConvertBase64ToBytes(tc.in)
		if tc.err {
			if err == nil {
				t.Fatalf("%q: want an error, got %v", tc.in, got)
			}
			continue
		}
		if err != nil || !bytes.Equal(got, tc.want) {
			t.Fatalf("%q = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}
