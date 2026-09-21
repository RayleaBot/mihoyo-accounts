// Package plugin carries info.json into the binary, so help and version
// replies read the same manifest the host installs.
package plugin

import _ "embed"

//go:embed info.json
var Info []byte
