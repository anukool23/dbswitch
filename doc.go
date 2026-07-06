// Package dbswitch is a small, explicit, dependency-light data-access library
// for Go. You describe tables and run CRUD in plain Go, and dbswitch builds the
// right, always-parameterized SQL for the configured database — no struct-tag
// reflection and no query-DSL magic.
//
// v0.1.0 supports PostgreSQL. MySQL and MongoDB backends are planned.
//
// The core package has no third-party dependencies; each backend lives in its
// own sub-package (for example, package postgres) so you only pull in a driver
// you actually use.
//
// [Project documentation]: https://forge.anukool.me/dbswitch/
// [Source and issues]: https://github.com/anukool23/dbswitch
package dbswitch
