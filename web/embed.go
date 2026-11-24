// Package web embeds the dashboard templates and static assets.
package web

import "embed"

//go:embed templates/*.html static/*
var FS embed.FS
