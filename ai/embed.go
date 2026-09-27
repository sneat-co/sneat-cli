package ai

import "embed"

// SkillsFS holds every file under skills/ at build time.
//
//go:embed all:skills
var SkillsFS embed.FS
