package httpapi_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xingshanghe/x-phone/internal/auth"
	"github.com/xingshanghe/x-phone/internal/blob"
	"github.com/xingshanghe/x-phone/internal/config"
	"github.com/xingshanghe/x-phone/internal/database"
	"github.com/xingshanghe/x-phone/internal/httpapi"
	"github.com/xingshanghe/x-phone/internal/model"
	"github.com/xingshanghe/x-phone/internal/service"
)

func TestControlPlane(t *testing.T) {
	engine, svc := newEngine(t)
	token := login(t, engine)

	rec := call(t, engine, http.MethodGet, "/api/v1/instances", token, nil)
	if rec.Code != http.StatusUnauthorized && token == "" {
		t.Fatal("expected unauthorized without token")
	}
	unauth := call(t, engine, http.MethodGet, "/api/v1/instances", "", nil)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("status %d body %s", unauth.Code, unauth.Body.String())
	}
	assertEnvelope(t, unauth.Body.Bytes(), false)
	if bytes.Contains(unauth.Body.Bytes(), []byte("agentToken")) || bytes.Contains(unauth.Body.Bytes(), []byte("tokenHash")) {
		t.Fatalf("unauthorized body leaked credentials: %s", unauth.Body.String())
	}

	lab := registerHost(t, engine, token, "lab", true)
	limited := registerHost(t, engine, token, "limited", false)

	first := createInstance(t, engine, token, "pixel-a", lab.ID, "pixel_8")
	second := createInstance(t, engine, token, "pixel-b", lab.ID, "samsung_s23")
	if first["status"] != "creating" || second["status"] != "creating" {
		t.Fatalf("instances = %#v %#v", first["status"], second["status"])
	}
	only := createInstance(t, engine, token, "only", limited.ID, "pixel_8")
	if only["id"] == "" {
		t.Fatal("first instance on a host without binderfs should be accepted")
	}
	denied := callJSON(t, engine, http.MethodPost, "/api/v1/instances", token, map[string]any{
		"name": "extra", "hostId": limited.ID, "profileId": "pixel_8",
	})
	if denied.Code != http.StatusConflict {
		t.Fatalf("second instance status %d body %s", denied.Code, denied.Body.String())
	}

	script := callJSON(t, engine, http.MethodPost, "/api/v1/instances/"+first["id"].(string)+"/tasks", token, map[string]any{"type": "script"})
	if script.Code != http.StatusNotImplemented {
		t.Fatalf("script status %d", script.Code)
	}
	var running int64
	if err := svc.DB.Model(&model.InstanceTask{}).Where("type = ? AND status = ?", "script", "running").Count(&running).Error; err != nil {
		t.Fatal(err)
	}
	if running != 0 {
		t.Fatalf("running script tasks = %d", running)
	}

	tasks := agentCall(t, engine, http.MethodGet, "/api/v1/agent/tasks", lab.Token, nil)
	items := tasks["items"].([]any)
	var task map[string]any
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["instanceId"] == first["id"] {
			task = item
		}
	}
	if task == nil {
		t.Fatalf("tasks %#v", tasks)
	}
	claimed := agentCall(t, engine, http.MethodPost, "/api/v1/agent/tasks/"+task["id"].(string)+"/claim", lab.Token, map[string]any{})
	if claimed["status"] != "running" {
		t.Fatalf("claim %#v", claimed)
	}
	done := agentCall(t, engine, http.MethodPost, "/api/v1/agent/tasks/"+task["id"].(string)+"/complete", lab.Token, map[string]any{
		"ok": true, "adbPort": 18120, "containerId": "abc", "report": map[string]any{"ok": true},
	})
	if done["status"] != "succeeded" {
		t.Fatalf("complete %#v", done)
	}
	got := callJSON(t, engine, http.MethodGet, "/api/v1/instances/"+first["id"].(string), token, nil)
	body := decode(t, got.Body.Bytes())
	instance := body["data"].(map[string]any)
	if instance["status"] != "running" {
		t.Fatalf("instance %#v", instance)
	}

	upload := uploadAPK(t, engine, token, first["id"].(string), "broken")
	installID := upload["id"].(string)
	agentCall(t, engine, http.MethodPost, "/api/v1/agent/tasks/"+installID+"/claim", lab.Token, map[string]any{})
	failed := agentCall(t, engine, http.MethodPost, "/api/v1/agent/tasks/"+installID+"/complete", lab.Token, map[string]any{
		"ok": false, "error": "adb install failed",
	})
	if failed["status"] != "failed" {
		t.Fatalf("install %#v", failed)
	}
	got = callJSON(t, engine, http.MethodGet, "/api/v1/instances/"+first["id"].(string), token, nil)
	instance = decode(t, got.Body.Bytes())["data"].(map[string]any)
	if instance["status"] != "running" {
		t.Fatalf("install failure changed status %#v", instance["status"])
	}
}

func TestAttachADBInstance(t *testing.T) {
	engine, svc := newEngine(t)
	token := login(t, engine)
	limited := registerHost(t, engine, token, "limited", false)
	createInstance(t, engine, token, "only", limited.ID, "pixel_8")

	denied := callJSON(t, engine, http.MethodPost, "/api/v1/instances", token, map[string]any{
		"name": "extra", "hostId": limited.ID, "profileId": "pixel_8",
	})
	if denied.Code != http.StatusConflict {
		t.Fatalf("second container status %d body %s", denied.Code, denied.Body.String())
	}

	attached := callJSON(t, engine, http.MethodPost, "/api/v1/instances", token, map[string]any{
		"name": "handset", "hostId": limited.ID, "adbSerial": "adb connect 192.168.1.8:5555",
	})
	if attached.Code != http.StatusOK {
		t.Fatalf("attach %d %s", attached.Code, attached.Body.String())
	}
	instance := decode(t, attached.Body.Bytes())["data"].(map[string]any)
	if instance["runtimeKind"] != "android-adb" || instance["adbSerial"] != "192.168.1.8:5555" {
		t.Fatalf("instance %#v", instance)
	}
	var task model.InstanceTask
	if err := svc.DB.Where("instance_id = ? AND type = ?", instance["id"], "attach").First(&task).Error; err != nil {
		t.Fatal(err)
	}
	claimed := agentCall(t, engine, http.MethodPost, "/api/v1/agent/tasks/"+task.ID+"/claim", limited.Token, map[string]any{})
	if claimed["status"] != "running" {
		t.Fatalf("claim %#v", claimed)
	}
	done := agentCall(t, engine, http.MethodPost, "/api/v1/agent/tasks/"+task.ID+"/complete", limited.Token, map[string]any{
		"ok": true, "adbPort": 5555, "serial": "192.168.1.8:5555", "width": 1080, "height": 2400,
	})
	if done["status"] != "succeeded" {
		t.Fatalf("complete %#v", done)
	}
	got := callJSON(t, engine, http.MethodGet, "/api/v1/instances/"+instance["id"].(string), token, nil)
	stored := decode(t, got.Body.Bytes())["data"].(map[string]any)
	if stored["status"] != "running" || stored["displayWidth"] != float64(1080) || stored["displayHeight"] != float64(2400) {
		t.Fatalf("stored %#v", stored)
	}
}

func newEngine(t *testing.T) (*httpapi.Engine, *service.Services) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		Driver: "sqlite", DSN: filepath.Join(dir, "t.db"), JWTSecret: "test-secret",
		AdminUser: "admin", AdminPassword: "admin123", ProfilesDir: profilesDir(t), DataDir: dir,
	}
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	signer := auth.NewSigner(cfg.JWTSecret)
	svc, err := service.New(db, cfg, blob.NewFile(filepath.Join(dir, "blobs")), signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Auth.EnsureAdmin(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Profiles.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	return httpapi.NewEngine(svc, signer), svc
}

func profilesDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "profiles")
}

func login(t *testing.T, engine *httpapi.Engine) string {
	t.Helper()
	rec := callJSON(t, engine, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %d %s", rec.Code, rec.Body.String())
	}
	data := decode(t, rec.Body.Bytes())["data"].(map[string]any)
	return data["accessToken"].(string)
}

type registeredHost struct {
	ID    string
	Token string
}

func registerHost(t *testing.T, engine *httpapi.Engine, token, name string, binder bool) registeredHost {
	t.Helper()
	rec := callJSON(t, engine, http.MethodPost, "/api/v1/hosts", token, map[string]any{"name": name, "addr": "127.0.0.1", "capacity": 4})
	if rec.Code != http.StatusOK {
		t.Fatalf("host %s", rec.Body.String())
	}
	data := decode(t, rec.Body.Bytes())["data"].(map[string]any)
	agent := data["agentToken"].(string)
	host := data["host"].(map[string]any)
	beat := agentCall(t, engine, http.MethodPost, "/api/v1/agent/heartbeat", agent, map[string]any{"binderfs": binder, "capacity": 4, "runningCount": 0})
	if beat["status"] != "online" {
		t.Fatalf("heartbeat %#v", beat)
	}
	return registeredHost{ID: host["id"].(string), Token: agent}
}

func createInstance(t *testing.T, engine *httpapi.Engine, token, name, hostID, profile string) map[string]any {
	t.Helper()
	rec := callJSON(t, engine, http.MethodPost, "/api/v1/instances", token, map[string]any{"name": name, "hostId": hostID, "profileId": profile})
	if rec.Code != http.StatusOK {
		t.Fatalf("create %s: %s", name, rec.Body.String())
	}
	return decode(t, rec.Body.Bytes())["data"].(map[string]any)
}

func uploadAPK(t *testing.T, engine *httpapi.Engine, token, instanceID, pkg string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	file, err := writer.CreateFormFile("file", "app.apk")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("not-a-real-apk"))
	_ = writer.WriteField("packageName", pkg)
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+instanceID+"/apk", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	engine.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload %s", rec.Body.String())
	}
	return decode(t, rec.Body.Bytes())["data"].(map[string]any)
}

func agentCall(t *testing.T, engine *httpapi.Engine, method, path, token string, body any) map[string]any {
	t.Helper()
	rec := callJSON(t, engine, method, path, token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s -> %d %s", method, path, rec.Code, rec.Body.String())
	}
	return decode(t, rec.Body.Bytes())["data"].(map[string]any)
}

func callJSON(t *testing.T, engine *httpapi.Engine, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(buf)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	engine.Router.ServeHTTP(rec, req)
	return rec
}

func call(t *testing.T, engine *httpapi.Engine, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	engine.Router.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertEnvelope(t *testing.T, raw []byte, success bool) {
	t.Helper()
	body := decode(t, raw)
	if body["success"] != success || body["code"] == nil || body["message"] == nil {
		t.Fatalf("envelope %#v", body)
	}
}
