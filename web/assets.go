// Package web embeds the GUI's templates and static assets into the
// compiled binary, so deployment to the Pi is a single file.
package web

import "embed"

//go:embed templates
var Templates embed.FS

//go:embed static
var Static embed.FS
