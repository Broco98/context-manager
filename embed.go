// Package assets holds the agent-integration files embedded into the ctx binary.
//
// go:embed cannot reference parent directories, so this package lives at the
// module root where skill/ and docs/ are reachable. Keeping the embed here lets
// skill/context-manager/SKILL.md and docs/global-claude-md-snippet.md remain the
// single canonical source — `ctx skill install` writes these exact bytes, so a
// go-installed binary needs no source tree present.
package assets

import _ "embed"

//go:embed skill/context-manager/SKILL.md
var SkillMD string

//go:embed docs/global-claude-md-snippet.md
var Snippet string
