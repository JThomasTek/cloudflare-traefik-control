package internal

import "strings"

// ownershipPrefix marks a Cloudflare DNS record as one CTC created and is
// therefore allowed to change or delete. The Traefik router name is appended to
// it, which is how a record is matched back to the router that caused it to
// exist.
//
// Changing this string orphans every record already carrying the old one.
const ownershipPrefix = "Managed by ctc: "

// ownershipComment renders the record comment CTC writes to claim a DNS record
// on behalf of the given router.
func ownershipComment(router string) string {
	return ownershipPrefix + router
}

// routerFromComment extracts the router name from a DNS record comment. It
// returns (router, true) only for comments carrying the ownership prefix, and
// ("", false) for records CTC does not own.
func routerFromComment(comment string) (string, bool) {
	// CutPrefix hands back the original string when the prefix is absent, which
	// would read as a router named after someone else's comment.
	router, ok := strings.CutPrefix(comment, ownershipPrefix)
	if !ok {
		return "", false
	}

	return router, true
}
