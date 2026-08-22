package webassets

import "embed"

// Files contains the templates and static assets shipped with RingRing.
//
//go:embed templates/*.html static/*
var Files embed.FS
