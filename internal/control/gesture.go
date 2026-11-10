package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"
)

const (
	defaultSwipe = 300
	longPress    = 800
	maxTextRunes = 200
)

// Target is one device the hub will encode a gesture for.
type Target struct {
	InstanceID string
	Width      int
	Height     int
}

// Gesture is a pointer or key action in normalized coordinates.
type Gesture struct {
	Kind     string
	XR       float64
	YR       float64
	X2R      float64
	Y2R      float64
	HasEnd   bool
	Action   string
	Key      string
	Text     string
	Duration int
}

// Parse reports whether data is a control gesture.
// A non-gesture (a video frame, for example) returns false and a nil error.
func Parse(data []byte) (Gesture, bool, error) {
	var body wire
	if err := json.Unmarshal(data, &body); err != nil || !isControl(body.Type) {
		return Gesture{}, false, nil
	}
	gesture, normalized, err := body.gesture()
	if !normalized {
		return Gesture{}, false, nil
	}
	return gesture, true, err
}

// VideoSize reads a node report of the current frame dimensions.
func VideoSize(data []byte) (int, int, bool) {
	if !bytes.Contains(data, []byte(`"video-size"`)) {
		return 0, 0, false
	}
	var body struct {
		Type   string `json:"type"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if json.Unmarshal(data, &body) != nil || body.Type != "video-size" {
		return 0, 0, false
	}
	if body.Width < 1 || body.Height < 1 || body.Width > 10000 || body.Height > 10000 {
		return 0, 0, false
	}
	return body.Width, body.Height, true
}

// Encode writes the device command for one target's current video size.
func (g Gesture) Encode(width, height int) ([]byte, error) {
	payload := map[string]any{"type": g.Kind}
	if g.needsPoint() {
		x, y, err := point(g.XR, g.YR, width, height)
		if err != nil {
			return nil, err
		}
		payload["x"] = x
		payload["y"] = y
		if g.HasEnd {
			x2, y2, err := point(g.X2R, g.Y2R, width, height)
			if err != nil {
				return nil, err
			}
			payload["x2"] = x2
			payload["y2"] = y2
			duration := g.Duration
			if duration == 0 {
				duration = defaultSwipe
			}
			payload["duration"] = duration
		}
		if g.Kind == "longpress" {
			payload["duration"] = longPress
		}
	}
	if g.Kind == "pointer" {
		payload["action"] = g.Action
	}
	if g.Kind == "key" {
		payload["key"] = g.Key
	}
	if g.Kind == "text" {
		payload["text"] = g.Text
	}
	return json.Marshal(payload)
}

func (g Gesture) needsPoint() bool {
	switch g.Kind {
	case "tap", "swipe", "pointer", "longpress":
		return true
	default:
		return false
	}
}

func point(xr, yr float64, width, height int) (int, int, error) {
	if width < 1 || height < 1 {
		return 0, 0, fmt.Errorf("video size is unknown")
	}
	x, err := scale(xr, width)
	if err != nil {
		return 0, 0, err
	}
	y, err := scale(yr, height)
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

func scale(ratio float64, size int) (int, error) {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
		return 0, fmt.Errorf("coordinate out of range")
	}
	value := int(math.Round(ratio * float64(size)))
	if value >= size {
		value = size - 1
	}
	if value < 0 {
		value = 0
	}
	return value, nil
}

type wire struct {
	Type     string   `json:"type"`
	XR       *float64 `json:"xr"`
	YR       *float64 `json:"yr"`
	X2R      *float64 `json:"x2r"`
	Y2R      *float64 `json:"y2r"`
	X        *float64 `json:"x"`
	Y        *float64 `json:"y"`
	X2       *float64 `json:"x2"`
	Y2       *float64 `json:"y2"`
	Width    *float64 `json:"width"`
	Height   *float64 `json:"height"`
	Action   string   `json:"action"`
	Key      string   `json:"key"`
	Text     string   `json:"text"`
	Duration int      `json:"duration"`
}

func isControl(kind string) bool {
	switch kind {
	case "tap", "swipe", "pointer", "longpress", "key", "text":
		return true
	default:
		return false
	}
}

func (body wire) gesture() (Gesture, bool, error) {
	gesture := Gesture{Kind: body.Type, Action: body.Action, Key: body.Key, Text: body.Text, Duration: body.Duration}
	if gesture.needsPoint() {
		xr, yr, ok, err := body.ratio(body.XR, body.YR, body.X, body.Y)
		if err != nil {
			return Gesture{}, true, err
		}
		if !ok {
			return Gesture{}, false, nil
		}
		gesture.XR, gesture.YR = xr, yr
	}
	if body.Type == "swipe" {
		x2, y2, ok, err := body.ratio(body.X2R, body.Y2R, body.X2, body.Y2)
		if err != nil {
			return Gesture{}, true, err
		}
		if !ok {
			return Gesture{}, false, nil
		}
		gesture.X2R, gesture.Y2R, gesture.HasEnd = x2, y2, true
		if gesture.Duration < 0 || gesture.Duration > 5000 {
			return Gesture{}, true, fmt.Errorf("swipe duration out of range")
		}
	}
	switch body.Type {
	case "pointer":
		switch body.Action {
		case "down", "move", "up":
		default:
			return Gesture{}, true, fmt.Errorf("pointer action must be down, move, or up")
		}
	case "key":
		if _, ok := allowedKeys[body.Key]; !ok {
			return Gesture{}, true, fmt.Errorf("unsupported key")
		}
	case "text":
		if body.Text == "" || utf8.RuneCountInString(body.Text) > maxTextRunes || !printable(body.Text) {
			return Gesture{}, true, fmt.Errorf("text must be 1-200 printable characters")
		}
	}
	return gesture, true, nil
}

func (body wire) ratio(xr, yr, x, y *float64) (float64, float64, bool, error) {
	if xr != nil && yr != nil {
		if err := checkRatio(*xr, *yr); err != nil {
			return 0, 0, false, err
		}
		return *xr, *yr, true, nil
	}
	if x != nil && y != nil && body.Width != nil && body.Height != nil && *body.Width > 0 && *body.Height > 0 {
		left := *x / *body.Width
		top := *y / *body.Height
		if err := checkRatio(left, top); err != nil {
			return 0, 0, false, err
		}
		return left, top, true, nil
	}
	return 0, 0, false, nil
}

func checkRatio(xr, yr float64) error {
	if _, err := scale(xr, 2); err != nil {
		return err
	}
	_, err := scale(yr, 2)
	return err
}

func printable(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

var allowedKeys = map[string]struct{}{
	"HOME": {}, "BACK": {}, "ENTER": {}, "APP_SWITCH": {},
	"VOLUME_UP": {}, "VOLUME_DOWN": {}, "DEL": {}, "MENU": {}, "POWER": {},
}
