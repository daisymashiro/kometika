package instagram

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	igramHostname = "api-wh.igram.world"
	igramAPIBase  = "api.igram.world"
	igramHMACKey  = "75f2d70d3724f98e4a7d1ffd0ba9cfd907f3ae2632ee159980e2c521bff62358"
	igramStaticTS = 1771418815381
)

func FetchIgram(targetURL string) (*UniversalInstagramData, error) {
	shortcode := extractShortcode(targetURL)
	if shortcode == "" {
		return nil, fmt.Errorf("invalid Instagram URL: missing /p/, /reel/, or /tv/")
	}
	return fetchIGram(targetURL, shortcode)
}

func fetchIGram(targetURL, shortcode string) (*UniversalInstagramData, error) {
	payload, err := buildIGramJSONPayload(targetURL)
	if err != nil {
		return nil, err
	}

	apiURL := fmt.Sprintf("https://%s/api/convert", igramHostname)
	req, err := http.NewRequest("POST", apiURL, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://igram.world/")

	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err == nil {
		return parseSingleResponse(data, shortcode)
	}

	var items []map[string]interface{}
	if err := json.Unmarshal(body, &items); err == nil {
		return parseArrayResponse(items, shortcode)
	}

	return nil, fmt.Errorf("unrecognized response format from iGram API: %s", string(body))
}

// buildIGramJSONPayload membuat signed JSON payload.
// Saya pakai encoder dengan SetEscapeHTML(false) supaya & tidak jadi \u0026.
func buildIGramJSONPayload(targetURL string) (io.Reader, error) {
	nowMs := time.Now().UnixMilli()
	serverMs := getIGramServerTime()
	drift := serverMs - nowMs

	var correction int64
	if drift >= 60000 || drift <= -60000 {
		correction = drift
	}
	ts := nowMs + correction

	// Data yang ditandatangani
	partial := map[string]interface{}{
		"target_url": targetURL,
		"_sc":        0,
		"_ef":        0,
		"_df":        0,
	}

	keys := []string{"_df", "_ef", "_sc", "target_url"}
	var buf strings.Builder
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		fmt.Fprintf(&buf, "%q:", k)
		v := partial[k]
		switch val := v.(type) {
		case string:
			fmt.Fprintf(&buf, "%q", val)
		case int:
			fmt.Fprintf(&buf, "%d", val)
		}
	}
	buf.WriteByte('}')
	dataToSign := buf.String() + strconv.FormatInt(ts, 10)

	keyBytes, err := hex.DecodeString(igramHMACKey)
	if err != nil {
		return nil, err
	}

	mac := hmac.New(sha256.New, keyBytes)
	mac.Write([]byte(dataToSign))
	sig := hex.EncodeToString(mac.Sum(nil))

	final := map[string]interface{}{
		"target_url": targetURL,
		"_sc":        0,
		"_ef":        0,
		"_df":        0,
		"ts":         ts,
		"_ts":        igramStaticTS,
		"_tsc":       correction,
		"_sv":        2,
		"_s":         sig,
	}

	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(final); err != nil {
		return nil, err
	}

	// encoder menambah newline, jadi trim
	return bytes.NewReader(bytes.TrimSpace(out.Bytes())), nil
}

func getIGramServerTime() int64 {
	apiURL := fmt.Sprintf("https://%s/msec", igramAPIBase)
	resp, err := http.Get(apiURL)
	if err != nil {
		return time.Now().UnixMilli()
	}
	defer resp.Body.Close()

	var result struct {
		Msec float64 `json:"msec"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return time.Now().UnixMilli()
	}

	// contoh respons: 1778492412.635
	// itu detik, jadi dikali 1000
	return int64(result.Msec * 1000)
}

func parseSingleResponse(data map[string]interface{}, shortcode string) (*UniversalInstagramData, error) {
	title := ""
	var audioURL string
	var videoURL, coverURL string
	var imageURLs []string

	if meta, ok := data["meta"].(map[string]interface{}); ok {
		if t, ok := meta["title"].(string); ok {
			title = cleanText(t)
		}
		if manifest, ok := meta["dash_manifest"].(string); ok && manifest != "" {
			audioURL = extractAudioFromManifestIgram(manifest)
		}
	}

	// fallback kalau dash_manifest ternyata ada di root
	if audioURL == "" {
		if manifest, ok := data["dash_manifest"].(string); ok && manifest != "" {
			audioURL = extractAudioFromManifestIgram(manifest)
		}
	}

	if thumb, ok := data["thumb"].(string); ok {
		coverURL, _ = getCleanURL(thumb)
	}

	if urls, ok := data["url"].([]interface{}); ok && len(urls) > 0 {
		for _, u := range urls {
			if item, ok := u.(map[string]interface{}); ok {
				mediaURL, _ := item["url"].(string)
				ext, _ := item["ext"].(string)
				clean, _ := getCleanURL(mediaURL)

				if ext == "mp4" && videoURL == "" {
					videoURL = clean
				} else if ext != "mp4" && clean != "" {
					imageURLs = append(imageURLs, clean)
				}
			}
		}
	}

	if videoURL == "" && len(imageURLs) == 0 {
		if sd, ok := data["sd"].(string); ok {
			clean, _ := getCleanURL(sd)
			if clean != "" {
				imageURLs = append(imageURLs, clean)
			}
		}
		if hd, ok := data["hd"].(string); ok {
			clean, _ := getCleanURL(hd)
			if clean != "" {
				imageURLs = append(imageURLs, clean)
			}
		}
	}

	isAlbum := len(imageURLs) > 1 || (videoURL != "" && len(imageURLs) > 0)

	return &UniversalInstagramData{
		ID:        shortcode,
		Title:     title,
		VideoURL:  videoURL,
		AudioURL:  audioURL,
		IsAlbum:   isAlbum,
		ImageURLs: imageURLs,
		CoverURL:  coverURL,
	}, nil
}

func parseArrayResponse(items []map[string]interface{}, shortcode string) (*UniversalInstagramData, error) {
	var videoURL, audioURL, coverURL string
	var imageURLs []string
	title := ""

	for _, item := range items {
		if title == "" {
			if t, ok := item["title"].(string); ok {
				title = cleanText(t)
			}
			if meta, ok := item["meta"].(map[string]interface{}); ok {
				if t, ok := meta["title"].(string); ok {
					title = cleanText(t)
				}
				if manifest, ok := meta["dash_manifest"].(string); ok && manifest != "" && audioURL == "" {
					audioURL = extractAudioFromManifestIgram(manifest)
				}
			}
		}

		if manifest, ok := item["dash_manifest"].(string); ok && manifest != "" && audioURL == "" {
			audioURL = extractAudioFromManifestIgram(manifest)
		}

		if thumb, ok := item["thumb"].(string); ok && coverURL == "" {
			coverURL, _ = getCleanURL(thumb)
		}

		if urls, ok := item["url"].([]interface{}); ok {
			for _, u := range urls {
				if media, ok := u.(map[string]interface{}); ok {
					mediaURL, _ := media["url"].(string)
					ext, _ := media["ext"].(string)
					clean, _ := getCleanURL(mediaURL)

					if ext == "mp4" && videoURL == "" {
						videoURL = clean
					} else if clean != "" {
						imageURLs = append(imageURLs, clean)
					}
				}
			}
		}
	}

	isAlbum := len(items) > 1 || len(imageURLs) > 1

	return &UniversalInstagramData{
		ID:        shortcode,
		Title:     title,
		VideoURL:  videoURL,
		AudioURL:  audioURL,
		IsAlbum:   isAlbum,
		ImageURLs: imageURLs,
		CoverURL:  coverURL,
	}, nil
}

func getCleanURL(rawURL string) (string, error) {
	if rawURL == "" {
		return "", nil
	}

	// Unescape dulu supaya &amp; dan \u0026 tidak ganggu parsing
	rawURL = html.UnescapeString(rawURL)
	rawURL = strings.ReplaceAll(rawURL, `\u0026`, "&")

	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		// kalau parse gagal, tetap kembalikan versi yang sudah dibersihkan
		return rawURL, nil
	}

	q := parsed.Query()
	if uri := q.Get("uri"); uri != "" {
		uri = html.UnescapeString(uri)
		uri = strings.ReplaceAll(uri, `\u0026`, "&")
		if decoded, err := neturl.QueryUnescape(uri); err == nil {
			return decoded, nil
		}
		return uri, nil
	}

	return rawURL, nil
}

// sanitizeXML memperbaiki karakter '&' yang bukan bagian dari entitas XML
func sanitizeXML(s string) string {
	// Pola untuk entitas XML standar dan numerik
	re := regexp.MustCompile(`&(?:[a-zA-Z]+|#\d+);`)
	var result strings.Builder
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			// Cek apakah ini entitas valid
			match := re.FindStringIndex(s[i:])
			if match != nil && match[0] == 0 {
				// Entitas valid, tulis apa adanya
				result.WriteString(s[last:i])
				result.WriteString(s[i : i+match[1]])
				last = i + match[1]
				i += match[1] - 1
				continue
			} else {
				// '&' tidak valid, ganti dengan &amp;
				result.WriteString(s[last:i])
				result.WriteString("&amp;")
				last = i + 1
			}
		}
	}
	result.WriteString(s[last:])
	return result.String()
}

// extractAudioFromManifestIgram mencari audio URL dari manifest XML
func extractAudioFromManifestIgram(manifestXML string) string {
	// Escape HTML entities
	manifestXML = html.UnescapeString(manifestXML)
	manifestXML = strings.ReplaceAll(manifestXML, `\u0026`, "&")
	// Sanitasi XML untuk menangani '&' tidak valid
	manifestXML = sanitizeXML(manifestXML)

	decoder := xml.NewDecoder(strings.NewReader(manifestXML))
	inAudioSet := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("Warning: XML parse error: %v", err)
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "AdaptationSet":
				inAudioSet = false
				for _, attr := range t.Attr {
					name := strings.ToLower(attr.Name.Local)
					val := strings.ToLower(attr.Value)
					if name == "contenttype" && val == "audio" {
						inAudioSet = true
					}
					if name == "mimetype" && strings.HasPrefix(val, "audio/") {
						inAudioSet = true
					}
				}
			case "BaseURL":
				if inAudioSet {
					var base string
					if err := decoder.DecodeElement(&base, &t); err == nil {
						base = strings.TrimSpace(base)
						base = strings.ReplaceAll(base, `\u0026`, "&")
						base = html.UnescapeString(base)
						return base
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "AdaptationSet" {
				inAudioSet = false
			}
		}
	}

	// Fallback regex
	re := regexp.MustCompile(`(?s)<AdaptationSet[^>]*contentType="audio"[^>]*>.*?<BaseURL>([^<]+)</BaseURL>`)
	if m := re.FindStringSubmatch(manifestXML); len(m) > 1 {
		base := strings.TrimSpace(m[1])
		base = strings.ReplaceAll(base, `\u0026`, "&")
		base = html.UnescapeString(base)
		return base
	}
	return ""
}
