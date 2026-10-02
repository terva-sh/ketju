// Package api is a fixture for TestTheLayeringScanFindsAViolation: it
// imports a store package. It is parsed, never compiled.
package api

import "github.com/terva-sh/ketju/internal/store/db"

var _ *db.Options
