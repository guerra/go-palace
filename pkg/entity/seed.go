package entity

// Seed data for detectors + registry. Lowercase-canonical keys so callers
// can `commonTools[strings.ToLower(word)]` for O(1) membership.

// commonTools is the tool-name seed used by the detector and optionally
// pre-populated into the Registry. ~40 entries cover the most-cited modern
// developer tools — deliberately short so we avoid the false-positive blast
// radius of a full package-manager index.
var commonTools = map[string]bool{
	"docker":     true,
	"git":        true,
	"python":     true,
	"node":       true,
	"rust":       true,
	"go":         true,
	"kubernetes": true,
	"terraform":  true,
	"ansible":    true,
	"postgresql": true,
	"redis":      true,
	"mysql":      true,
	"mongodb":    true,
	"nginx":      true,
	"aws":        true,
	"gcp":        true,
	"azure":      true,
	"kafka":      true,
	"vim":        true,
	"vscode":     true,
	"tmux":       true,
	"bash":       true,
	"zsh":        true,
	"fish":       true,
	"npm":        true,
	"yarn":       true,
	"pnpm":       true,
	"pip":        true,
	"cargo":      true,
	"make":       true,
	"gcc":        true,
	"clang":      true,
	"webpack":    true,
	"vite":       true,
	"jest":       true,
	"pytest":     true,
	"mocha":      true,
	"gradle":     true,
	"maven":      true,
	"sqlite":     true,
}

// placeSuffixes are geographic-hint words that, when preceded by a capitalized
// span, mark the span as a place (Mount Washington, Hudson River, ...).
var placeSuffixes = map[string]bool{
	"mountain":  true,
	"mount":     true,
	"river":     true,
	"street":    true,
	"avenue":    true,
	"city":      true,
	"town":      true,
	"village":   true,
	"county":    true,
	"province":  true,
	"capital":   true,
	"district":  true,
	"island":    true,
	"region":    true,
	"country":   true,
	"continent": true,
	"lake":      true,
	"bay":       true,
	"ocean":     true,
	"sea":       true,
	"valley":    true,
	"peak":      true,
	"hill":      true,
	"forest":    true,
	"desert":    true,
	"beach":     true,
}
