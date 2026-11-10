package agentrt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Options struct {
	Server     string
	Token      string
	Simulate   bool
	Python     string
	PythonPath string
	Interval   time.Duration
}

type Client struct {
	Options Options
	HTTP    *http.Client
}

func (c *Client) Loop(ctx context.Context) error {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 60 * time.Second}
	}
	if c.Options.Interval == 0 {
		c.Options.Interval = 15 * time.Second
	}
	go c.superviseStreams(ctx)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	heartbeat := time.NewTicker(c.Options.Interval)
	defer heartbeat.Stop()
	if err := c.beat(ctx); err != nil {
		return err
	}
	for {
		if err := c.drain(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-heartbeat.C:
			if err := c.beat(ctx); err != nil {
				return err
			}
		case <-ticker.C:
		}
	}
}

func (c *Client) beat(ctx context.Context) error {
	running, _ := c.running(ctx)
	body := map[string]any{"binderfs": c.Options.Simulate || binderfs(), "capacity": 8, "runningCount": len(running)}
	_, err := c.call(ctx, http.MethodPost, "/api/v1/agent/heartbeat", body)
	return err
}

func (c *Client) drain(ctx context.Context) error {
	payload, err := c.call(ctx, http.MethodGet, "/api/v1/agent/tasks", nil)
	if err != nil {
		return err
	}
	var data struct {
		Items []task `json:"items"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return err
	}
	for _, item := range data.Items {
		if err := c.handle(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

type task struct {
	ID         string `json:"id"`
	InstanceID string `json:"instanceId"`
	Type       string `json:"type"`
	Payload    string `json:"payload"`
}

type taskPayload struct {
	ContainerName string          `json:"containerName"`
	ADBPort       int             `json:"adbPort"`
	Serial        string          `json:"serial"`
	RuntimeKind   string          `json:"runtimeKind"`
	Proxy         string          `json:"proxy"`
	PackageName   string          `json:"packageName"`
	Profile       json.RawMessage `json:"profile"`
	APKKey        string          `json:"apkKey"`
}

func (c *Client) handle(ctx context.Context, item task) error {
	if _, err := c.call(ctx, http.MethodPost, "/api/v1/agent/tasks/"+item.ID+"/claim", map[string]any{}); err != nil {
		return nil
	}
	result := c.execute(ctx, item)
	_, err := c.call(ctx, http.MethodPost, "/api/v1/agent/tasks/"+item.ID+"/complete", result)
	return err
}

func (c *Client) execute(ctx context.Context, item task) map[string]any {
	if c.Options.Simulate {
		return simulateResult(item)
	}
	var payload taskPayload
	_ = json.Unmarshal([]byte(item.Payload), &payload)
	switch item.Type {
	case "create":
		return c.phoneUp(ctx, payload)
	case "attach":
		return c.phone(ctx, "adb-connect", "--serial", payload.Serial)
	case "start":
		if payload.RuntimeKind == "android-adb" {
			return c.phone(ctx, "adb-connect", "--serial", payload.Serial)
		}
		return c.phoneStart(ctx, payload)
	case "stop":
		if payload.RuntimeKind == "android-adb" {
			return c.phone(ctx, "adb-disconnect", "--serial", payload.Serial)
		}
		return c.phone(ctx, "stop", "--name", payload.ContainerName)
	case "delete":
		if payload.RuntimeKind == "android-adb" {
			return c.phone(ctx, "adb-disconnect", "--serial", payload.Serial)
		}
		return c.phone(ctx, "down", "--name", payload.ContainerName)
	case "install":
		return c.install(ctx, item.ID, payload)
	default:
		return map[string]any{"ok": false, "error": "unknown task"}
	}
}

func simulateResult(item task) map[string]any {
	if item.Type == "install" && strings.Contains(item.Payload, `"packageName":"broken"`) {
		return map[string]any{"ok": false, "error": "adb install failed"}
	}
	if item.Type == "stop" || item.Type == "delete" {
		return map[string]any{"ok": true, "log": "simulated " + item.Type}
	}
	if item.Type == "attach" {
		var payload taskPayload
		_ = json.Unmarshal([]byte(item.Payload), &payload)
		return map[string]any{"ok": true, "adbPort": payload.ADBPort, "serial": payload.Serial, "width": 1080, "height": 2400, "log": "simulated attach"}
	}
	report := map[string]any{
		"ok": true,
		"steps": []map[string]any{
			{"name": "identity", "ok": true, "detail": "matched"},
			{"name": "display", "ok": true, "detail": "matched"},
			{"name": "locale", "ok": true, "detail": "matched"},
			{"name": "proxy", "ok": true, "detail": "matched"},
			{"name": "hardware", "ok": true, "detail": "matched"},
		},
		"gaps": []string{"baseband", "play_integrity", "camera", "gps"},
	}
	return map[string]any{"ok": true, "adbPort": 18120, "containerId": "sim-" + item.InstanceID, "report": report, "log": "simulated " + item.Type}
}

func (c *Client) phoneUp(ctx context.Context, payload taskPayload) map[string]any {
	file, err := writeProfile(payload.Profile)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	defer os.Remove(file)
	return c.phone(ctx, "up", "--name", payload.ContainerName, "--profile", file, "--proxy", payload.Proxy)
}

func (c *Client) phoneStart(ctx context.Context, payload taskPayload) map[string]any {
	file, err := writeProfile(payload.Profile)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	defer os.Remove(file)
	serial := fmt.Sprintf("127.0.0.1:%d", payload.ADBPort)
	result := c.phone(ctx, "start", "--name", payload.ContainerName, "--serial", serial, "--profile", file, "--proxy", payload.Proxy)
	result["adbPort"] = payload.ADBPort
	return result
}

func (c *Client) install(ctx context.Context, taskID string, payload taskPayload) map[string]any {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Options.Server+"/api/v1/agent/tasks/"+taskID+"/apk", nil)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.Options.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	file, err := os.CreateTemp("", "xphone-*.apk")
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	defer os.Remove(file.Name())
	if _, err := io.Copy(file, resp.Body); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	_ = file.Close()
	serial := payload.Serial
	if serial == "" {
		serial = fmt.Sprintf("127.0.0.1:%d", payload.ADBPort)
	}
	return c.phone(ctx, "install-apk", "--serial", serial, "--apk", file.Name(), "--package", payload.PackageName)
}

func (c *Client) phone(ctx context.Context, args ...string) map[string]any {
	python := c.Options.Python
	if python == "" {
		python = "python3"
	}
	cmd := exec.CommandContext(ctx, python, append([]string{"-m", "phone"}, args...)...)
	if c.Options.PythonPath != "" {
		cmd.Env = append(os.Environ(), "PYTHONPATH="+c.Options.PythonPath)
	}
	output, err := cmd.CombinedOutput()
	parsed, perr := parseResult(output)
	if perr != nil {
		message := strings.TrimSpace(string(output))
		if err != nil {
			message = err.Error() + ": " + message
		}
		return map[string]any{"ok": false, "error": message, "log": string(output)}
	}
	parsed["log"] = string(output)
	return parsed
}

func parseResult(output []byte) (map[string]any, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil, fmt.Errorf("empty phone output")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeProfile(raw json.RawMessage) (string, error) {
	file, err := os.CreateTemp("", "xphone-profile-*.json")
	if err != nil {
		return "", err
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return "", err
	}
	return file.Name(), file.Close()
}

func (c *Client) call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Options.Server+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Options.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		return nil, fmt.Errorf("%s", envelope.Message)
	}
	return envelope.Data, nil
}

func (c *Client) running(ctx context.Context) ([]map[string]any, error) {
	payload, err := c.call(ctx, http.MethodGet, "/api/v1/agent/instances", nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, err
	}
	return data.Items, nil
}

func binderfs() bool {
	text, err := os.ReadFile("/proc/filesystems")
	if err != nil {
		return false
	}
	return strings.Contains(string(text), "binder")
}

func (c *Client) superviseStreams(ctx context.Context) {
	active := map[string]context.CancelFunc{}
	var mu sync.Mutex
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		items, err := c.running(ctx)
		if err == nil {
			seen := map[string]bool{}
			for _, item := range items {
				id, _ := item["id"].(string)
				if id == "" {
					continue
				}
				seen[id] = true
				mu.Lock()
				_, exists := active[id]
				mu.Unlock()
				if exists {
					continue
				}
				streamCtx, cancel := context.WithCancel(ctx)
				mu.Lock()
				active[id] = cancel
				mu.Unlock()
				go func(instance map[string]any, instanceID string, cancel context.CancelFunc) {
					defer func() {
						cancel()
						mu.Lock()
						if active[instanceID] != nil {
							delete(active, instanceID)
						}
						mu.Unlock()
					}()
					c.stream(streamCtx, instance)
				}(item, id, cancel)
			}
			mu.Lock()
			for id, cancel := range active {
				if !seen[id] {
					cancel()
					delete(active, id)
				}
			}
			mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Client) stream(ctx context.Context, instance map[string]any) {
	id, _ := instance["id"].(string)
	endpoint, err := url.Parse(c.Options.Server + "/api/v1/agent/stream")
	if err != nil {
		return
	}
	query := endpoint.Query()
	query.Set("instanceId", id)
	query.Set("token", c.Options.Token)
	endpoint.RawQuery = query.Encode()
	wsURL := strings.Replace(endpoint.String(), "http", "ws", 1)
	if c.Options.Simulate {
		c.simulateStream(ctx, wsURL)
		return
	}
	serial, _ := instance["adbSerial"].(string)
	if serial == "" {
		port, _ := instance["adbPort"].(float64)
		if port == 0 {
			return
		}
		serial = fmt.Sprintf("127.0.0.1:%d", int(port))
	}
	python := c.Options.Python
	if python == "" {
		python = "python3"
	}
	width, _ := instance["width"].(float64)
	height, _ := instance["height"].(float64)
	cmd := exec.CommandContext(ctx, python, "-m", "phone", "stream",
		"--serial", serial, "--url", wsURL,
		"--width", strconv.Itoa(int(width)), "--height", strconv.Itoa(int(height)))
	if c.Options.PythonPath != "" {
		cmd.Env = append(os.Environ(), "PYTHONPATH="+c.Options.PythonPath)
	}
	_ = cmd.Run()
}

func (c *Client) simulateStream(ctx context.Context, wsURL string) {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	var writeMu sync.Mutex
	taps := 0
	send := func() {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteMessage(websocket.TextMessage, svgFrame(taps))
	}
	send()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var body struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(message, &body) == nil && body.Type == "tap" {
				taps++
				send()
			}
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			send()
		}
	}
}

func svgFrame(taps int) []byte {
	color := "#1f6b4a"
	if taps%2 == 1 {
		color = "#d6ff4a"
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="360" height="640"><rect width="360" height="640" fill="%s"/><text x="24" y="80" font-size="28" fill="#102117">Android</text><text x="24" y="130" font-size="22" fill="#102117">taps %d</text></svg>`, color, taps)
	payload, _ := json.Marshal(map[string]any{
		"type": "frame",
		"mime": "image/svg+xml",
		"data": base64.StdEncoding.EncodeToString([]byte(svg)),
	})
	return payload
}
