// Package ruleservice is the root package for the rule-service module.
// It exists solely to embed the migrations directory for use at runtime.
package ruleservice

import "embed"

// MigrationsFS contains all SQL migration files embedded at build time.
//
//go:embed migrations
var MigrationsFS embed.FS
