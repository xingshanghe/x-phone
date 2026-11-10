package service

import "testing"

func TestCleanSerial(t *testing.T) {
	serial, port, err := CleanSerial("  adb connect 192.168.1.8:5555 ")
	if err != nil || serial != "192.168.1.8:5555" || port != 5555 {
		t.Fatalf("serial=%s port=%d err=%v", serial, port, err)
	}
	if _, _, err := CleanSerial(""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CleanSerial("emulator-5554"); err == nil {
		t.Fatal("expected host:port")
	}
}
