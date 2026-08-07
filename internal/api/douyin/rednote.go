// Package douyin scrapes Xiaohongshu (RED note / 小红书) posts.
//
// Port of ParseHub's provider_api/xhs.py with two extra fallbacks: og:meta
// extraction from the same HTML response, and the rednoteapp.app proxy API.
// Primary source (window.__INITIAL_STATE__) needs no cookie for public notes;
// a cookie is only required for private / login-walled posts.
package douyin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36 Edg/132.0.0.0"
	proxyEndpoint    = "https://rednoteapp.app/api/rednoteUrlContent"
	rawImageBase     = "https://sns-img-hw.xhscdn.com/"
)

// Result is a parsed note: either a video post or an image post.
type Result struct {
	Type    string   `json:"type"` // "video" | "image" | "unknown"
	Title   string   `json:"title"`
	Desc    string   `json:"desc,omitempty"`
	Video   string   `json:"video,omitempty"`  // direct CDN stream URL (signed; GET only)
	Cover   string   `json:"cover,omitempty"`  // video post thumbnail
	Photos  []string `json:"photos,omitempty"` // image post photos (watermark-free)
	Source  string   `json:"source"`           // "direct" | "og" | "proxy"
	NoteURL string   `json:"note_url,omitempty"`
}

// API scrapes Xiaohongshu posts. Cookie is optional; public notes need none.
type API struct {
	Cookie    string // optional, e.g. "web_session=...; a1=..."
	Proxy     string // optional HTTP proxy URL
	UserAgent string
}

func (a *API) userAgent() string {
	if a.UserAgent != "" {
		return a.UserAgent
	}
	return defaultUserAgent
}

func (a *API) httpClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if a.Proxy != "" {
		if u, err := url.Parse(a.Proxy); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}

// Parse resolves rawURL (xhslink.com short links included) and returns the
// note. Fallback chain: page INITIAL_STATE -> og:meta -> rednoteapp proxy.
// Pages that resolve to a xiaohongshu.com host are re-served from
// rednote.com, where the session cookie in API.Cookie is scoped.
func (a *API) Parse(ctx context.Context, rawURL string) (*Result, error) {
	client := a.httpClient()
	pageHTML, finalURL, err := a.getPage(ctx, client, rawURL)
	if err != nil {
		return nil, err
	}
	if alt := rewriteRednoteHost(finalURL); alt != "" && alt != finalURL {
		if pageHTML, finalURL, err = a.getPage(ctx, client, alt); err != nil {
			return nil, err
		}
	}
	if r, err := parseInitialState(pageHTML); err == nil {
		r.Source = "direct"
		r.NoteURL = finalURL
		return r, nil
	}
	if r, ok := parseOG(pageHTML); ok {
		r.Source = "og"
		r.NoteURL = finalURL
		return r, nil
	}
	r, err := a.fetchProxy(ctx, client, rawURL)
	if err != nil {
		return nil, fmt.Errorf("all sources failed (page, og, proxy): %v", err)
	}
	return r, nil
}

// getPage fetches the note page following redirects; returns the resolved URL.
func (a *API) getPage(ctx context.Context, client *http.Client, rawURL string) (html_, finalURL string, err error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if rerr != nil {
			return "", "", rerr
		}
		req.Header.Set("User-Agent", a.userAgent())
		if a.Cookie != "" {
			req.Header.Set("Cookie", a.Cookie)
		}
		resp, rerr := client.Do(req)
		switch {
		case rerr != nil:
			lastErr = rerr
		case resp.StatusCode >= 400:
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			resp.Body.Close()
		default:
			b, rerr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if rerr != nil {
				lastErr = rerr
			} else {
				return string(b), resp.Request.URL.String(), nil
			}
		}
		if attempt+1 < 3 {
			select {
			case <-ctx.Done():
				return "", "", ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	return "", "", lastErr
}

// rewriteRednoteHost maps any xiaohongshu.com host to its rednote.com twin,
// keeping path and query (xsec_token included) intact.
func rewriteRednoteHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Host)
	switch {
	case host == "www.xiaohongshu.com":
		u.Host = "www.rednote.com"
	case host == "xiaohongshu.com":
		u.Host = "rednote.com"
	case strings.HasSuffix(host, ".xiaohongshu.com"):
		u.Host = strings.TrimSuffix(host, ".xiaohongshu.com") + ".rednote.com"
	default:
		return ""
	}
	return u.String()
}

var (
	scriptRe    = regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)
	undefinedRe = regexp.MustCompile(`\bundefined\b`)
)

// parseInitialState extracts the note from the page's window.__INITIAL_STATE__.
func parseInitialState(pageHTML string) (*Result, error) {
	raw := ""
	for _, m := range scriptRe.FindAllStringSubmatch(pageHTML, -1) {
		if strings.HasPrefix(strings.TrimSpace(m[1]), "window.__INITIAL_STATE__") {
			raw = m[1]
			break
		}
	}
	if raw == "" {
		return nil, errors.New("no __INITIAL_STATE__ script")
	}
	if i := strings.Index(raw, "="); i >= 0 {
		raw = raw[i+1:]
	}
	js := undefinedRe.ReplaceAllString(strings.TrimRight(strings.TrimSpace(raw), ";"), "null")
	var data map[string]any
	if err := json.Unmarshal([]byte(js), &data); err != nil {
		return nil, err
	}
	note, ok := noteFromState(data)
	if !ok {
		return nil, errors.New("note missing (login/verification required?)")
	}
	r := &Result{
		Type:  "image",
		Title: strings.TrimSuffix(firstString(note, "title"), " - 小红书"),
		Desc:  firstString(note, "desc"),
	}
	if video := videoStreamURL(asMap(note["video"])); video != "" {
		r.Video = video
		r.Type = "video"
	}
	for _, item := range asSlice(note["imageList"]) {
		u := getRawImageURL(firstString(asMap(item), "urlDefault"))
		if u == "" {
			continue
		}
		if r.Type == "video" {
			if r.Cover == "" {
				r.Cover = u
			}
		} else {
			r.Photos = append(r.Photos, u)
		}
	}
	if (r.Type == "image" && len(r.Photos) == 0) || (r.Type == "video" && r.Video == "") {
		return nil, errors.New("note without media")
	}
	return r, nil
}

func noteFromState(data map[string]any) (map[string]any, bool) {
	note := asMap(data["note"])
	if len(note) == 0 {
		return nil, false
	}
	fid := firstString(note, "firstNoteId")
	if fid == "" {
		return nil, false
	}
	detail := asMap(asMap(note["noteDetailMap"])[fid])
	n, ok := detail["note"].(map[string]any)
	return n, ok && len(n) > 0
}

// videoStreamURL picks a playable stream. Xiaohongshu serves codec-keyed
// streams (h264/av1/h265/h266); the rednote.com mirror rekeys them as
// EF4/EF5/EF6/EF7. Prefer an entry flagged defaultStream, else first hit
// (EF4 is rednote's H.264-equivalent, highest quality first).
func videoStreamURL(video map[string]any) string {
	stream := asMap(asMap(video["media"])["stream"])
	var fallback string
	for _, codec := range []string{"h264", "EF4", "av1", "EF5", "h265", "EF6", "h266", "EF7"} {
		for _, item := range asSlice(stream[codec]) {
			m := asMap(item)
			u := firstString(m, "masterUrl")
			if u == "" {
				if a := asSlice(m["backupUrls"]); len(a) > 0 {
					u, _ = a[0].(string)
				}
			}
			if u == "" {
				continue
			}
			if firstInt(m, "defaultStream") == 1 {
				return u
			}
			if fallback == "" {
				fallback = u
			}
		}
	}
	return fallback
}

var (
	ogTitleRe = regexp.MustCompile(`(?is)<meta[^>]*property="og:title"[^>]*content="([^"]*)"`)
	ogVideoRe = regexp.MustCompile(`(?is)<meta[^>]*property="og:video"[^>]*content="([^"]*)"`)
	ogImageRe = regexp.MustCompile(`(?is)<meta[^>]*property="og:image"[^>]*content="([^"]*)"`)
)

// parseOG is a lighter fallback that works off the same HTML when
// __INITIAL_STATE__ is missing (e.g. login-walled or JS-rendered pages).
func parseOG(pageHTML string) (*Result, bool) {
	r := &Result{}
	if m := ogTitleRe.FindStringSubmatch(pageHTML); m != nil {
		r.Title = strings.TrimSpace(html.UnescapeString(m[1]))
		r.Title = strings.TrimSuffix(r.Title, " - 小红书")
	}
	if m := ogVideoRe.FindStringSubmatch(pageHTML); m != nil {
		r.Video = strings.TrimSpace(m[1])
	}
	for _, m := range ogImageRe.FindAllStringSubmatch(pageHTML, -1) {
		u := strings.TrimSpace(m[1])
		if !strings.Contains(u, "xhscdn.com") { // skip site-logo placeholders
			continue
		}
		if !strings.HasPrefix(u, "http") {
			u = "https:" + u
		}
		r.Photos = append(r.Photos, u)
	}
	switch {
	case r.Video != "":
		r.Type = "video"
		if len(r.Photos) > 0 {
			r.Cover, r.Photos = r.Photos[0], nil
		}
	case len(r.Photos) > 0:
		r.Type = "image"
	default:
		return nil, false
	}
	if r.Title == "" {
		return nil, false
	}
	return r, true
}

// proxyResponse is the shape returned by the rednoteapp.app fallback API.
type proxyResponse struct {
	Title     string      `json:"title"`
	Content   string      `json:"content"`
	VideoInfo []proxyItem `json:"videoInfo"`
}

type proxyItem struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (a *API) fetchProxy(ctx context.Context, client *http.Client, rawURL string) (*Result, error) {
	payload, _ := json.Marshal(map[string]string{"inputInfo": rawURL})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://rednoteapp.app")
	req.Header.Set("Referer", "https://rednoteapp.app/downloader")
	req.Header.Set("User-Agent", a.userAgent())
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("proxy HTTP %d", resp.StatusCode)
	}
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return parseProxyResponse(data)
}

func parseProxyResponse(data map[string]any) (*Result, error) {
	r := &Result{Source: "proxy"}
	r.Title = firstString(data, "title")
	if r.Title == "" {
		r.Title = firstString(data, "content")
	}
	r.Title = strings.TrimSuffix(r.Title, " - 小红书")
	if u := firstInArray(data, "videoInfo", "url"); u != "" {
		r.Video = u
	}
	for _, img := range collectImages(data) {
		r.Photos = append(r.Photos, img)
	}
	switch {
	case r.Video != "":
		r.Type = "video"
		if len(r.Photos) > 0 {
			r.Cover, r.Photos = r.Photos[0], nil
		}
	case len(r.Photos) > 0:
		r.Type = "image"
	default:
		return nil, errors.New("proxy returned no media")
	}
	if r.Title == "" {
		return nil, errors.New("proxy returned no title")
	}
	return r, nil
}

var imageKeyRe = regexp.MustCompile(`(?i)image|photo`)

// collectImages drains any response array whose key mentions image/photo,
// so photo posts keep working even if the proxy renames the field.
func collectImages(data map[string]any) []string {
	var out []string
	for k, v := range data {
		if !imageKeyRe.MatchString(k) {
			continue
		}
		for _, item := range asSlice(v) {
			switch e := item.(type) {
			case string:
				if strings.HasPrefix(e, "http") {
					out = append(out, e)
				}
			case map[string]any:
				if u := firstString(e, "url"); strings.HasPrefix(u, "http") {
					out = append(out, u)
				}
			}
		}
	}
	return out
}

// getTraceID extracts the image path/name that survives CDN re-processing.
func getTraceID(imgURL string) string {
	if m := traceRe.FindStringSubmatch(imgURL); m != nil {
		return m[1] + "/" + m[2]
	}
	if m := tailRe.FindStringSubmatch(imgURL); m != nil {
		return m[1]
	}
	return imgURL
}

// getRawImageURL rebuilds a watermark-free, full-size image URL from the trace id.
func getRawImageURL(imgURL string) string {
	if imgURL == "" {
		return ""
	}
	return rawImageBase + getTraceID(imgURL)
}

var (
	traceRe = regexp.MustCompile(`/[a-f0-9]{32}/(.*)/([^/!]+)(?:!.*)?`)
	tailRe  = regexp.MustCompile(`/([^/!]+)(?:!.*)?$`)
)

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok {
			return s
		}
	}
	return ""
}

func firstInt(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return int64(v)
		case int:
			return int64(v)
		}
	}
	return 0
}

func firstInArray(data map[string]any, key, field string) string {
	for _, item := range asSlice(data[key]) {
		if s, ok := item.(string); ok && s != "" {
			return s
		}
		if m := asMap(item); m != nil {
			if u := firstString(m, field); u != "" {
				return u
			}
		}
	}
	return ""
}
