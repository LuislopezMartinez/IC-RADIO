// Package fonts embeds the open-licensed typefaces used by IC-MIC.
package fonts

import _ "embed"

//go:embed Inter-Regular.ttf
var InterRegular []byte

//go:embed Inter-SemiBold.ttf
var InterSemiBold []byte

//go:embed rajdhani/Rajdhani-Regular.ttf
var RajdhaniRegular []byte

//go:embed rajdhani/Rajdhani-SemiBold.ttf
var RajdhaniSemiBold []byte

//go:embed rajdhani/Rajdhani-Bold.ttf
var RajdhaniBold []byte

//go:embed black-ops-one/BlackOpsOne-Regular.ttf
var BlackOpsOne []byte
