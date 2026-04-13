//go:build ORT

package embed

import (
	"os"

	hugot "github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/options"
)

func newHugotSession() (*hugot.Session, error) {
	libPath := os.Getenv("ONNXRUNTIME_LIB_PATH")
	if libPath == "" {
		libPath = "/usr/local/lib"
	}
	return hugot.NewORTSession(options.WithOnnxLibraryPath(libPath))
}

func needsGoMLXTruncation() bool { return false }
