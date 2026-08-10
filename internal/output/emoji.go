// Package output emoji: cool state + action glyphs used to decorate the
// structured log records. Two orthogonal vocabularies are exposed:
//
//   - State  emojis encode the record's level / outcome (✅ OK, ℹ️ INFO,
//     ⚠️ WARN, ❌ FAIL, ⏳ pending). StateEmoji maps a level token to its
//     glyph; the empty string is returned for unknown levels.
//   - Action emojis encode the pipeline step / verb (📂 open, 📥 stage,
//     🔍 diff, 🤖 ollama, 🔑 load key, 📝 commit, 🏷️ tag, ✍️ sign,
//     💬 msg). ActionEmoji maps a step name to its glyph; the empty
//     string is returned for unknown steps.
//
// The text tokens (OK / INFO / WARN / FAIL / step names) are kept intact
// in the rendered line — the emojis are prepended so the records stay
// greppable and the existing test contracts (substring checks on "INFO",
// "stage", "committed", "tagged", ...) keep passing.
package output

// StateEmoji returns the emoji for a structured-log level token
// ("OK", "INFO", "WARN", "FAIL", "" for unknown). The pending glyph is
// exposed as a constant for in-progress / spinner lines.
func StateEmoji(level string) string {
	switch level {
	case "OK":
		return "✅"
	case "INFO":
		return "ℹ️"
	case "WARN":
		return "⚠️"
	case "FAIL":
		return "❌"
	default:
		return ""
	}
}

// StatePending is the in-progress glyph used by spinner / progress lines.
const StatePending = "⏳"

// ActionEmoji returns the emoji for a pipeline step name. Unknown steps
// get the empty string so callers can render "icon step" only when an
// icon exists.
func ActionEmoji(step string) string {
	if e, ok := actionIcons[step]; ok {
		return e
	}
	return ""
}

// actionIcons maps each pipeline step to a cool action glyph.
var actionIcons = map[string]string{
	"open":     "📂",  // repository detection
	"stage":    "📥",  // staging changes
	"diff":     "🔍",  // reading the staged diff
	"ollama":   "🤖",  // LLM message generation
	"load key": "🔑",  // SSH signing key load
	"commit":   "📝",  // creating the commit
	"tag":      "🏷️", // semver tagging
	"sign":     "✍️", // signing notice
	"msg":      "💬",  // commit message preview
}
