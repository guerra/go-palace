package extractor

import "regexp"

// Marker pattern strings — ported verbatim from
// mempalace/general_extractor.py:30-161. All patterns use RE2-compatible
// syntax (no backreferences, no lookaround); Python `re.IGNORECASE` is
// expressed as inline `(?i)` flags.
var (
	decisionMarkerPatterns = []string{
		`(?i)\blet'?s (use|go with|try|pick|choose|switch to)\b`,
		`(?i)\bwe (should|decided|chose|went with|picked|settled on)\b`,
		`(?i)\bi'?m going (to|with)\b`,
		`(?i)\bbetter (to|than|approach|option|choice)\b`,
		`(?i)\binstead of\b`,
		`(?i)\brather than\b`,
		`(?i)\bthe reason (is|was|being)\b`,
		`(?i)\bbecause\b`,
		`(?i)\btrade-?off\b`,
		`(?i)\bpros and cons\b`,
		`(?i)\bover\b.*\bbecause\b`,
		`(?i)\barchitecture\b`,
		`(?i)\bapproach\b`,
		`(?i)\bstrategy\b`,
		`(?i)\bpattern\b`,
		`(?i)\bstack\b`,
		`(?i)\bframework\b`,
		`(?i)\binfrastructure\b`,
		`(?i)\bset (it |this )?to\b`,
		`(?i)\bconfigure\b`,
		`(?i)\bdefault\b`,
	}

	preferenceMarkerPatterns = []string{
		`(?i)\bi prefer\b`,
		`(?i)\balways use\b`,
		`(?i)\bnever use\b`,
		`(?i)\bdon'?t (ever |like to )?(use|do|mock|stub|import)\b`,
		`(?i)\bi like (to|when|how)\b`,
		`(?i)\bi hate (when|how|it when)\b`,
		`(?i)\bplease (always|never|don'?t)\b`,
		`(?i)\bmy (rule|preference|style|convention) is\b`,
		`(?i)\bwe (always|never)\b`,
		`(?i)\bfunctional\b.*\bstyle\b`,
		`(?i)\bimperative\b`,
		`(?i)\bsnake_?case\b`,
		`(?i)\bcamel_?case\b`,
		`(?i)\btabs\b.*\bspaces\b`,
		`(?i)\bspaces\b.*\btabs\b`,
		`(?i)\buse\b.*\binstead of\b`,
	}

	milestoneMarkerPatterns = []string{
		`(?i)\bit works\b`,
		`(?i)\bit worked\b`,
		`(?i)\bgot it working\b`,
		`(?i)\bfixed\b`,
		`(?i)\bsolved\b`,
		`(?i)\bbreakthrough\b`,
		`(?i)\bfigured (it )?out\b`,
		`(?i)\bnailed it\b`,
		`(?i)\bcracked (it|the)\b`,
		`(?i)\bfinally\b`,
		`(?i)\bfirst time\b`,
		`(?i)\bfirst ever\b`,
		`(?i)\bnever (done|been|had) before\b`,
		`(?i)\bdiscovered\b`,
		`(?i)\brealized\b`,
		`(?i)\bfound (out|that)\b`,
		`(?i)\bturns out\b`,
		`(?i)\bthe key (is|was|insight)\b`,
		`(?i)\bthe trick (is|was)\b`,
		`(?i)\bnow i (understand|see|get it)\b`,
		`(?i)\bbuilt\b`,
		`(?i)\bcreated\b`,
		`(?i)\bimplemented\b`,
		`(?i)\bshipped\b`,
		`(?i)\blaunched\b`,
		`(?i)\bdeployed\b`,
		`(?i)\breleased\b`,
		`(?i)\bprototype\b`,
		`(?i)\bproof of concept\b`,
		`(?i)\bdemo\b`,
		`(?i)\bversion \d`,
		`(?i)\bv\d+\.\d+`,
		`(?i)\d+x (compression|faster|slower|better|improvement|reduction)`,
		`(?i)\d+% (reduction|improvement|faster|better|smaller)`,
	}

	problemMarkerPatterns = []string{
		`(?i)\b(bug|error|crash|fail|broke|broken|issue|problem)\b`,
		`(?i)\bdoesn'?t work\b`,
		`(?i)\bnot working\b`,
		`(?i)\bwon'?t\b.*\bwork\b`,
		`(?i)\bkeeps? (failing|crashing|breaking|erroring)\b`,
		`(?i)\broot cause\b`,
		`(?i)\bthe (problem|issue|bug) (is|was)\b`,
		`(?i)\bturns out\b.*\b(was|because|due to)\b`,
		`(?i)\bthe fix (is|was)\b`,
		`(?i)\bworkaround\b`,
		`(?i)\bthat'?s why\b`,
		`(?i)\bthe reason it\b`,
		`(?i)\bfixed (it |the |by )\b`,
		`(?i)\bsolution (is|was)\b`,
		`(?i)\bresolved\b`,
		`(?i)\bpatched\b`,
		`(?i)\bthe answer (is|was)\b`,
		`(?i)\b(had|need) to\b.*\binstead\b`,
	}

	emotionMarkerPatterns = []string{
		`(?i)\blove\b`,
		`(?i)\bscared\b`,
		`(?i)\bafraid\b`,
		`(?i)\bproud\b`,
		`(?i)\bhurt\b`,
		`(?i)\bhappy\b`,
		`(?i)\bsad\b`,
		`(?i)\bcry\b`,
		`(?i)\bcrying\b`,
		`(?i)\bmiss\b`,
		`(?i)\bsorry\b`,
		`(?i)\bgrateful\b`,
		`(?i)\bangry\b`,
		`(?i)\bworried\b`,
		`(?i)\blonely\b`,
		`(?i)\bbeautiful\b`,
		`(?i)\bamazing\b`,
		`(?i)\bwonderful\b`,
		`(?i)i feel`,
		`(?i)i'm scared`,
		`(?i)i love you`,
		`(?i)i'm sorry`,
		`(?i)i can't`,
		`(?i)i wish`,
		`(?i)i miss`,
		`(?i)i need`,
		`(?i)never told anyone`,
		`(?i)nobody knows`,
		`\*[^*]+\*`,
	}

	resolutionPatternStrings = []string{
		`(?i)\bfixed\b`,
		`(?i)\bsolved\b`,
		`(?i)\bresolved\b`,
		`(?i)\bpatched\b`,
		`(?i)\bgot it working\b`,
		`(?i)\bit works\b`,
		`(?i)\bnailed it\b`,
		`(?i)\bfigured (it )?out\b`,
		`(?i)\bthe (fix|answer|solution)\b`,
	}

	codeLinePatternStrings = []string{
		`^\s*[\$#]\s`,
		`^\s*(cd|source|echo|export|pip|npm|git|python|bash|curl|wget|mkdir|rm|cp|mv|ls|cat|grep|find|chmod|sudo|brew|docker)\s`,
		"^\\s*```",
		`^\s*(import|from|def|class|function|const|let|var|return)\s`,
		`^\s*[A-Z_]{2,}=`,
		`^\s*\|`,
		`^\s*[-]{2,}`,
		`^\s*[{}\[\]]\s*$`,
		`^\s*(if|for|while|try|except|elif|else:)\b`,
		`^\s*\w+\.\w+\(`,
		`^\s*\w+ = \w+\.\w+`,
	}

	turnPatternStrings = []string{
		`^>\s`,
		`(?i)^(Human|User|Q)\s*:`,
		`(?i)^(Assistant|AI|A|Claude|ChatGPT)\s*:`,
	}
)

// Compiled marker slices. Built at package init; never compiled per-call.
var (
	decisionMarkers   []*regexp.Regexp
	preferenceMarkers []*regexp.Regexp
	milestoneMarkers  []*regexp.Regexp
	problemMarkers    []*regexp.Regexp
	emotionMarkers    []*regexp.Regexp

	// allMarkers keys on the public ClassificationType constants (unified on
	// "emotion", unlike the Python oracle's "emotional" string).
	allMarkers map[ClassificationType][]*regexp.Regexp

	resolutionPatterns []*regexp.Regexp
	codeLinePatterns   []*regexp.Regexp
	turnPatterns       []*regexp.Regexp
)

// wordRe tokenises a string on word boundaries for sentiment detection.
var wordRe = regexp.MustCompile(`(?i)\b\w+\b`)

// Positive / negative sentiment word sets — ported from
// general_extractor.py:176-237.
var (
	positiveWords = map[string]bool{
		"pride": true, "proud": true, "joy": true, "happy": true, "love": true,
		"loving": true, "beautiful": true, "amazing": true, "wonderful": true,
		"incredible": true, "fantastic": true, "brilliant": true, "perfect": true,
		"excited": true, "thrilled": true, "grateful": true, "warm": true,
		"breakthrough": true, "success": true, "works": true, "working": true,
		"solved": true, "fixed": true, "nailed": true, "heart": true, "hug": true,
		"precious": true, "adore": true,
	}

	negativeWords = map[string]bool{
		"bug": true, "error": true, "crash": true, "crashing": true, "crashed": true,
		"fail": true, "failed": true, "failing": true, "failure": true, "broken": true,
		"broke": true, "breaking": true, "breaks": true, "issue": true, "problem": true,
		"wrong": true, "stuck": true, "blocked": true, "unable": true, "impossible": true,
		"missing": true, "terrible": true, "horrible": true, "awful": true, "worse": true,
		"worst": true, "panic": true, "disaster": true, "mess": true,
	}
)

func mustCompileAll(pats []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(pats))
	for i, p := range pats {
		out[i] = regexp.MustCompile(p)
	}
	return out
}

func init() {
	decisionMarkers = mustCompileAll(decisionMarkerPatterns)
	preferenceMarkers = mustCompileAll(preferenceMarkerPatterns)
	milestoneMarkers = mustCompileAll(milestoneMarkerPatterns)
	problemMarkers = mustCompileAll(problemMarkerPatterns)
	emotionMarkers = mustCompileAll(emotionMarkerPatterns)

	allMarkers = map[ClassificationType][]*regexp.Regexp{
		TypeDecision:   decisionMarkers,
		TypePreference: preferenceMarkers,
		TypeMilestone:  milestoneMarkers,
		TypeProblem:    problemMarkers,
		TypeEmotion:    emotionMarkers,
	}

	resolutionPatterns = mustCompileAll(resolutionPatternStrings)
	codeLinePatterns = mustCompileAll(codeLinePatternStrings)
	turnPatterns = mustCompileAll(turnPatternStrings)
}
