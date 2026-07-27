package instagram

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	fastdlKey, _   = hex.DecodeString("1c3b62b60c1f6accb9f03bfc00839fa3346d13b92b3a7b691a533b7ed01aa52c")
	fastdlSessionTS = "1784922230712"
)

// FetchFastdlApp mengambil data Instagram via fastdl.app API
func FetchFastdlApp(instaURL string) (*UniversalInstagramData, error) {
	instaURL = strings.TrimSpace(instaURL)
	shortcode := extractShortcode(instaURL)

	if isStoryURL(instaURL) {
		return fetchFastdlStory(instaURL, shortcode)
	}
	return fetchFastdlConvert(instaURL, shortcode)
}

// --- helpers ---

func isStoryURL(rawURL string) bool {
	return regexp.MustCompile(`/stories/`).MatchString(rawURL)
}

func fastdlSign(input string) string {
	mac := hmac.New(sha256.New, fastdlKey)
	mac.Write([]byte(input))
	return hex.EncodeToString(mac.Sum(nil))
}

func fastdlSortedJSON(obj map[string]interface{}) string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteByte('"')
		buf.WriteString(escapeJSONStr(k))
		buf.WriteString(`":`)
		writeJSONVal(&buf, obj[k])
	}
	buf.WriteByte('}')
	return buf.String()
}

func writeJSONVal(buf *bytes.Buffer, v interface{}) {
	switch val := v.(type) {
	case string:
		buf.WriteByte('"')
		buf.WriteString(escapeJSONStr(val))
		buf.WriteByte('"')
	case int64:
		buf.WriteString(strconv.FormatInt(val, 10))
	case int:
		buf.WriteString(strconv.Itoa(val))
	case float64:
		buf.WriteString(strconv.FormatFloat(val, 'f', -1, 64))
	case bool:
		buf.WriteString(strconv.FormatBool(val))
	case nil:
		buf.WriteString("null")
	default:
		buf.WriteString(fastdlJSON(v))
	}
}

func escapeJSONStr(s string) string {
	var buf bytes.Buffer
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

func fastdlJSON(v interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return strings.TrimRight(buf.String(), "\n")
}

func fastdlURLEnc(s string) string {
	r := strings.NewReplacer("=", "%3D", "/", "%2F", "?", "%3F", "&", "%26", ":", "%3A")
	return r.Replace(s)
}

// --- HTTP client ---

func fastdlReq(url, contentType, body string) *http.Request {
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Origin", "https://fastdl.app")
	req.Header.Set("Referer", "https://fastdl.app/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	return req
}

func fastdlDo(req *http.Request) ([]byte, error) {
	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke fastdl.app gagal: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca respons fastdl.app: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fastdl.app mengembalikan status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// --- Reels / Posts (form-urlencoded) ---

func fetchFastdlConvert(url, shortcode string) (*UniversalInstagramData, error) {
	ts := time.Now().UnixMilli()
	sig := fastdlSign(url + strconv.FormatInt(ts, 10))

	body := "sf_url=" + fastdlURLEnc(url) +
		"&ts=" + strconv.FormatInt(ts, 10) +
		"&_ts=" + fastdlSessionTS +
		"&_tsc=0&_sv=2&_s=" + sig

	req := fastdlReq("https://api-wh.fastdl.app/api/convert",
		"application/x-www-form-urlencoded;charset=UTF-8", body)
	data, err := fastdlDo(req)
	if err != nil {
		return nil, err
	}
	return parseFastdlConvert(data, shortcode)
}

func parseFastdlConvert(data []byte, shortcode string) (*UniversalInstagramData, error) {
	// Coba carousel (array)
	var carousel []struct {
		URL []struct {
			URL string `json:"url"`
		} `json:"url"`
		Meta struct {
			Title     string `json:"title"`
			Shortcode string `json:"shortcode"`
		} `json:"meta"`
		Thumb string `json:"thumb"`
	}
	if err := json.Unmarshal(data, &carousel); err == nil && len(carousel) > 0 {
		id := shortcode
		if id == "" {
			id = carousel[0].Meta.Shortcode
		}
		res := UniversalInstagramData{
			ID:       generateNumericID(id),
			Title:    cleanText(carousel[0].Meta.Title),
			IsAlbum:  true,
			CoverURL: carousel[0].Thumb,
		}
		for _, item := range carousel {
			if len(item.URL) > 0 {
				res.ImageURLs = append(res.ImageURLs, item.URL[0].URL)
			}
		}
		return &res, nil
	}

	// Coba single (reel atau foto)
	var single struct {
		URL []struct {
			URL     string `json:"url"`
			Quality int    `json:"quality"`
			Ext     string `json:"ext"`
		} `json:"url"`
		Meta struct {
			Title     string `json:"title"`
			Shortcode string `json:"shortcode"`
		} `json:"meta"`
		Thumb string `json:"thumb"`
	}
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("gagal parsing respons fastdl.app: %v", err)
	}
	if len(single.URL) == 0 && single.Meta.Shortcode == "" {
		return nil, fmt.Errorf("respons fastdl.app kosong atau tidak valid")
	}

	id := shortcode
	if id == "" {
		id = single.Meta.Shortcode
	}

	res := UniversalInstagramData{
		ID:       generateNumericID(id),
		Title:    cleanText(single.Meta.Title),
		CoverURL: single.Thumb,
	}

	var bestVideo string
	maxQ := -1
	for _, v := range single.URL {
		switch strings.ToLower(v.Ext) {
		case "mp4", "mov", "webm":
			if v.Quality > maxQ {
				maxQ = v.Quality
				bestVideo = v.URL
			}
		default:
			res.ImageURLs = append(res.ImageURLs, v.URL)
		}
	}

	switch {
	case bestVideo != "":
		res.VideoURL = bestVideo
	case len(res.ImageURLs) == 1:
		res.CoverURL = res.ImageURLs[0]
		fallthrough
	case len(res.ImageURLs) > 0:
		res.IsAlbum = false
	default:
		return nil, fmt.Errorf("tidak ada media valid dalam respons fastdl.app")
	}
	return &res, nil
}

// --- Story (JSON) ---

func fetchFastdlStory(url, shortcode string) (*UniversalInstagramData, error) {
	ts := time.Now().UnixMilli()

	inputBody := map[string]interface{}{"url": url}
	jsonStr := fastdlSortedJSON(inputBody)
	message := jsonStr + strconv.FormatInt(ts, 10)
	sig := fastdlSign(message)

	finalBody := map[string]interface{}{
		"url":  url,
		"ts":   ts,
		"_ts":  fastdlSessionTS,
		"_tsc": 0,
		"_sv":  2,
		"_s":   sig,
	}
	finalJSON := fastdlJSON(finalBody)

	req := fastdlReq("https://api-wh.fastdl.app/api/v1/instagram/story",
		"application/json;charset=UTF-8", finalJSON)
	data, err := fastdlDo(req)
	if err != nil {
		return nil, err
	}
	return parseFastdlStory(data, shortcode)
}

func parseFastdlStory(data []byte, shortcode string) (*UniversalInstagramData, error) {
	var wrapper struct {
		Result []struct {
			Pk      string `json:"pk"`
			TakenAt int64  `json:"taken_at"`
			ImageVersions2 struct {
				Candidates []struct {
					URL string `json:"url"`
				} `json:"candidates"`
			} `json:"image_versions2"`
			VideoVersions []struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"video_versions"`
			User    *struct {
				Username string `json:"username"`
			} `json:"user"`
			HasAudio bool `json:"has_audio"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("gagal parsing story fastdl.app: %v", err)
	}
	if len(wrapper.Result) == 0 {
		return nil, fmt.Errorf("tidak ada story ditemukan")
	}

	var username string
	if wrapper.Result[0].User != nil {
		username = wrapper.Result[0].User.Username
	}

	title := "Story dari @" + username
	if username == "" {
		title = "Instagram Story"
	}

	id := shortcode
	if id == "" {
		id = wrapper.Result[0].Pk
	}

	res := UniversalInstagramData{
		ID:    generateNumericID(id),
		Title: cleanText(title),
	}

	var imageURLs []string
	var videoURL, coverURL string

	for _, item := range wrapper.Result {
		if len(item.ImageVersions2.Candidates) > 0 && coverURL == "" {
			coverURL = item.ImageVersions2.Candidates[0].URL
		}

		if len(item.VideoVersions) > 0 {
			if videoURL == "" {
				best := item.VideoVersions[0]
				for _, v := range item.VideoVersions[1:] {
					if v.Width*v.Height > best.Width*best.Height {
						best = v
					}
				}
				videoURL = best.URL
			}
		} else if len(item.ImageVersions2.Candidates) > 0 {
			imageURLs = append(imageURLs, item.ImageVersions2.Candidates[0].URL)
		}
	}

	res.VideoURL = videoURL
	res.ImageURLs = imageURLs
	res.CoverURL = coverURL
	res.IsAlbum = len(wrapper.Result) > 1

	return &res, nil
}
