package mihomo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"hop/internal/model"
)

type configsResponse struct {
	Mode string `json:"mode"`
}

type modeRequest struct {
	Mode string `json:"mode"`
}

type connectionsMessage struct {
	DownloadTotal int64               `json:"downloadTotal"`
	UploadTotal   int64               `json:"uploadTotal"`
	Connections   []connectionMessage `json:"connections"`
}

type connectionMessage struct {
	ID          string             `json:"id"`
	Metadata    connectionMetadata `json:"metadata"`
	Upload      int64              `json:"upload"`
	Download    int64              `json:"download"`
	Start       time.Time          `json:"start"`
	Chains      []string           `json:"chains"`
	Rule        string             `json:"rule"`
	RulePayload string             `json:"rulePayload"`
}

type connectionMetadata struct {
	Network         string `json:"network"`
	Host            string `json:"host"`
	SniffHost       string `json:"sniffHost"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort string `json:"destinationPort"`
	SourcePort      string `json:"sourcePort"`
	Process         string `json:"process"`
}

type logMessage struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// Mode returns the core's outbound mode.
func (c *Client) Mode(ctx context.Context) (string, error) {
	raw, err := c.do(ctx, c.timeout, http.MethodGet, "/configs", nil, http.StatusOK)
	if err != nil {
		return "", err
	}
	var out configsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode mihomo configs response (body=%q): %w", raw, err)
	}
	return out.Mode, nil
}

// SetMode changes the core's outbound mode while it runs.
func (c *Client) SetMode(ctx context.Context, mode string) error {
	body, err := json.Marshal(modeRequest{Mode: mode})
	if err != nil {
		return fmt.Errorf("encode mode request (mode=%q): %w", mode, err)
	}
	_, err = c.do(ctx, c.timeout, http.MethodPatch, "/configs", body, http.StatusNoContent)
	return err
}

// StreamConnections calls handle with the connection table about once a
// second until ctx is done or the stream fails. It always returns a non-nil
// error.
func (c *Client) StreamConnections(ctx context.Context, handle func(model.Connections)) error {
	return c.stream(ctx, "/connections", func(raw []byte) error {
		var msg connectionsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return fmt.Errorf("decode mihomo connections message (size=%d): %w", len(raw), err)
		}
		handle(toConnections(msg))
		return nil
	})
}

// Connections returns the connection table as it is now.
func (c *Client) Connections(ctx context.Context) (model.Connections, error) {
	raw, err := c.do(ctx, c.timeout, http.MethodGet, "/connections", nil, http.StatusOK)
	if err != nil {
		return model.Connections{}, err
	}
	var msg connectionsMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return model.Connections{}, fmt.Errorf("decode mihomo connections response (size=%d): %w", len(raw), err)
	}
	return toConnections(msg), nil
}

func toConnections(msg connectionsMessage) model.Connections {
	active := make([]model.Connection, len(msg.Connections))
	for i, m := range msg.Connections {
		active[i] = model.Connection{
			ID:         m.ID,
			Host:       connectionHost(m.Metadata),
			Port:       m.Metadata.DestinationPort,
			Network:    m.Metadata.Network,
			Process:    m.Metadata.Process,
			SourcePort: m.Metadata.SourcePort,
			Rule:       ruleText(m.Rule, m.RulePayload),
			Chain:      reversed(m.Chains),
			Upload:     m.Upload,
			Download:   m.Download,
			Start:      m.Start,
		}
	}
	return model.Connections{Active: active, UploadTotal: msg.UploadTotal, DownloadTotal: msg.DownloadTotal}
}

func connectionHost(m connectionMetadata) string {
	switch {
	case m.Host != "":
		return m.Host
	case m.SniffHost != "":
		return m.SniffHost
	default:
		return m.DestinationIP
	}
}

func ruleText(rule string, payload string) string {
	if payload == "" {
		return rule
	}
	return rule + "(" + payload + ")"
}

// reversed turns the core's chain, which lists the proxy first, into the
// order traffic takes: from the outermost group to the proxy.
func reversed(chain []string) []string {
	out := make([]string, len(chain))
	for i, name := range chain {
		out[len(chain)-1-i] = name
	}
	return out
}

// CloseConnection closes one connection.
func (c *Client) CloseConnection(ctx context.Context, id string) error {
	_, err := c.do(ctx, c.timeout, http.MethodDelete, "/connections/"+url.PathEscape(id), nil, http.StatusNoContent)
	return err
}

// CloseAllConnections closes every connection.
func (c *Client) CloseAllConnections(ctx context.Context) error {
	_, err := c.do(ctx, c.timeout, http.MethodDelete, "/connections", nil, http.StatusNoContent)
	return err
}

// StreamLogs calls handle for every log line at level or above until ctx is
// done or the stream fails. It always returns a non-nil error.
func (c *Client) StreamLogs(ctx context.Context, level string, handle func(model.LogEntry)) error {
	return c.stream(ctx, "/logs?level="+url.QueryEscape(level), func(raw []byte) error {
		var msg logMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return fmt.Errorf("decode mihomo log message (body=%q): %w", raw, err)
		}
		handle(model.LogEntry{Level: msg.Type, Message: msg.Payload, Time: time.Now()})
		return nil
	})
}
