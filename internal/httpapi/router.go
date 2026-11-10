package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/xingshanghe/x-phone/internal/auth"
	"github.com/xingshanghe/x-phone/internal/httpx"
	"github.com/xingshanghe/x-phone/internal/model"
	"github.com/xingshanghe/x-phone/internal/service"
)

type Engine struct {
	Router *gin.Engine
	Hub    *Hub
}

func NewEngine(svc *service.Services, signer *auth.Signer) *Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery(), httpx.CORS(), httpx.RequestID())
	hub := NewHub()
	api := &api{svc: svc, signer: signer, hub: hub}
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	v1 := router.Group("/api/v1")
	v1.POST("/auth/login", api.login)

	user := v1.Group("")
	user.Use(httpx.UserAuth(signer))
	user.GET("/hosts", api.listHosts)
	user.POST("/hosts", api.createHost)
	user.GET("/profiles", api.listProfiles)
	user.GET("/instances", api.listInstances)
	user.POST("/instances", api.createInstance)
	user.GET("/instances/:id", api.getInstance)
	user.POST("/instances/:id/start", api.startInstance)
	user.POST("/instances/:id/stop", api.stopInstance)
	user.DELETE("/instances/:id", api.deleteInstance)
	user.GET("/instances/:id/tasks", api.listTasks)
	user.POST("/instances/:id/tasks", api.createTask)
	user.POST("/instances/:id/apk", api.uploadAPK)
	user.GET("/instances/:id/stream", api.browserStream)
	user.GET("/groups", api.listGroups)
	user.POST("/groups", api.createGroup)
	user.GET("/groups/:id", api.getGroup)
	user.DELETE("/groups/:id", api.deleteGroup)
	user.POST("/groups/:id/batch", api.expandBatch)
	user.GET("/groups/:id/control", api.groupControl)

	agent := v1.Group("/agent")
	agent.Use(api.agentAuth)
	agent.POST("/heartbeat", api.heartbeat)
	agent.GET("/tasks", api.pendingTasks)
	agent.POST("/tasks/:id/claim", api.claimTask)
	agent.POST("/tasks/:id/complete", api.completeTask)
	agent.GET("/instances", api.runningInstances)
	agent.GET("/tasks/:id/apk", api.downloadAPK)
	agent.GET("/stream", api.agentStream)
	return &Engine{Router: router, Hub: hub}
}

type api struct {
	svc    *service.Services
	signer *auth.Signer
	hub    *Hub
}

func (a *api) login(c *gin.Context) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&body) != nil || body.Username == "" || body.Password == "" {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	session, err := a.svc.Auth.Login(c.Request.Context(), body.Username, body.Password, time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, session, nil)
}

func (a *api) createHost(c *gin.Context) {
	var body struct {
		Name     string `json:"name"`
		Addr     string `json:"addr"`
		Capacity int    `json:"capacity"`
	}
	if c.ShouldBindJSON(&body) != nil {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	created, err := a.svc.Hosts.Register(c.Request.Context(), httpx.AccountID(c), body.Name, body.Addr, body.Capacity)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, created, nil)
}

func (a *api) listHosts(c *gin.Context) {
	page, size := httpx.Page(c)
	items, total, err := a.svc.Hosts.List(c.Request.Context(), page, size)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"items": items, "total": total}, &httpx.Meta{Page: page, PageSize: size, Total: total})
}

func (a *api) listProfiles(c *gin.Context) {
	items, err := a.svc.Profiles.List(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	views := make([]profileView, 0, len(items))
	for _, item := range items {
		views = append(views, newProfileView(item))
	}
	httpx.OK(c, gin.H{"items": views, "total": len(views)}, nil)
}

func (a *api) createInstance(c *gin.Context) {
	var body struct {
		Name      string `json:"name"`
		HostID    string `json:"hostId"`
		ProfileID string `json:"profileId"`
		HTTPProxy string `json:"httpProxy"`
		ADBSerial string `json:"adbSerial"`
	}
	if c.ShouldBindJSON(&body) != nil {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	created, err := a.svc.Instances.Create(c.Request.Context(), httpx.AccountID(c), service.CreateInstanceInput{
		Name: body.Name, HostID: body.HostID, ProfileID: body.ProfileID, HTTPProxy: body.HTTPProxy, ADBSerial: body.ADBSerial,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, created, nil)
}

func (a *api) listInstances(c *gin.Context) {
	page, size := httpx.Page(c)
	items, total, err := a.svc.Instances.List(c.Request.Context(), page, size)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"items": items, "total": total}, &httpx.Meta{Page: page, PageSize: size, Total: total})
}

func (a *api) getInstance(c *gin.Context) {
	item, err := a.svc.Instances.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item, nil)
}

func (a *api) startInstance(c *gin.Context) { a.transition(c, "start") }
func (a *api) stopInstance(c *gin.Context)  { a.transition(c, "stop") }

func (a *api) transition(c *gin.Context, action string) {
	item, err := a.svc.Instances.Transition(c.Request.Context(), httpx.AccountID(c), c.Param("id"), action)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item, nil)
}

func (a *api) deleteInstance(c *gin.Context) {
	item, err := a.svc.Instances.Delete(c.Request.Context(), httpx.AccountID(c), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item, nil)
}

func (a *api) listTasks(c *gin.Context) {
	items, err := a.svc.Instances.Tasks(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"items": items, "total": len(items)}, nil)
}

func (a *api) createTask(c *gin.Context) {
	var body struct {
		Type string `json:"type"`
	}
	if c.ShouldBindJSON(&body) != nil {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	if body.Type != "script" {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	httpx.Fail(c, a.svc.Instances.RejectScript(c.Request.Context(), c.Param("id")))
}

func (a *api) uploadAPK(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	opened, err := file.Open()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer opened.Close()
	task, err := a.svc.Instances.SaveAPK(c.Request.Context(), httpx.AccountID(c), c.Param("id"), c.PostForm("packageName"), file.Filename, opened, file.Size)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, task, nil)
}

func (a *api) agentAuth(c *gin.Context) {
	token := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = strings.TrimSpace(token[7:])
	} else {
		token = strings.TrimSpace(c.Query("token"))
	}
	host, err := a.svc.Hosts.ByToken(c.Request.Context(), token)
	if err != nil {
		httpx.Fail(c, service.ErrUnauthorized)
		c.Abort()
		return
	}
	httpx.SetHostID(c, host.ID)
	c.Next()
}

func (a *api) heartbeat(c *gin.Context) {
	var body struct {
		Binderfs     bool `json:"binderfs"`
		Capacity     int  `json:"capacity"`
		RunningCount int  `json:"runningCount"`
	}
	if c.ShouldBindJSON(&body) != nil {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	host, err := a.svc.Hosts.Heartbeat(c.Request.Context(), httpx.HostID(c), service.HeartbeatInput{
		Binderfs: body.Binderfs, Capacity: body.Capacity, RunningCount: body.RunningCount,
	}, time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, host, nil)
}

func (a *api) pendingTasks(c *gin.Context) {
	items, err := a.svc.Agent.Pending(c.Request.Context(), httpx.HostID(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"items": items, "total": len(items)}, nil)
}

func (a *api) claimTask(c *gin.Context) {
	task, err := a.svc.Agent.Claim(c.Request.Context(), httpx.HostID(c), c.Param("id"), time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, task, nil)
}

func (a *api) completeTask(c *gin.Context) {
	var body struct {
		OK          bool            `json:"ok"`
		Error       string          `json:"error"`
		Log         string          `json:"log"`
		ADBPort     int             `json:"adbPort"`
		ContainerID string          `json:"containerId"`
		Serial      string          `json:"serial"`
		Width       int             `json:"width"`
		Height      int             `json:"height"`
		Report      json.RawMessage `json:"report"`
	}
	if c.ShouldBindJSON(&body) != nil {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	task, err := a.svc.Agent.Complete(c.Request.Context(), httpx.HostID(c), c.Param("id"), service.CompleteInput{
		OK: body.OK, Error: body.Error, Log: body.Log, ADBPort: body.ADBPort, ContainerID: body.ContainerID,
		Serial: body.Serial, Width: body.Width, Height: body.Height, Report: body.Report,
	}, time.Now())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, task, nil)
}

func (a *api) runningInstances(c *gin.Context) {
	items, err := a.svc.Agent.Running(c.Request.Context(), httpx.HostID(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"items": items, "total": len(items)}, nil)
}

func (a *api) downloadAPK(c *gin.Context) {
	reader, err := a.svc.Agent.OpenAPK(c.Request.Context(), httpx.HostID(c), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer reader.Close()
	c.Header("Content-Type", "application/vnd.android.package-archive")
	_, _ = io.Copy(c.Writer, reader)
}

type profileView struct {
	ID           string `json:"id"`
	Brand        string `json:"brand"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Device       string `json:"device"`
	Product      string `json:"product"`
	Fingerprint  string `json:"fingerprint"`
	Release      string `json:"release"`
	SDK          string `json:"sdk"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	DPI          int    `json:"dpi"`
	Locale       string `json:"locale"`
	Timezone     string `json:"timezone"`
	GPU          string `json:"gpu"`
	CPUCores     int    `json:"cpuCores"`
	MemoryMB     int    `json:"memoryMb"`
	TouchPoints  int    `json:"touchPoints"`
}

func newProfileView(item model.DeviceProfile) profileView {
	return profileView{
		ID: item.Slug, Brand: item.Brand, Manufacturer: item.Manufacturer, Model: item.Model,
		Device: item.Device, Product: item.Product, Fingerprint: item.Fingerprint, Release: item.Release,
		SDK: item.SDK, Width: item.Width, Height: item.Height, DPI: item.DPI, Locale: item.Locale,
		Timezone: item.Timezone, GPU: item.GPU, CPUCores: item.CPUCores, MemoryMB: item.MemoryMB,
		TouchPoints: item.TouchPoints,
	}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
