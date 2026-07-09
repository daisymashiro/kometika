package instagram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sardanioss/httpcloak/client"
)

// ==================== FUNGSI UTAMA (EXPORTED) ====================

// FetchInstagram mendeteksi tipe URL dan mengambil data media Instagram.
func FetchInstagram(rawURL string) (*UniversalInstagramData, error) {
	if strings.Contains(rawURL, "/stories/") {
		storyID := extractStoryID(rawURL)
		if storyID == "" {
			return nil, fmt.Errorf("invalid story URL")
		}
		return FetchInstagramStory(storyID)
	}
	shortcode := extractShortcode(rawURL)
	if shortcode == "" {
		return nil, fmt.Errorf("invalid Instagram URL")
	}
	return FetchInstagramPost(shortcode)
}

// FetchInstagramPost mengambil data post/reel/IGTV berdasarkan shortcode.
func FetchInstagramPost(shortcode string) (*UniversalInstagramData, error) {
	headers, form := buildPostRequest(shortcode)
	body := strings.NewReader(form.Encode())

	httpClient := client.NewClient("chrome-latest",
		client.WithTimeout(1*time.Minute),
		client.WithRetry(2),
	)
	defer httpClient.Close()

	ctx := context.Background()
	customHeaders := map[string][]string{
		"Content-Type": {"application/x-www-form-urlencoded"},
	}
	for k, v := range headers {
		customHeaders[k] = []string{v}
	}

	resp, err := httpClient.Post(ctx, "https://www.instagram.com/graphql/query/", body, customHeaders)
	if err != nil {
		return nil, fmt.Errorf("request gagal: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("baca response gagal: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse JSON gagal: %w", err)
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("data tidak ditemukan")
	}
	shortcodeMedia, ok := data["xdt_shortcode_media"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("shortcode_media tidak ditemukan")
	}

	numericID, _ := shortcodeMedia["id"].(string)
	if numericID == "" {
		return nil, fmt.Errorf("numeric ID tidak ditemukan")
	}

	// Caption
	title := ""
	if edgeCaption, ok := shortcodeMedia["edge_media_to_caption"].(map[string]interface{}); ok {
		if edges, ok := edgeCaption["edges"].([]interface{}); ok && len(edges) > 0 {
			if firstEdge, ok := edges[0].(map[string]interface{}); ok {
				if node, ok := firstEdge["node"].(map[string]interface{}); ok {
					if text, ok := node["text"].(string); ok {
						title = cleanText(text)
					}
				}
			}
		}
	}

	typename, _ := shortcodeMedia["__typename"].(string)
	isAlbum := (typename == "GraphSidecar" || typename == "XDTGraphSidecar")

	var videoURL, audioURL, coverURL string
	var imageURLs []string

	if isAlbum {
		if sidecar, ok := shortcodeMedia["edge_sidecar_to_children"].(map[string]interface{}); ok {
			if edges, ok := sidecar["edges"].([]interface{}); ok {
				for _, edge := range edges {
					if nodeMap, ok := edge.(map[string]interface{}); ok {
						if node, ok := nodeMap["node"].(map[string]interface{}); ok {
							nodeTypename, _ := node["__typename"].(string)
							displayURL, _ := node["display_url"].(string)
							vidURL, _ := node["video_url"].(string)

							if nodeTypename == "GraphVideo" || nodeTypename == "XDTGraphVideo" {
								if videoURL == "" && vidURL != "" {
									videoURL = vidURL
								}
								if coverURL == "" {
									coverURL = displayURL
								}
								imageURLs = append(imageURLs, vidURL)
							} else {
								if displayURL != "" {
									imageURLs = append(imageURLs, displayURL)
									if coverURL == "" {
										coverURL = displayURL
									}
								}
							}
						}
					}
				}
			}
		}
	} else {
		displayURL, _ := shortcodeMedia["display_url"].(string)
		vidURL, _ := shortcodeMedia["video_url"].(string)

		if typename == "GraphVideo" || typename == "XDTGraphVideo" {
			videoURL = vidURL
			coverURL = displayURL
			if dashInfo, ok := shortcodeMedia["dash_info"].(map[string]interface{}); ok {
				if manifest, ok := dashInfo["video_dash_manifest"].(string); ok && manifest != "" {
					audioURL = extractAudioFromManifest(manifest)
				}
			}
		} else {
			imageURLs = append(imageURLs, displayURL)
			coverURL = displayURL
		}
	}

	return &UniversalInstagramData{
		ID:        numericID,
		Title:     title,
		VideoURL:  videoURL,
		AudioURL:  audioURL,
		IsAlbum:   isAlbum,
		ImageURLs: imageURLs,
		CoverURL:  coverURL,
	}, nil
}

// FetchInstagramStory mengambil data story Instagram berdasarkan numeric story ID.
func FetchInstagramStory(storyID string) (*UniversalInstagramData, error) {
	headers, form := buildStoryRequest(storyID)
	body := strings.NewReader(form.Encode())

	httpClient := client.NewClient("chrome-latest",
		client.WithTimeout(60*time.Second),
		client.WithRetry(2),
	)
	defer httpClient.Close()

	ctx := context.Background()
	customHeaders := map[string][]string{
		"Content-Type": {"application/x-www-form-urlencoded"},
	}
	for k, v := range headers {
		customHeaders[k] = []string{v}
	}

	resp, err := httpClient.Post(ctx, "https://www.instagram.com/graphql/query/", body, customHeaders)
	if err != nil {
		return nil, fmt.Errorf("request gagal: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("baca response gagal: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse JSON gagal: %w", err)
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("data tidak ditemukan")
	}
	reel, ok := data["reel"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("reel tidak ditemukan")
	}
	items, ok := reel["items"].([]interface{})
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("tidak ada item story")
	}
	firstItem, ok := items[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("item story tidak valid")
	}

	numericID, _ := firstItem["id"].(string)
	if numericID == "" {
		numericID, _ = reel["id"].(string)
	}

	title := ""
	if caption, ok := firstItem["caption"].(string); ok {
		title = cleanText(caption)
	} else if edgeMediaToCaption, ok := firstItem["edge_media_to_caption"].(map[string]interface{}); ok {
		if edges, ok := edgeMediaToCaption["edges"].([]interface{}); ok && len(edges) > 0 {
			if edge, ok := edges[0].(map[string]interface{}); ok {
				if node, ok := edge["node"].(map[string]interface{}); ok {
					if text, ok := node["text"].(string); ok {
						title = cleanText(text)
					}
				}
			}
		}
	}

	mediaType, _ := firstItem["media_type"].(float64)
	isVideo := mediaType == 2

	var videoURL, audioURL, coverURL string
	var imageURLs []string

	if isVideo {
		if videoVersions, ok := firstItem["video_versions"].([]interface{}); ok && len(videoVersions) > 0 {
			if v, ok := videoVersions[0].(map[string]interface{}); ok {
				videoURL, _ = v["url"].(string)
			}
		}
		if imageVersions, ok := firstItem["image_versions2"].(map[string]interface{}); ok {
			if candidates, ok := imageVersions["candidates"].([]interface{}); ok && len(candidates) > 0 {
				if img, ok := candidates[0].(map[string]interface{}); ok {
					coverURL, _ = img["url"].(string)
				}
			}
		}
		if dashInfo, ok := firstItem["dash_info"].(map[string]interface{}); ok {
			if manifest, ok := dashInfo["video_dash_manifest"].(string); ok && manifest != "" {
				audioURL = extractAudioFromManifest(manifest)
			}
		}
	} else {
		if imageVersions, ok := firstItem["image_versions2"].(map[string]interface{}); ok {
			if candidates, ok := imageVersions["candidates"].([]interface{}); ok && len(candidates) > 0 {
				best := candidates[0].(map[string]interface{})
				for _, c := range candidates {
					cand := c.(map[string]interface{})
					w1, _ := best["width"].(float64)
					w2, _ := cand["width"].(float64)
					if w2 > w1 {
						best = cand
					}
				}
				coverURL, _ = best["url"].(string)
				imageURLs = []string{coverURL}
			}
		}
	}

	return &UniversalInstagramData{
		ID:        numericID,
		Title:     title,
		VideoURL:  videoURL,
		AudioURL:  audioURL,
		IsAlbum:   false,
		ImageURLs: imageURLs,
		CoverURL:  coverURL,
	}, nil
}

func extractStoryID(rawURL string) string {
	re := regexp.MustCompile(`instagram\.com/stories/[a-zA-Z0-9._]+/(\d+)`)
	m := re.FindStringSubmatch(rawURL)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// buildPostRequest dan buildStoryRequest tetap internal, tidak diexport.
func buildPostRequest(shortcode string) (map[string]string, url.Values) {
	randBytes := func(n int) string {
		b := make([]byte, n)
		rand.Read(b)
		return hex.EncodeToString(b)
	}
	csrf := randBytes(32)
	did := randBytes(24)
	mid := randBytes(24)
	lsd := randBytes(8)
	dyn := randBytes(154)
	csr := randBytes(154)
	jazoest := strconv.Itoa(1 + time.Now().Nanosecond()%10000)
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	headers := map[string]string{
		"x-ig-app-id":        "936619743392459",
		"X-FB-LSD":           lsd,
		"X-CSRFToken":        csrf,
		"X-Bloks-Version-Id": "6309c8d03d8a3f47a1658ba38b304a3f837142ef5f637ebf1f8f52d4b802951e",
		"x-asbd-id":          "129477",
		"User-Agent":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
		"cookie":             fmt.Sprintf("csrftoken=%s; ig_did=%s; mid=%s;", csrf, did, mid),
	}
	form := url.Values{}
	form.Set("__d", "www")
	form.Set("__a", "1")
	form.Set("__s", "::"+randBytes(6))
	form.Set("__hs", "20126.HYP:instagram_web_pkg.2.1...0")
	form.Set("__req", "b")
	form.Set("__ccg", "EXCELLENT")
	form.Set("__rev", "1019933358")
	form.Set("__hsi", "7436540909012459023")
	form.Set("__dyn", dyn)
	form.Set("__csr", csr)
	form.Set("__user", "0")
	form.Set("__comet_req", "7")
	form.Set("libav", "0")
	form.Set("dpr", "2")
	form.Set("lsd", lsd)
	form.Set("jazoest", jazoest)
	form.Set("__spin_r", "1019933358")
	form.Set("__spin_b", "trunk")
	form.Set("__spin_t", ts)
	form.Set("fb_api_caller_class", "RelayModern")
	form.Set("fb_api_req_friendly_name", "PolarisPostActionLoadPostQueryQuery")
	form.Set("server_timestamps", "true")
	form.Set("doc_id", "8845758582119845")
	vars := fmt.Sprintf(`{"shortcode":"%s","fetch_tagged_user_count":null,"hoisted_comment_id":null,"hoisted_reply_id":null}`, shortcode)
	form.Set("variables", vars)
	return headers, form
}

func buildStoryRequest(storyID string) (map[string]string, url.Values) {
	randBytes := func(n int) string {
		b := make([]byte, n)
		rand.Read(b)
		return hex.EncodeToString(b)
	}
	csrf := randBytes(32)
	did := randBytes(24)
	mid := randBytes(24)
	lsd := randBytes(8)
	dyn := randBytes(154)
	csr := randBytes(154)
	jazoest := strconv.Itoa(1 + time.Now().Nanosecond()%10000)
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	headers := map[string]string{
		"x-ig-app-id":        "936619743392459",
		"X-FB-LSD":           lsd,
		"X-CSRFToken":        csrf,
		"X-Bloks-Version-Id": "6309c8d03d8a3f47a1658ba38b304a3f837142ef5f637ebf1f8f52d4b802951e",
		"x-asbd-id":          "129477",
		"User-Agent":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
		"cookie":             fmt.Sprintf("csrftoken=%s; ig_did=%s; mid=%s;", csrf, did, mid),
	}
	form := url.Values{}
	form.Set("__d", "www")
	form.Set("__a", "1")
	form.Set("__s", "::"+randBytes(6))
	form.Set("__hs", "20126.HYP:instagram_web_pkg.2.1...0")
	form.Set("__req", "b")
	form.Set("__ccg", "EXCELLENT")
	form.Set("__rev", "1019933358")
	form.Set("__hsi", "7436540909012459023")
	form.Set("__dyn", dyn)
	form.Set("__csr", csr)
	form.Set("__user", "0")
	form.Set("__comet_req", "7")
	form.Set("libav", "0")
	form.Set("dpr", "2")
	form.Set("lsd", lsd)
	form.Set("jazoest", jazoest)
	form.Set("__spin_r", "1019933358")
	form.Set("__spin_b", "trunk")
	form.Set("__spin_t", ts)
	form.Set("fb_api_caller_class", "RelayModern")
	form.Set("fb_api_req_friendly_name", "PolarisStoryReelQuery")
	form.Set("server_timestamps", "true")
	form.Set("doc_id", "24218456672618399")
	vars := fmt.Sprintf(`{"reel_ids":["%s"],"tag_names":[],"location_ids":[],"highlight_ids":[],"precomposed_overlay":false,"show_story_viewer_list":true,"story_viewer_fetch_count":50,"story_viewer_cursor":"","stories_video_dash_manifest_version":"0.1.0"}`, storyID)
	form.Set("variables", vars)
	return headers, form
}

type mpdRoot struct {
	Period struct {
		AdaptationSets []struct {
			ContentType     string `xml:"contentType,attr"`
			Representations []struct {
				BaseURL string `xml:"BaseURL"`
			} `xml:"Representation"`
		} `xml:"AdaptationSet"`
	} `xml:"Period"`
}

func extractAudioFromManifest(manifestXML string) string {
	var mpd mpdRoot
	if err := xml.Unmarshal([]byte(manifestXML), &mpd); err != nil {
		return ""
	}
	for _, as := range mpd.Period.AdaptationSets {
		if as.ContentType == "audio" {
			for _, rep := range as.Representations {
				if rep.BaseURL != "" {
					return rep.BaseURL
				}
			}
		}
	}
	return ""
}
