// Package api is a fixture for TestTheLayeringScanFindsAViolation: it
// imports database/sql. It is parsed, never compiled.
package api

import "database/sql"

var _ sql.NullInt64
