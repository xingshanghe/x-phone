package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/xingshanghe/x-phone/internal/control"
	"github.com/xingshanghe/x-phone/internal/httpx"
	"github.com/xingshanghe/x-phone/internal/service"
)

func (a *api) browserStream(c *gin.Context) {
	instance, err := a.svc.Instances.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	link := newBrowserLink(&lockedConn{conn: conn})
	a.hub.addBrowser(instance.ID, link)
	defer func() {
		link.stop()
		a.hub.removeBrowser(instance.ID, link)
		_ = conn.Close()
	}()
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		a.dispatchControl(c, httpx.AccountID(c), instance.ID, message, link.conn)
	}
}

func (a *api) agentStream(c *gin.Context) {
	instanceID := c.Query("instanceId")
	instance, err := a.svc.Instances.Get(c.Request.Context(), instanceID)
	if err != nil || instance.HostID != httpx.HostID(c) {
		httpx.Fail(c, service.ErrNotFound)
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	link := newAgentLink(&lockedConn{conn: conn})
	a.hub.setAgent(instance.ID, link)
	defer func() {
		link.stop()
		a.hub.clearAgent(instance.ID, link)
		_ = conn.Close()
	}()
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		payload := append([]byte(nil), message...)
		if width, height, ok := control.VideoSize(payload); ok {
			a.hub.SetVideo(instance.ID, width, height)
			continue
		}
		a.hub.offerFrame(instance.ID, payload)
	}
}
