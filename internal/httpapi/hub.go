package httpapi

import (
	"sync"
	"time"

	"fmt"
	"github.com/gorilla/websocket"
	"github.com/xingshanghe/x-phone/internal/control"
	"github.com/xingshanghe/x-phone/internal/service"
)

const (
	controlQueue   = 32
	controlLockTTL = 15 * time.Second
)

type lockedConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *lockedConn) write(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(messageType, data)
}

type agentLink struct {
	conn     *lockedConn
	control  chan []byte
	done     chan struct{}
	stopOnce sync.Once
}

func newAgentLink(conn *lockedConn) *agentLink {
	link := &agentLink{conn: conn, control: make(chan []byte, controlQueue), done: make(chan struct{})}
	go link.run()
	return link
}

func (link *agentLink) run() {
	for {
		select {
		case <-link.done:
			return
		case payload := <-link.control:
			if err := link.conn.write(websocket.TextMessage, payload); err != nil {
				return
			}
		}
	}
}

func (link *agentLink) stop() {
	link.stopOnce.Do(func() { close(link.done) })
}

type browserLink struct {
	conn     *lockedConn
	frames   chan []byte
	done     chan struct{}
	stopOnce sync.Once
}

func newBrowserLink(conn *lockedConn) *browserLink {
	link := &browserLink{conn: conn, frames: make(chan []byte, 1), done: make(chan struct{})}
	go link.run()
	return link
}

func (link *browserLink) run() {
	for {
		select {
		case <-link.done:
			return
		case payload := <-link.frames:
			if err := link.conn.write(websocket.TextMessage, payload); err != nil {
				return
			}
		}
	}
}

func (link *browserLink) stop() {
	link.stopOnce.Do(func() { close(link.done) })
}

type lockHolder struct {
	accountID string
	until     time.Time
}

type videoSize struct {
	width  int
	height int
}

// FanoutResult counts devices that accepted the gesture.
type FanoutResult struct {
	Delivered int
	Offline   int
}

// Hub relays one video stream per instance and a serial control queue per device.
type Hub struct {
	mu       sync.Mutex
	agents   map[string]*agentLink
	browsers map[string]map[*browserLink]struct{}
	video    map[string]videoSize
	locks    map[string]lockHolder
}

func NewHub() *Hub {
	return &Hub{
		agents:   map[string]*agentLink{},
		browsers: map[string]map[*browserLink]struct{}{},
		video:    map[string]videoSize{},
		locks:    map[string]lockHolder{},
	}
}

func (h *Hub) setAgent(instanceID string, link *agentLink) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if current := h.agents[instanceID]; current != nil && current != link {
		current.stop()
	}
	h.agents[instanceID] = link
}

func (h *Hub) clearAgent(instanceID string, link *agentLink) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if current := h.agents[instanceID]; current == link {
		delete(h.agents, instanceID)
	}
}

func (h *Hub) addBrowser(instanceID string, link *browserLink) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.browsers[instanceID]
	if set == nil {
		set = map[*browserLink]struct{}{}
		h.browsers[instanceID] = set
	}
	set[link] = struct{}{}
}

func (h *Hub) removeBrowser(instanceID string, link *browserLink) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.browsers[instanceID], link)
	if len(h.browsers[instanceID]) == 0 {
		delete(h.browsers, instanceID)
	}
}

func (h *Hub) offerFrame(instanceID string, payload []byte) {
	h.mu.Lock()
	links := make([]*browserLink, 0, len(h.browsers[instanceID]))
	for link := range h.browsers[instanceID] {
		links = append(links, link)
	}
	h.mu.Unlock()
	for _, link := range links {
		offer(link.frames, payload)
	}
}

func (h *Hub) SetVideo(instanceID string, width, height int) {
	h.mu.Lock()
	h.video[instanceID] = videoSize{width: width, height: height}
	h.mu.Unlock()
}

func (h *Hub) Video(instanceID string) (int, int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	size, ok := h.video[instanceID]
	return size.width, size.height, ok && size.width > 0 && size.height > 0
}

// ApplyVideo replaces profile dimensions with the latest frame size from the device.
func (h *Hub) ApplyVideo(targets []control.Target) []control.Target {
	out := make([]control.Target, len(targets))
	copy(out, targets)
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, target := range out {
		size, ok := h.video[target.InstanceID]
		if ok && size.width > 0 && size.height > 0 {
			out[i].Width = size.width
			out[i].Height = size.height
		}
	}
	return out
}

// Enqueue sends one already-encoded control message to a single device.
func (h *Hub) Enqueue(instanceID string, payload []byte) error {
	cloned := append([]byte(nil), payload...)
	h.mu.Lock()
	defer h.mu.Unlock()
	link := h.agents[instanceID]
	if link == nil {
		return fmt.Errorf("%w: agent is offline", service.ErrConflict)
	}
	return enqueue(link, cloned)
}

// Fanout encodes one gesture for each target and queues it without dropping earlier events.
func (h *Hub) Fanout(accountID string, targets []control.Target, gesture control.Gesture, now time.Time) (FanoutResult, error) {
	encoded := make([][]byte, len(targets))
	for i, target := range targets {
		payload, err := gesture.Encode(target.Width, target.Height)
		if err != nil {
			return FanoutResult{}, fmt.Errorf("%w: %s", service.ErrValidation, err.Error())
		}
		encoded[i] = payload
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.acquire(accountID, targets, now); err != nil {
		return FanoutResult{}, err
	}
	type job struct {
		link    *agentLink
		payload []byte
	}
	jobs := make([]job, 0, len(targets))
	offline := 0
	for i, target := range targets {
		link := h.agents[target.InstanceID]
		if link == nil {
			offline++
			continue
		}
		if len(link.control) == cap(link.control) {
			return FanoutResult{}, fmt.Errorf("%w: control queue full", service.ErrConflict)
		}
		jobs = append(jobs, job{link: link, payload: encoded[i]})
	}
	if len(jobs) == 0 {
		return FanoutResult{Offline: offline}, fmt.Errorf("%w: no connected agents", service.ErrConflict)
	}
	for _, job := range jobs {
		job.link.control <- job.payload
	}
	return FanoutResult{Delivered: len(jobs), Offline: offline}, nil
}

func (h *Hub) acquire(accountID string, targets []control.Target, now time.Time) error {
	for _, target := range targets {
		current, ok := h.locks[target.InstanceID]
		if ok && now.Before(current.until) && current.accountID != accountID {
			return service.ErrLocked
		}
	}
	until := now.Add(controlLockTTL)
	for _, target := range targets {
		h.locks[target.InstanceID] = lockHolder{accountID: accountID, until: until}
	}
	return nil
}

func enqueue(link *agentLink, payload []byte) error {
	if len(link.control) == cap(link.control) {
		return fmt.Errorf("%w: control queue full", service.ErrConflict)
	}
	link.control <- payload
	return nil
}

func offer(ch chan []byte, payload []byte) {
	select {
	case ch <- payload:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- payload:
		default:
		}
	}
}
