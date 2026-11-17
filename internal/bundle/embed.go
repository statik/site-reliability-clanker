//go:build bundle

package bundle

import _ "embed"

//go:embed bundle.tar.gz
var embedded []byte

func init() { archive = embedded }
