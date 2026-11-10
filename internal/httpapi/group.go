package httpapi

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/xingshanghe/x-phone/internal/control"
	"github.com/xingshanghe/x-phone/internal/httpx"
	"github.com/xingshanghe/x-phone/internal/service"
)

func (a *api) listGroups(c *gin.Context) {
	items, err := a.svc.Groups.List(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"items": items, "total": len(items)}, nil)
}

func (a *api) createGroup(c *gin.Context) {
	var body struct {
		Name        string   `json:"name"`
		InstanceIDs []string `json:"instanceIds"`
	}
	if c.ShouldBindJSON(&body) != nil {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	created, err := a.svc.Groups.Create(c.Request.Context(), httpx.AccountID(c), body.Name, body.InstanceIDs)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, created, nil)
}

func (a *api) getGroup(c *gin.Context) {
	item, err := a.svc.Groups.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, item, nil)
}

func (a *api) deleteGroup(c *gin.Context) {
	if err := a.svc.Groups.Delete(c.Request.Context(), httpx.AccountID(c), c.Param("id")); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"deleted": true}, nil)
}

func (a *api) expandBatch(c *gin.Context) {
	var body struct {
		Type string `json:"type"`
	}
	if c.ShouldBindJSON(&body) != nil || body.Type == "" {
		httpx.Fail(c, service.ErrValidation)
		return
	}
	result, err := a.svc.Groups.Expand(c.Request.Context(), httpx.AccountID(c), c.Param("id"), body.Type)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, result, nil)
}

func (a *api) groupControl(c *gin.Context) {
	group, err := a.svc.Groups.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	locked := &lockedConn{conn: conn}
	defer conn.Close()
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		a.dispatchGroup(c, httpx.AccountID(c), group.ID, message, locked)
	}
}

func (a *api) dispatchControl(c *gin.Context, accountID, instanceID string, message []byte, reply *lockedConn) {
	gesture, isControl, err := control.Parse(message)
	if !isControl {
		if err := a.hub.Enqueue(instanceID, message); err != nil {
			writeStatus(reply, err.Error())
		}
		return
	}
	if err != nil {
		writeStatus(reply, err.Error())
		return
	}
	target, err := a.svc.Groups.Target(c.Request.Context(), instanceID)
	if err != nil {
		writeStatus(reply, err.Error())
		return
	}
	result, err := a.hub.Fanout(accountID, a.hub.ApplyVideo([]control.Target{target}), gesture, time.Now())
	if err != nil {
		writeStatus(reply, err.Error())
		return
	}
	writeFanout(reply, result)
}

func (a *api) dispatchGroup(c *gin.Context, accountID, groupID string, message []byte, reply *lockedConn) {
	gesture, isControl, err := control.Parse(message)
	if !isControl {
		writeStatus(reply, "unsupported control message")
		return
	}
	if err != nil {
		writeStatus(reply, err.Error())
		return
	}
	targets, err := a.svc.Groups.Targets(c.Request.Context(), groupID)
	if err != nil {
		writeStatus(reply, err.Error())
		return
	}
	result, err := a.hub.Fanout(accountID, a.hub.ApplyVideo(targets), gesture, time.Now())
	if err != nil {
		writeStatus(reply, err.Error())
		return
	}
	writeFanout(reply, result)
}

func writeStatus(conn *lockedConn, text string) {
	payload, _ := json.Marshal(map[string]string{"type": "status", "text": text})
	_ = conn.write(websocket.TextMessage, payload)
}

func writeFanout(conn *lockedConn, result FanoutResult) {
	payload, _ := json.Marshal(map[string]any{"type": "fanout", "delivered": result.Delivered, "offline": result.Offline})
	_ = conn.write(websocket.TextMessage, payload)
}
