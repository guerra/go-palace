// Package dialect implements the AAAK structured symbolic summary format.
// Phase D: stub only. Full encoder/decoder deferred to compress verb phase.
package dialect

const ProtocolVersion = "AAAK-1.0"

// EmotionCodes maps emotion names to their compact codes.
var EmotionCodes = map[string]string{
	"vulnerability":        "vul",
	"vulnerable":           "vul",
	"joy":                  "joy",
	"joyful":               "joy",
	"fear":                 "fear",
	"mild_fear":            "fear",
	"trust":                "trust",
	"trust_building":       "trust",
	"grief":                "grief",
	"raw_grief":            "grief",
	"wonder":               "wonder",
	"philosophical_wonder": "wonder",
	"rage":                 "rage",
	"anger":                "rage",
	"love":                 "love",
	"devotion":             "love",
	"hope":                 "hope",
	"despair":              "despair",
	"hopelessness":         "despair",
	"peace":                "peace",
	"relief":               "relief",
	"humor":                "humor",
	"dark_humor":           "humor",
	"tenderness":           "tender",
	"raw_honesty":          "raw",
	"brutal_honesty":       "raw",
	"self_doubt":           "doubt",
	"anxiety":              "anx",
	"exhaustion":           "exhaust",
	"conviction":           "convict",
	"quiet_passion":        "passion",
	"warmth":               "warmth",
	"curiosity":            "curious",
	"gratitude":            "grat",
	"frustration":          "frust",
	"confusion":            "confuse",
	"satisfaction":         "satis",
	"excitement":           "excite",
	"determination":        "determ",
	"surprise":             "surprise",
}

// FlagSignals maps keywords to their flag categories.
var FlagSignals = map[string]string{
	"decided":            "DECISION",
	"chose":              "DECISION",
	"switched":           "DECISION",
	"migrated":           "DECISION",
	"replaced":           "DECISION",
	"instead of":         "DECISION",
	"because":            "DECISION",
	"founded":            "ORIGIN",
	"created":            "ORIGIN",
	"started":            "ORIGIN",
	"born":               "ORIGIN",
	"launched":           "ORIGIN",
	"first time":         "ORIGIN",
	"core":               "CORE",
	"fundamental":        "CORE",
	"essential":          "CORE",
	"principle":          "CORE",
	"belief":             "CORE",
	"always":             "CORE",
	"never forget":       "CORE",
	"turning point":      "PIVOT",
	"changed everything": "PIVOT",
	"realized":           "PIVOT",
	"breakthrough":       "PIVOT",
	"epiphany":           "PIVOT",
	"api":                "TECHNICAL",
	"database":           "TECHNICAL",
	"architecture":       "TECHNICAL",
	"deploy":             "TECHNICAL",
	"infrastructure":     "TECHNICAL",
	"algorithm":          "TECHNICAL",
	"framework":          "TECHNICAL",
	"server":             "TECHNICAL",
	"config":             "TECHNICAL",
}

// Dialect holds entity codes and skip lists for AAAK encoding.
type Dialect struct {
	EntityCodes map[string]string
	SkipNames   []string
}

// New creates a Dialect with the given entity codes and skip names.
func New(entities map[string]string, skipNames []string) *Dialect {
	return &Dialect{EntityCodes: entities, SkipNames: skipNames}
}

// Compress is a stub that returns text unchanged. Full implementation
// deferred to the compress verb phase.
func (d *Dialect) Compress(text string) string {
	return text
}
