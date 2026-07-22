package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"mybot/internal/log"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sardanioss/httpcloak/client"
	"go.uber.org/zap"
)

const (
	defaultStreamTimeout = 10 * time.Minute // Ubah dari 0 menjadi 10 menit
	defaultHeadTimeout   = 1 * time.Minute
)

// contextBody memastikan cancel() terpanggil saat stream ditutup.
type contextBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *contextBody) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}

func validateURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("missing host in URL")
	}

	return parsed, nil
}

func parseContentLength(resp *client.Response) int64 {
	cl, err := strconv.ParseInt(resp.GetHeader("Content-Length"), 10, 64)
	if err != nil {
		return -1
	}
	return cl
}

func baseReferer(u *url.URL) string {
	return u.Scheme + "://" + u.Host + "/"
}

type VideoClient struct {
	httpClient    *client.Client
	logger        *zap.SugaredLogger
	streamTimeout time.Duration
}

func NewVideoClient(profile string, log *zap.Logger) *VideoClient {
	return NewVideoClientWithTimeout(profile, log, defaultStreamTimeout)
}

func NewVideoClientWithTimeout(profile string, log *zap.Logger, streamTimeout time.Duration) *VideoClient {
	if log == nil {
		log = zap.NewNop()
	}

	c := client.NewClient(profile,
		client.WithTimeout(streamTimeout), // Gunakan timeout yang wajar
		client.WithRetry(0),
	)

	return &VideoClient{
		httpClient:    c,
		logger:        log.Sugar(),
		streamTimeout: streamTimeout,
	}
}

func (vc *VideoClient) Close() {
	if vc.httpClient != nil {
		vc.httpClient.Close()
	}
}

func (vc *VideoClient) doRequest(
	ctx context.Context,
	method string,
	rawURL string,
	timeout time.Duration,
) (*client.Response, context.CancelFunc, error) {

	parsed, err := validateURL(rawURL)
	if err != nil {
		return nil, nil, err
	}

	reqCtx := ctx
	var cancel context.CancelFunc

	if timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		reqCtx, cancel = context.WithCancel(ctx)
	}

	resp, err := vc.httpClient.Do(reqCtx, &client.Request{
		Method:    method,
		URL:       rawURL,
		Referer:   baseReferer(parsed),
		FetchMode: client.FetchModeNoCors,
	})

	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("%s %q failed: %w", method, rawURL, err)
	}

	if !resp.IsSuccess() {
		cancel()
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("unexpected status %d for %q", resp.StatusCode, rawURL)
	}

	return resp, cancel, nil
}

func (vc *VideoClient) GetVideoStream(ctx context.Context, videoURL string) (io.ReadCloser, int64, error) {
	resp, cancel, err := vc.doRequest(ctx, http.MethodGet, videoURL, vc.streamTimeout)
	if err != nil {
		vc.logger.Errorw("video stream failed", "url", videoURL, "error", err)
		return nil, 0, err
	}

	cl := parseContentLength(resp)

	if cl == -1 {
		vc.logger.Infow("Content-Length not provided", "url", videoURL)
	} else {
		vc.logger.Debugw("Content-Length", "bytes", cl, "url", videoURL)
	}

	return &contextBody{
		ReadCloser: resp.Body,
		cancel:     cancel,
	}, cl, nil
}

func (vc *VideoClient) GetAudioStream(ctx context.Context, audioURL string) (io.ReadCloser, int64, error) {
	return vc.GetVideoStream(ctx, audioURL)
}

func (vc *VideoClient) GetThumbnail(ctx context.Context, imageURL string) ([]byte, error) {
	resp, cancel, err := vc.doRequest(ctx, http.MethodGet, imageURL, defaultHeadTimeout)
	if err != nil {
		vc.logger.Errorw("thumbnail fetch failed", "url", imageURL, "error", err)
		return nil, err
	}
	defer cancel()
	defer resp.Body.Close()

	data, err := resp.Bytes()
	if err != nil {
		return nil, err
	}

	vc.logger.Debugw("thumbnail fetched", "url", imageURL)
	return data, nil
}

func (vc *VideoClient) GetContentType(ctx context.Context, rawURL string) (string, error) {
	resp, cancel, err := vc.doRequest(ctx, http.MethodHead, rawURL, defaultHeadTimeout)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer resp.Body.Close()

	ctype := resp.GetHeader("Content-Type")
	if ctype == "" {
		return "application/octet-stream", nil
	}

	return strings.Split(ctype, ";")[0], nil
}

var (
	clientPool []*VideoClient
	poolOnce   sync.Once
)

var browserPresets = []string{
	"chrome-144-windows", "chrome-144-macos",
	"chrome-143-windows", "chrome-143-macos",
	"safari-18", "firefox-133",
	"chrome-144-linux", "chrome-143-linux",
	"ios-chrome-144", "ios-chrome-143",
	"ios-safari-18", "ios-safari-17",
	"android-chrome-144", "android-chrome-143",
	"chrome-144", "chrome-143", "chrome-141", "chrome-133",
}

func initClientPool() {

	clientPool = make([]*VideoClient, len(browserPresets))
	for i, preset := range browserPresets {
		clientPool[i] = NewVideoClient(preset, nil)
	}
}

func getRotatedClient() *VideoClient {
	poolOnce.Do(initClientPool)
	randomIndex := rand.Intn(len(clientPool))
	return clientPool[randomIndex]
}

func GetVideoStream(ctx context.Context, url string) (io.ReadCloser, int64, error) {
	return getRotatedClient().GetVideoStream(ctx, url)
}

func GetAudioStream(ctx context.Context, url string) (io.ReadCloser, int64, error) {
	return getRotatedClient().GetAudioStream(ctx, url)
}

func GetThumbnail(ctx context.Context, url string) ([]byte, error) {
	return getRotatedClient().GetThumbnail(ctx, url)
}

func GetContentType(ctx context.Context, url string) (string, error) {
	return getRotatedClient().GetContentType(ctx, url)
}

// GetM3U8ToMP4Stream menjalankan ffmpeg untuk mengonversi m3u8 langsung menjadi stream MP4 via pipe dengan stderr logger
func GetM3U8ToMP4Stream(ctx context.Context, m3u8URL string, logger *zap.Logger) (io.ReadCloser, error) {
	pr, pw := io.Pipe()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-allowed_extensions", "ALL",
		"-hwaccel", "auto",
		"-i", m3u8URL,
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "frag_keyframe+empty_moov+faststart",
		"-f", "mp4",
		"pipe:1",
	)

	cmd.Stdout = pw

	// Tangkap stderr ke dalam buffer untuk keperluan logging analisa bug
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	go func() {
		logger.Info("Memulai proses konversi FFmpeg untuk M3U8", zap.String("url", m3u8URL))
		err := cmd.Run()
		if err != nil {
			logger.Error("FFmpeg gagal berjalan / terhenti",
				zap.Error(err),
				zap.String("ffmpeg_stderr", stderrBuf.String()),
			)
			_ = pw.CloseWithError(fmt.Errorf("ffmpeg error: %w (stderr: %s)", err, stderrBuf.String()))
			return
		}
		log.LogInfo(ctx, fmt.Sprintf("🎬 FFmpeg sukses memproses M3U8 ke MP4 Stream!\n🔗 URL: %s", m3u8URL))
		logger.Info("FFmpeg selesai mengonversi m3u8 ke mp4 stream dengan sukses")
		_ = pw.Close()
	}()

	return pr, nil
}

func (vc *VideoClient) GetVideoStreamWithHeader(ctx context.Context, videoURL string) (io.ReadCloser, int64, string, error) {
	resp, cancel, err := vc.doRequest(ctx, http.MethodGet, videoURL, vc.streamTimeout)
	if err != nil {
		vc.logger.Errorw("video stream failed", "url", videoURL, "error", err)
		return nil, 0, "", err
	}

	cl := parseContentLength(resp)
	contentType := resp.GetHeader("Content-Type")

	if cl == -1 {
		vc.logger.Infow("Content-Length not provided", "url", videoURL)
	} else {
		vc.logger.Debugw("Content-Length", "bytes", cl, "url", videoURL)
	}

	return &contextBody{
		ReadCloser: resp.Body,
		cancel:     cancel,
	}, cl, contentType, nil
}

// Shortcut global untuk rotated client
func GetVideoStreamWithHeader(ctx context.Context, url string) (io.ReadCloser, int64, string, error) {
	return getRotatedClient().GetVideoStreamWithHeader(ctx, url)
}
