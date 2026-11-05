package ragfixtures

import "embed"

// Files embeds all structured JSON and Markdown fixtures in app_build/fixtures/rag.
//
//go:embed *.json *.md
var Files embed.FS
