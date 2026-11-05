package app

import "strings"

// Browser capabilities describe delivery only; they never change the source or
// its recording codec. Unknown clients receive the conservative H.264 route.
func browserHasHEVC(codecs []string) bool {
	for _, codec := range codecs {
		switch strings.ToLower(strings.TrimSpace(codec)) {
		case "h265", "hevc", "video/h265", "video/hevc":
			return true
		}
	}
	return false
}

func deliveryMode(codecs []string, force bool) string {
	if force {
		return "converted"
	}
	if browserHasHEVC(codecs) {
		return "native"
	}
	return "compatible"
}

func archiveNeedsConversion(codec, mode string) bool {
	if mode == "converted" {
		return true
	}
	codec = strings.ToLower(codec)
	return mode == "compatible" && (strings.HasPrefix(codec, "hvc1") || strings.HasPrefix(codec, "hev1") || codec == "hevc" || codec == "h265")
}
