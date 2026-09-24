// Package views holds the hand-written view queries: the lens-filtered and
// cursor-paged reads of docs/06 whose shape depends on the request, which
// sqlc cannot express as one static statement.
//
// This is the only place outside package dbq where SQL lives, and the split is
// a rule rather than a convenience (docs/04, "Engine"). A query whose shape is
// fixed belongs in ../queries/ and goes through sqlc. Queries here use only
// database/sql and take the *sql.Tx that db.Read or db.Write hands them, so
// they stay inside the same transaction as the generated ones.
package views
