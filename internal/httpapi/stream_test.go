package httpapi

import "testing"

func TestOfferReplacesPendingFrame(t *testing.T) {
	frames := make(chan []byte, 1)
	offer(frames, []byte("old"))
	offer(frames, []byte("new"))
	if got := string(<-frames); got != "new" {
		t.Fatalf("pending frame = %s", got)
	}
}
