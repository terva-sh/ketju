package db

// sqlc compiles queries/*.sql against migrations/ into package dbq, and the
// parse smoke in testdata/smoke/ into testdata/smoke/out. Generated code is
// committed; CI regenerates it and fails on any difference.
//
// go run rather than a tool directive, so sqlc's dependencies stay out of the
// shipped module's go.mod. v1.31.1 parses SQL through WebAssembly and builds
// with CGO_ENABLED=0.
//
//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate -f ../../../sqlc.yaml
