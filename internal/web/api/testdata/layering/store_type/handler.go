// Package api is a fixture for TestTheLayeringScanFindsAViolation: a
// handler names session.Store. It is parsed, never compiled.
package api

import "github.com/terva-sh/ketju/internal/auth/session"

func sessionList() {
	var s session.Store
	_ = s
}
