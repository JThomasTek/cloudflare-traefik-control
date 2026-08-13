package internal

import "testing"

func TestRouterFromComment(t *testing.T) {
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
			t.Parallel()

			got, ok := routerFromComment(tt.comment)
			if ok != tt.wantOK {
				t.Fatalf("routerFromComment(%q) ok = %v, want %v", tt.comment, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("routerFromComment(%q) = %q, want %q", tt.comment, got, tt.want)
			}
		})
	}
}

// The two halves of the marker must agree: whatever we write on a record has to
// be readable back as the router that owns it.
func TestOwnershipCommentRoundTrip(t *testing.T) {
	t.Parallel()

	routers := []string{"web", "my-router", "router.with.dots", "", "router with spaces"}

	for _, router := range routers {
		comment := ownershipComment(router)

		got, ok := routerFromComment(comment)
		if !ok {
			t.Errorf("routerFromComment(ownershipComment(%q)) = not owned, want owned", router)
			continue
		}
		if got != router {
			t.Errorf("round trip of %q produced %q", router, got)
		}
	}
}
