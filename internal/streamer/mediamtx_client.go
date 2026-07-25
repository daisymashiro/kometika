package streamer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// MediaMTXClient adalah client untuk berinteraksi dengan MediaMTX HTTP API
type MediaMTXClient struct {
	baseURL string
	client  *http.Client
	logger  *zap.Logger
}

// StreamInfo berisi informasi tentang stream yang sedang berjalan
type StreamInfo struct {
	Name         string    `json:"name"`
	Ready        bool      `json:"ready"`
	ReadyTime    time.Time `json:"readyTime"`
	Tracks       int       `json:"tracks"`
	BytesRead    int64     `json:"bytesRead"`
	BytesWritten int64     `json:"bytesWritten"`
	Readers      int       `json:"readers"`
}

// PathsResponse adalah response dari /v3/paths/list
type PathsResponse struct {
	PageCount int                     `json:"pageCount"`
	ItemCount int                     `json:"itemCount"`
	Items     []map[string]StreamInfo `json:"items"`
}

// NewMediaMTXClient membuat instance baru MediaMTX client
func NewMediaMTXClient(baseURL string, logger *zap.Logger) *MediaMTXClient {
	return &MediaMTXClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// GetStreamInfo mendapatkan informasi stream berdasarkan nama path
func (c *MediaMTXClient) GetStreamInfo(ctx context.Context, streamName string) (*StreamInfo, error) {
	url := fmt.Sprintf("%s/v3/paths/get/%s", c.baseURL, streamName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get stream info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("stream not found: %s", streamName)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var info StreamInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &info, nil
}

// ListStreams mendapatkan daftar semua stream yang aktif
func (c *MediaMTXClient) ListStreams(ctx context.Context) ([]StreamInfo, error) {
	url := fmt.Sprintf("%s/v3/paths/list", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list streams: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var pathsResp PathsResponse
	if err := json.NewDecoder(resp.Body).Decode(&pathsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var streams []StreamInfo
	for _, item := range pathsResp.Items {
		for _, info := range item {
			streams = append(streams, info)
		}
	}

	return streams, nil
}

// IsStreamActive mengecek apakah stream dengan nama tertentu sedang aktif
func (c *MediaMTXClient) IsStreamActive(ctx context.Context, streamName string) bool {
	info, err := c.GetStreamInfo(ctx, streamName)
	if err != nil {
		c.logger.Debug("Stream not active", zap.String("stream", streamName), zap.Error(err))
		return false
	}
	return info.Ready
}

// WaitForStreamReady menunggu hingga stream ready atau timeout
func (c *MediaMTXClient) WaitForStreamReady(ctx context.Context, streamName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(deadline)):
			return fmt.Errorf("timeout waiting for stream to be ready")
		case <-ticker.C:
			if c.IsStreamActive(ctx, streamName) {
				c.logger.Info("Stream is ready", zap.String("stream", streamName))
				return nil
			}
		}
	}
}

// GetStreamStats mendapatkan statistik stream untuk monitoring
func (c *MediaMTXClient) GetStreamStats(ctx context.Context, streamName string) (map[string]interface{}, error) {
	info, err := c.GetStreamInfo(ctx, streamName)
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"name":          info.Name,
		"ready":         info.Ready,
		"ready_time":    info.ReadyTime,
		"tracks":        info.Tracks,
		"bytes_read":    info.BytesRead,
		"bytes_written": info.BytesWritten,
		"readers":       info.Readers,
		"duration":      time.Since(info.ReadyTime).String(),
	}

	return stats, nil
}
