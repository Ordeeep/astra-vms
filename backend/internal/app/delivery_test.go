package app

import "testing"

func TestDeliveryAdaptsWithoutChangingSourceCodec(t *testing.T) {
	for _, tc := range []struct {
		codecs []string
		force  bool
		mode   string
	}{
		{nil, false, "compatible"},
		{[]string{"h264"}, false, "compatible"},
		{[]string{"video/H265", "h264"}, false, "native"},
		{[]string{"hevc"}, true, "converted"},
	} {
		if got := deliveryMode(tc.codecs, tc.force); got != tc.mode {
			t.Fatalf("delivery = %s, want %s", got, tc.mode)
		}
	}
	if archiveNeedsConversion("avc1.640028", "compatible") || archiveNeedsConversion("hvc1.1.6.L120.B0", "native") {
		t.Fatal("compatible video should retain its original encoding")
	}
	if !archiveNeedsConversion("hvc1.1.6.L120.B0", "compatible") || !archiveNeedsConversion("avc1.640028", "converted") {
		t.Fatal("unsupported clients and failed decoders need the compatible encoding")
	}
}
