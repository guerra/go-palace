//go:build ORT

package embed

import (
	hugot "github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/options"
)

func newHugotSession() (*hugot.Session, error) {
	return hugot.NewORTSession(options.WithOnnxLibraryPath("/usr/local/lib"))
}

func needsGoMLXTruncation() bool { return false }
