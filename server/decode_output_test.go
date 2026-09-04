package server

import (
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestDecodeCommandOutputKeepsUTF8(t *testing.T) {
	const text = "以太网适配器 Ethernet"
	if got := decodeCommandOutput([]byte(text)); got != text {
		t.Fatalf("utf-8 output = %q, want %q", got, text)
	}
}

func TestDecodeCommandOutputDecodesGB18030(t *testing.T) {
	const text = "以太网适配器"
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatalf("encode GB18030: %v", err)
	}
	if got := decodeCommandOutput(encoded); got != text {
		t.Fatalf("gb18030 output = %q, want %q", got, text)
	}
}

func TestDecodeCommandOutputStripsUTF8BOM(t *testing.T) {
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte("lite-agent-ok")...)
	if got := decodeCommandOutput(raw); got != "lite-agent-ok" {
		t.Fatalf("bom output = %q", got)
	}
}
