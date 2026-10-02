// Package api is a fixture for TestTheLayeringScanFindsAViolation: a
// handler takes the database handle off a service. It is parsed, never
// compiled.
package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/service/users"
)

func profile(u *users.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = u.DB
	}
}
