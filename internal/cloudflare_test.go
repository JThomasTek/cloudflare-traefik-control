package internal

import (
	"errors"
	"testing"
)

// errStub is a sentinel error reused by tests that stub out Cloudflare calls.
var errStub = errors.New("stub cloudflare error")

func TestParseRouterFromComment(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    string
		wantOK  bool
	}{
		{name: "managed record", comment: "Managed by ctc: my-router", want: "my-router", wantOK: true},
		{name: "prefix only", comment: "Managed by ctc: ", want: "", wantOK: true},
		{name: "unmanaged record", comment: "Some other comment", want: "", wantOK: false},
		{name: "shorter than prefix", comment: "Managed", want: "", wantOK: false},
		{name: "empty comment", comment: "", want: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRouterFromComment(tt.comment)
			if ok != tt.wantOK {
				t.Fatalf("parseRouterFromComment(%q) ok = %v, want %v", tt.comment, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("parseRouterFromComment(%q) = %q, want %q", tt.comment, got, tt.want)
			}
		})
	}
}
