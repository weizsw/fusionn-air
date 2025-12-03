package version

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Version holds the current build version. Override with
// -ldflags "-X github.com/fusionn-air/internal/version.Version=v1.2.3".
var Version = "dev"

const (
	separator = "────────────────────────────────────────────────────────────"
	banner    = `
   ___           _                              _
  / _|_   _ ___(_) ___  _ __  _ __         __ _(_)_ __
 | |_| | | / __| |/ _ \| '_ \| '_ \ _____ / _' | | '__|
 |  _| |_| \__ \ | (_) | | | | | | |_____| (_| | | |
 |_|  \__,_|___/_|\___/|_| |_|_| |_|      \__,_|_|_|
`
)

// Banner returns the ASCII-art project banner.
func Banner() string {
	return strings.Trim(banner, "\n")
}

// PrintBanner writes the decorated banner and version info to w (stdout if nil).
func PrintBanner(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, separator)
	fmt.Fprintln(w, Banner())
	fmt.Fprintf(w, "\n  fusionn-air %s\n", Version)
	fmt.Fprintf(w, "  Automated Media Request Service\n")
	fmt.Fprintln(w, separator)
	fmt.Fprintln(w)
}
