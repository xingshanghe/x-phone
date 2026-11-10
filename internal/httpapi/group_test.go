package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xingshanghe/x-phone/internal/control"
	"github.com/xingshanghe/x-phone/internal/model"
	"github.com/xingshanghe/x-phone/internal/service"
)

func TestGroupFanoutScalesAndLocks(t *testing.T) {
	engine, svc := newEngine(t)
	token := login(t, engine)
	lab := registerHost(t, engine, token, "lab-group", true)
	first := createInstance(t, engine, token, "pixel-a", lab.ID, "pixel_8")
	second := createInstance(t, engine, token, "pixel-b", lab.ID, "pixel_8")
	other := createInstance(t, engine, token, "samsung", lab.ID, "samsung_s23")
	firstID := first["id"].(string)
	secondID := second["id"].(string)

	mixed := callJSON(t, engine, http.MethodPost, "/api/v1/groups", token, map[string]any{
		"name": "mixed", "instanceIds": []string{firstID, other["id"].(string)},
	})
	if mixed.Code != http.StatusBadRequest {
		t.Fatalf("mixed profile status %d body %s", mixed.Code, mixed.Body.String())
	}
	created := callJSON(t, engine, http.MethodPost, "/api/v1/groups", token, map[string]any{
		"name": "wall", "instanceIds": []string{firstID, secondID},
	})
	if created.Code != http.StatusOK {
		t.Fatalf("create group %d %s", created.Code, created.Body.String())
	}
	group := decode(t, created.Body.Bytes())["data"].(map[string]any)
	groupID := group["id"].(string)
	if group["profileId"] != "pixel_8" {
		t.Fatalf("group %#v", group)
	}
	again := callJSON(t, engine, http.MethodPost, "/api/v1/groups", token, map[string]any{
		"name": "again", "instanceIds": []string{firstID, secondID},
	})
	if again.Code != http.StatusConflict {
		t.Fatalf("duplicate membership status %d", again.Code)
	}
	early := callJSON(t, engine, http.MethodPost, "/api/v1/groups/"+groupID+"/batch", token, map[string]any{"type": "start"})
	if early.Code != http.StatusConflict {
		t.Fatalf("batch before stopped status %d %s", early.Code, early.Body.String())
	}

	server := httptest.NewServer(engine.Router)
	t.Cleanup(server.Close)
	agentA := dialWS(t, server.URL, "/api/v1/agent/stream?instanceId="+firstID+"&token="+lab.Token)
	agentB := dialWS(t, server.URL, "/api/v1/agent/stream?instanceId="+secondID+"&token="+lab.Token)
	controlSocket := dialWS(t, server.URL, "/api/v1/groups/"+groupID+"/control?token="+token)

	if err := controlSocket.WriteJSON(map[string]any{"type": "tap", "xr": 0.5, "yr": 0.25}); err != nil {
		t.Fatal(err)
	}
	ack := readWS(t, controlSocket)
	if ack["type"] != "fanout" || ack["delivered"] != 2.0 {
		t.Fatalf("ack %#v", ack)
	}
	tapA := readWS(t, agentA)
	tapB := readWS(t, agentB)
	if tapA["x"] != 540.0 || tapA["y"] != 600.0 || tapB["x"] != 540.0 || tapB["y"] != 600.0 {
		t.Fatalf("scaled %#v %#v", tapA, tapB)
	}

	if err := agentA.WriteJSON(map[string]any{"type": "video-size", "width": 720, "height": 1280}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		width, height, ok := engine.Hub.Video(firstID)
		return ok && width == 720 && height == 1280
	})
	if err := controlSocket.WriteJSON(map[string]any{"type": "tap", "xr": 0.5, "yr": 0.25}); err != nil {
		t.Fatal(err)
	}
	_ = readWS(t, controlSocket)
	resized := readWS(t, agentA)
	unchanged := readWS(t, agentB)
	if resized["x"] != 360.0 || resized["y"] != 320.0 {
		t.Fatalf("resized %#v", resized)
	}
	if unchanged["x"] != 540.0 || unchanged["y"] != 600.0 {
		t.Fatalf("profile size %#v", unchanged)
	}

	var account model.Account
	if err := svc.DB.Where("username = ?", "admin").First(&account).Error; err != nil {
		t.Fatal(err)
	}
	targets, err := svc.Groups.Targets(t.Context(), groupID)
	if err != nil {
		t.Fatal(err)
	}
	gesture, ok, err := control.Parse([]byte(`{"type":"tap","xr":0.5,"yr":0.5}`))
	if err != nil || !ok {
		t.Fatal(err)
	}
	_, err = engine.Hub.Fanout("intruder", engine.Hub.ApplyVideo(targets), gesture, time.Now())
	if !errors.Is(err, service.ErrLocked) {
		t.Fatalf("lock err = %v", err)
	}
	if _, err := engine.Hub.Fanout("intruder", engine.Hub.ApplyVideo(targets), gesture, time.Now().Add(controlLockWait)); err != nil {
		t.Fatal(err)
	}

	if err := svc.DB.Model(&model.Instance{}).Where("id IN ?", []string{firstID, secondID}).Update("status", "stopped").Error; err != nil {
		t.Fatal(err)
	}
	batch := callJSON(t, engine, http.MethodPost, "/api/v1/groups/"+groupID+"/batch", token, map[string]any{"type": "start"})
	if batch.Code != http.StatusOK {
		t.Fatalf("batch %d %s", batch.Code, batch.Body.String())
	}
	var pending int64
	if err := svc.DB.Model(&model.InstanceTask{}).Where("type = ? AND status = ?", "start", "pending").Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending != 2 {
		t.Fatalf("start tasks = %d", pending)
	}
}

const controlLockWait = 16 * time.Second

func dialWS(t *testing.T, serverURL, path string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(serverURL, "http")+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readWS(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func waitUntil(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}
