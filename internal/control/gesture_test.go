package control

import (
	"encoding/json"
	"testing"
)

func TestEncodeScalesRatioOntoEachDevice(t *testing.T) {
	gesture, ok, err := Parse([]byte(`{"type":"tap","xr":0.5,"yr":0.25}`))
	if err != nil || !ok {
		t.Fatalf("parse ok=%v err=%v", ok, err)
	}
	first := decode(t, mustEncode(t, gesture, 1080, 2400))
	if first["x"] != 540.0 || first["y"] != 600.0 {
		t.Fatalf("pixel_8 %#v", first)
	}
	second := decode(t, mustEncode(t, gesture, 720, 1280))
	if second["x"] != 360.0 || second["y"] != 320.0 {
		t.Fatalf("override %#v", second)
	}
}

func TestParseAbsolutePixelsUsesReferenceSize(t *testing.T) {
	gesture, ok, err := Parse([]byte(`{"type":"swipe","x":540,"y":600,"x2":108,"y2":120,"width":1080,"height":2400,"duration":400}`))
	if err != nil || !ok {
		t.Fatalf("parse ok=%v err=%v", ok, err)
	}
	body := decode(t, mustEncode(t, gesture, 720, 1280))
	if body["x"] != 360.0 || body["y"] != 320.0 || body["x2"] != 72.0 || body["y2"] != 64.0 || body["duration"] != 400.0 {
		t.Fatalf("rescaled %#v", body)
	}
}

func TestParseRejectsOutOfRangeAndPassesFramesThrough(t *testing.T) {
	if _, ok, err := Parse([]byte(`{"type":"frame","mime":"image/png","data":"abc"}`)); ok || err != nil {
		t.Fatalf("frame should pass through ok=%v err=%v", ok, err)
	}
	if _, ok, err := Parse([]byte(`{"type":"tap","x":10,"y":20}`)); ok || err != nil {
		t.Fatalf("pixel tap without a reference size is not a normalized gesture")
	}
	_, ok, err := Parse([]byte(`{"type":"tap","xr":1.2,"yr":0.2}`))
	if !ok || err == nil {
		t.Fatal("expected a range error")
	}
	_, ok, err = Parse([]byte(`{"type":"key","key":"VOLUME_NOISE"}`))
	if !ok || err == nil {
		t.Fatal("expected an unsupported key")
	}
}

func TestVideoSize(t *testing.T) {
	width, height, ok := VideoSize([]byte(`{"type":"video-size","width":720,"height":1280}`))
	if !ok || width != 720 || height != 1280 {
		t.Fatalf("size %d %d ok=%v", width, height, ok)
	}
	if _, _, ok := VideoSize([]byte(`{"type":"frame","data":"video-size"}`)); ok {
		t.Fatal("frame must not report a video size")
	}
}

func mustEncode(t *testing.T, gesture Gesture, width, height int) []byte {
	t.Helper()
	payload, err := gesture.Encode(width, height)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func decode(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	return body
}
