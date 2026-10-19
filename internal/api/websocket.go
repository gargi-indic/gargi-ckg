package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/metrics"
	gargiv1 "github.com/gargi-indic/gargi-ckg/pkg/api/v1"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc/metadata"
)

// WebSocketOptions configures the behavior of the WebSocket streaming handler.
type WebSocketOptions struct {
	WriteWait      time.Duration
	PongWait       time.Duration
	PingPeriod     time.Duration
	SendBufferSize int
	CheckOrigin    func(r *http.Request) bool
}

// DefaultWebSocketOptions returns standard defaults for WebSocket streaming.
func DefaultWebSocketOptions() WebSocketOptions {
	pongWait := 60 * time.Second
	return WebSocketOptions{
		WriteWait:      5 * time.Second,
		PongWait:       pongWait,
		PingPeriod:     (pongWait * 9) / 10,
		SendBufferSize: 256,
		CheckOrigin: func(_ *http.Request) bool {
			return true
		},
	}
}

// WSClientMessage represents an incoming message from a WebSocket client.
type WSClientMessage struct {
	Action       string `json:"action"`                  // "subscribe" or "unsubscribe"
	ExecutionID  string `json:"execution_id"`            // Target execution ID
	FromSequence uint64 `json:"from_sequence,omitempty"` // Sequence number to resume from
}

// WSServerMessage represents an outgoing message to a WebSocket client.
type WSServerMessage struct {
	Event        string                                  `json:"event"`                   // "update", "subscribed", "unsubscribed", "error"
	ExecutionID  string                                  `json:"execution_id"`            // Target execution ID
	Sequence     uint64                                  `json:"sequence,omitempty"`      // Sequence number of the update
	Timestamp    string                                  `json:"timestamp,omitempty"`     // RFC3339 event timestamp
	EventType    string                                  `json:"event_type,omitempty"`    // Type of execution event
	NodeID       string                                  `json:"node_id,omitempty"`       // Node ID if node-level update
	FromStatus   string                                  `json:"from_status,omitempty"`   // Previous status
	ToStatus     string                                  `json:"to_status,omitempty"`     // New status
	ErrorMessage string                                  `json:"error_message,omitempty"` // Error message if failed
	Error        string                                  `json:"error,omitempty"`         // Operational error message
	Payload      *gargiv1.StreamExecutionUpdatesResponse `json:"payload,omitempty"`       // Full protobuf payload
}

// WebSocketHandler manages WebSocket connections for streaming execution updates.
type WebSocketHandler struct {
	service  gargiv1.GargiServiceServer
	logger   *slog.Logger
	opts     WebSocketOptions
	upgrader websocket.Upgrader
}

// NewWebSocketHandler creates a new WebSocketHandler.
func NewWebSocketHandler(service gargiv1.GargiServiceServer, logger *slog.Logger, opts ...WebSocketOptions) *WebSocketHandler {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := DefaultWebSocketOptions()
	if len(opts) > 0 {
		userOpts := opts[0]
		if userOpts.WriteWait > 0 {
			cfg.WriteWait = userOpts.WriteWait
		}
		if userOpts.PongWait > 0 {
			cfg.PongWait = userOpts.PongWait
		}
		if userOpts.PingPeriod > 0 {
			cfg.PingPeriod = userOpts.PingPeriod
		}
		if userOpts.SendBufferSize > 0 {
			cfg.SendBufferSize = userOpts.SendBufferSize
		}
		if userOpts.CheckOrigin != nil {
			cfg.CheckOrigin = userOpts.CheckOrigin
		}
	}

	return &WebSocketHandler{
		service: service,
		logger:  logger,
		opts:    cfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     cfg.CheckOrigin,
		},
	}
}

type wsConnection struct {
	handler *WebSocketHandler
	conn    *websocket.Conn
	logger  *slog.Logger
	opts    WebSocketOptions

	send chan []byte
	done chan struct{}

	mu            sync.Mutex
	subscriptions map[string]context.CancelFunc
	closed        bool
}

type wsStreamAdapter struct {
	ctx    context.Context
	client *wsConnection
}

func (a *wsStreamAdapter) Send(resp *gargiv1.StreamExecutionUpdatesResponse) error {
	return a.client.queueUpdate(resp)
}

func (a *wsStreamAdapter) SetHeader(metadata.MD) error  { return nil }
func (a *wsStreamAdapter) SendHeader(metadata.MD) error { return nil }
func (a *wsStreamAdapter) SetTrailer(metadata.MD)       {}
func (a *wsStreamAdapter) Context() context.Context     { return a.ctx }
func (a *wsStreamAdapter) SendMsg(_ any) error          { return nil }
func (a *wsStreamAdapter) RecvMsg(_ any) error          { return nil }

func (c *wsConnection) subscribe(execID string, fromSeq uint64) {
	if execID == "" {
		c.sendError(execID, "execution_id cannot be empty")
		return
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	if cancel, exists := c.subscriptions[execID]; exists {
		cancel()
		delete(c.subscriptions, execID)
	}

	subCtx, cancel := context.WithCancel(context.Background())
	c.subscriptions[execID] = cancel
	c.mu.Unlock()

	c.sendAck("subscribed", execID)

	go func() {
		adapter := &wsStreamAdapter{
			ctx:    subCtx,
			client: c,
		}

		req := &gargiv1.StreamExecutionUpdatesRequest{
			ExecutionId:  execID,
			FromSequence: fromSeq,
		}

		if err := c.handler.service.StreamExecutionUpdates(req, adapter); err != nil {
			if !errors.Is(subCtx.Err(), context.Canceled) {
				c.logger.Debug("WS stream finished with error", "execution_id", execID, "error", err)
			}
		}
	}()
}

func (c *wsConnection) unsubscribe(execID string) {
	c.mu.Lock()
	if cancel, ok := c.subscriptions[execID]; ok {
		cancel()
		delete(c.subscriptions, execID)
	}
	c.mu.Unlock()

	c.sendAck("unsubscribed", execID)
}

func (c *wsConnection) sendAck(event, execID string) {
	msg := WSServerMessage{
		Event:       event,
		ExecutionID: execID,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.enqueue(data)
}

func (c *wsConnection) sendError(execID, errMsg string) {
	msg := WSServerMessage{
		Event:       "error",
		ExecutionID: execID,
		Error:       errMsg,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.enqueue(data)
}

func (c *wsConnection) enqueue(data []byte) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	select {
	case <-c.done:
	case c.send <- data:
	default:
		c.closeSlowConsumer()
	}
}

func (c *wsConnection) queueUpdate(resp *gargiv1.StreamExecutionUpdatesResponse) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("connection closed")
	}
	c.mu.Unlock()

	msg := WSServerMessage{
		Event:        "update",
		ExecutionID:  resp.GetExecutionId(),
		Sequence:     resp.GetSequence(),
		EventType:    resp.GetEventType(),
		NodeID:       resp.GetNodeId(),
		FromStatus:   resp.GetFromStatus().String(),
		ToStatus:     resp.GetToStatus().String(),
		ErrorMessage: resp.GetErrorMessage(),
		Payload:      resp,
	}
	if resp.GetTimestamp() != nil {
		msg.Timestamp = resp.GetTimestamp().AsTime().Format(time.RFC3339Nano)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case <-c.done:
		return errors.New("connection closed")
	case c.send <- data:
		return nil
	default:
		c.closeSlowConsumer()
		return errors.New("slow consumer buffer overflow")
	}
}

func (c *wsConnection) closeSlowConsumer() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.done)
	for _, cancel := range c.subscriptions {
		cancel()
	}
	c.subscriptions = make(map[string]context.CancelFunc)
	c.mu.Unlock()

	c.logger.Warn("slow consumer detected, closing websocket connection", "remote", c.conn.RemoteAddr().String())
	_ = c.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "slow consumer"),
		time.Now().Add(c.opts.WriteWait),
	)
	_ = c.conn.Close()
}

func (c *wsConnection) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.done)
	for _, cancel := range c.subscriptions {
		cancel()
	}
	c.subscriptions = make(map[string]context.CancelFunc)
	c.mu.Unlock()
}

func (c *wsConnection) writeLoop() {
	ticker := time.NewTicker(c.opts.PingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			_ = c.conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(c.opts.WriteWait),
			)
			return
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.opts.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.opts.WriteWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}

func (c *wsConnection) readLoop() {
	defer func() {
		c.close()
	}()

	c.conn.SetReadLimit(65536)
	_ = c.conn.SetReadDeadline(time.Now().Add(c.opts.PongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.opts.PongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.ClosePolicyViolation) {
				c.logger.Debug("websocket read error", "error", err)
			}
			break
		}

		var clientMsg WSClientMessage
		if err := json.Unmarshal(message, &clientMsg); err != nil {
			c.sendError("", "invalid JSON message format")
			continue
		}

		switch strings.ToLower(clientMsg.Action) {
		case "subscribe":
			c.subscribe(clientMsg.ExecutionID, clientMsg.FromSequence)
		case "unsubscribe":
			c.unsubscribe(clientMsg.ExecutionID)
		default:
			c.sendError(clientMsg.ExecutionID, fmt.Sprintf("unknown action: %q", clientMsg.Action))
		}
	}
}

// ServeHTTP handles incoming HTTP requests and upgrades them to WebSocket connections.
func (h *WebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("websocket upgrade failed", "error", err)
		return
	}

	metrics.RecordStreamConnected("websocket")
	defer metrics.RecordStreamDisconnected("websocket")

	client := &wsConnection{
		handler:       h,
		conn:          conn,
		logger:        h.logger,
		opts:          h.opts,
		send:          make(chan []byte, h.opts.SendBufferSize),
		done:          make(chan struct{}),
		subscriptions: make(map[string]context.CancelFunc),
	}

	execID := r.PathValue("id")
	if execID == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 3 && parts[0] == "v1" && parts[1] == "executions" && parts[len(parts)-1] == "ws" {
			execID = parts[2]
		}
	}
	if execID == "" {
		execID = r.URL.Query().Get("execution_id")
	}

	var fromSeq uint64
	if seqStr := r.URL.Query().Get("from_sequence"); seqStr != "" {
		if parsed, err := strconv.ParseUint(seqStr, 10, 64); err == nil {
			fromSeq = parsed
		}
	}

	go client.writeLoop()

	if execID != "" {
		client.subscribe(execID, fromSeq)
	}

	client.readLoop()
}
