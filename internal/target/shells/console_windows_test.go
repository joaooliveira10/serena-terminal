//go:build windows

package shells

import "testing"

// TestDecodeCodePage uses the bytes Windows PowerShell 5.1 was seen to write
// to a pipe on a system with console code page 850.
func TestDecodeCodePage(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		codePage uint32
		want     string
	}{
		{"ASCII", `C:\Users\u\Documents`, 850, `C:\Users\u\Documents`},
		{"a with tilde in 850", "Jo\xc6o", 850, "Jo\u00e3o"},
		{"e with acute in 850", "Jos\x82", 850, "Jos\u00e9"},
		{"a with diaeresis in 437", "J\x84ger", 437, "J\u00e4ger"},
		{"UTF-8 when the console is set to it", "Jo\xc3\xa3o", 65001, "Jo\u00e3o"},
		{"empty", "", 850, ""},
		{"a code page that does not exist", "abc", 1, "abc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := decodeCodePage([]byte(test.data), test.codePage); got != test.want {
				t.Errorf("decodeCodePage = %q, want %q", got, test.want)
			}
		})
	}
	// Whatever the console of the test run is, ASCII comes through unchanged.
	if got := decodeConsole([]byte("RemoteSigned\r\n")); got != "RemoteSigned\r\n" {
		t.Errorf("decodeConsole = %q", got)
	}
}
