//go:build !ORT

package embed

import hugot "github.com/knights-analytics/hugot"

func newHugotSession() (*hugot.Session, error) {
	return hugot.NewGoSession()
}

func needsGoMLXTruncation() bool { return true }
