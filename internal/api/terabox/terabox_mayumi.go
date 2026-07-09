package terabox

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ========== KONSTANTA PRIVATE ==========
const (
	teraboxNDUS       = "YVpRgB8peHuioBg2od16nL6d818WSLZg1nbJ8Tuv"
	teraboxUSERID     = "320067180347"
	teraboxDEVUID     = "TBIMXV2-O_E58C50CDB6B04EF889B1B7B0FE0A57CB-C_0-D_99IEPI7RT-M_0068EB688271-V_8C6A2F6D"
	teraboxUSER_AGENT = "terabox;1.34.0.4;PC;PC-Windows;10.0.19045;WindowsTeraBox"
	proxyAPI          = "https://tera-proxy.givace1540.workers.dev/generate"
)

// ========== PRIVATE STRUCTURES ==========
type teraboxProxyResponse struct {
	Success   bool   `json:"success"`
	StreamURL string `json:"stream_url"`
	ExpiresIn string `json:"expires_in"`
	ID        string `json:"id"`
}

type teraboxThumbnails struct {
	URL1 string `json:"url1,omitempty"`
	URL2 string `json:"url2,omitempty"`
	URL3 string `json:"url3,omitempty"`
	Icon string `json:"icon,omitempty"`
}

type teraboxFileData struct {
	ServerFilename string            `json:"server_filename"`
	MD5            string            `json:"md5"`
	EMD5           string            `json:"emd5"`
	Size           int64             `json:"size"`
	IsAdult        int               `json:"is_adult"`
	Dlink          string            `json:"dlink"`
	Thumbs         teraboxThumbnails `json:"thumbs"`
	LocatedDlinks  []string          `json:"located_dlinks"`
	FullPath       string            `json:"full_path"`
}

// ========== PRIVATE FUNCTIONS ==========
func teraboxSha1Hash(text string) string {
	h := sha1.New()
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

func teraboxGenerateRand() (string, string) {
	salt := "Ng2sz6ktQahkvEkcKIhfak4WrM3r9a86"
	t := strconv.FormatInt(time.Now().Unix(), 10)
	ndusHash := teraboxSha1Hash(teraboxNDUS)
	payload := ndusHash + teraboxUSERID + salt + t + teraboxDEVUID
	randHash := teraboxSha1Hash(payload)
	return randHash, t
}

func teraboxExtractSurl(inputURL string, client *http.Client) string {
	if !strings.Contains(inputURL, "http") {
		return inputURL
	}
	req, _ := http.NewRequest("GET", inputURL, nil)
	req.Header.Set("User-Agent", teraboxUSER_AGENT)
	res, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	finalURL := res.Request.URL.String()
	re := regexp.MustCompile(`(?:surl=|/)([a-zA-Z0-9\-_]+)$`)
	match := re.FindStringSubmatch(finalURL)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func teraboxGenerateID(shortURL string) (string, error) {
	parsed, err := url.Parse(shortURL)
	if err != nil {
		return "", err
	}
	path := parsed.Path
	parts := strings.Split(path, "/s/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid terabox URL format, missing /s/")
	}
	code := parts[1]
	code = strings.Split(code, "?")[0]
	code = strings.Split(code, "/")[0]

	hash := md5.Sum([]byte(code))
	hexHash := hex.EncodeToString(hash[:])
	hexPart := hexHash[:8]
	val, err := strconv.ParseUint(hexPart, 16, 64)
	if err != nil {
		return "", err
	}
	idNum := val % 10000000000
	idStr := fmt.Sprintf("%010d", idNum)
	return idStr, nil
}

func teraboxFormatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %s", float64(bytes)/float64(div), []string{"KB", "MB", "GB", "TB"}[exp])
}

func teraboxGenerateStreamURL(dlinkURL string) (string, error) {
	payload := map[string]string{
		"url": dlinkURL,
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", proxyAPI, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", teraboxUSER_AGENT)

	client := &http.Client{Timeout: 40 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	var proxyRes teraboxProxyResponse
	if err := json.Unmarshal(body, &proxyRes); err != nil {
		return "", err
	}

	if !proxyRes.Success {
		return "", fmt.Errorf("proxy API returned success=false")
	}

	return proxyRes.StreamURL, nil
}

func teraboxGetInfoRaw(client *http.Client, surl string, path string) (string, error) {
	randStr, t := teraboxGenerateRand()
	surlCode := teraboxExtractSurl(surl, client)
	reqURL, _ := url.Parse("https://dm.terabox.com/share/list")
	q := reqURL.Query()
	q.Add("clienttype", "8")
	q.Add("channel", "0")
	q.Add("version", "1.34.0.4")
	q.Add("devuid", teraboxDEVUID)
	q.Add("rand", randStr)
	q.Add("time", t)
	q.Add("vip", "2")
	q.Add("lang", "en")
	q.Add("shorturl", surlCode)
	if path == "" {
		q.Add("root", "1")
	} else {
		q.Add("root", "0")
		q.Add("dir", path)
	}
	reqURL.RawQuery = q.Encode()
	req, _ := http.NewRequest("POST", reqURL.String(), nil)
	req.Header.Set("User-Agent", teraboxUSER_AGENT)
	req.AddCookie(&http.Cookie{Name: "ndus", Value: teraboxNDUS})
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return string(body), nil
}

func teraboxGetLocatedDownloads(client *http.Client, dlink string) ([]string, error) {
	randStr, t := teraboxGenerateRand()
	parsedURL, err := url.Parse(dlink)
	if err != nil {
		return nil, err
	}
	originalQuery := parsedURL.Query()
	pathParts := strings.Split(parsedURL.Path, "/")
	lastPath := pathParts[len(pathParts)-1]
	reqURL, _ := url.Parse("https://dm.terabox.com/rest/2.0/pcs/file")
	q := reqURL.Query()
	q.Add("app_id", "25028")
	q.Add("method", "locatedownload")
	q.Add("path", lastPath)
	q.Add("clienttype", "8")
	q.Add("devuid", teraboxDEVUID)
	q.Add("rand", randStr)
	q.Add("time", t)
	for k, v := range originalQuery {
		if len(v) > 0 {
			q.Add(k, v[0])
		}
	}
	reqURL.RawQuery = q.Encode()
	req, _ := http.NewRequest("POST", reqURL.String(), nil)
	req.Header.Set("User-Agent", teraboxUSER_AGENT)
	req.AddCookie(&http.Cookie{Name: "ndus", Value: teraboxNDUS})
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	var urls []string
	if urlsArray, ok := result["urls"].([]interface{}); ok {
		for _, u := range urlsArray {
			if uMap, ok := u.(map[string]interface{}); ok {
				if urlStr, ok := uMap["url"].(string); ok {
					urls = append(urls, urlStr)
				}
			}
		}
	}
	if bakUrls, ok := result["oversea_bakurls"].([]interface{}); ok {
		for _, u := range bakUrls {
			if uMap, ok := u.(map[string]interface{}); ok {
				if urlStr, ok := uMap["url"].(string); ok {
					urls = append(urls, urlStr)
				}
			}
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no download URLs found")
	}
	return urls, nil
}

func teraboxCollectFiles(client *http.Client, surl, currentPath string, files *[]teraboxFileData) {
	raw, err := teraboxGetInfoRaw(client, surl, currentPath)
	if err != nil {
		logError("Error fetching terabox info",
			zap.String("path", currentPath),
			zap.Error(err))
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		logError("Error parsing terabox JSON",
			zap.String("path", currentPath),
			zap.Error(err))
		return
	}
	list, ok := result["list"].([]interface{})
	if !ok {
		return
	}
	for _, itemRaw := range list {
		item := itemRaw.(map[string]interface{})
		isDir := false
		if v, ok := item["isdir"].(string); ok && v == "1" {
			isDir = true
		} else if v, ok := item["isdir"].(float64); ok && v == 1 {
			isDir = true
		}
		if !isDir {
			serverFilename, _ := item["server_filename"].(string)
			md5, _ := item["md5"].(string)
			emd5, _ := item["emd5"].(string)
			size, _ := item["size"].(float64)
			isAdult, _ := item["is_adult"].(float64)
			dlink, _ := item["dlink"].(string)

			thumbs := teraboxThumbnails{}
			if thumbsRaw, ok := item["thumbs"].(map[string]interface{}); ok {
				if u1, ok := thumbsRaw["url1"].(string); ok {
					thumbs.URL1 = u1
				}
				if u2, ok := thumbsRaw["url2"].(string); ok {
					thumbs.URL2 = u2
				}
				if u3, ok := thumbsRaw["url3"].(string); ok {
					thumbs.URL3 = u3
				}
				if icon, ok := thumbsRaw["icon"].(string); ok {
					thumbs.Icon = icon
				}
			}

			locatedDlinks := []string{}
			if dlink != "" {
				links, err := teraboxGetLocatedDownloads(client, dlink)
				if err != nil {
					logWarn("Failed to get located downloads",
						zap.String("filename", serverFilename),
						zap.Error(err))
				} else {
					locatedDlinks = links
				}
			}

			fullPath := currentPath
			if apiPath, ok := item["path"].(string); ok && apiPath != "" {
				fullPath = apiPath
			} else if fullPath == "" {
				fullPath = "/" + serverFilename
			} else {
				fullPath = fullPath + "/" + serverFilename
			}

			*files = append(*files, teraboxFileData{
				ServerFilename: serverFilename,
				MD5:            md5,
				EMD5:           emd5,
				Size:           int64(size),
				IsAdult:        int(isAdult),
				Dlink:          dlink,
				Thumbs:         thumbs,
				LocatedDlinks:  locatedDlinks,
				FullPath:       fullPath,
			})
		} else {
			if subPath, ok := item["path"].(string); ok {
				teraboxCollectFiles(client, surl, subPath, files)
			}
		}
	}
}

// ========== PUBLIC FUNCTION ==========
// FetchTeraboxDirectUniversal adalah API adapter untuk terabox extractor
// Fungsi ini akan dipanggil oleh FetchTeraboxUniversal sebagai salah satu pilihan API
func FetchTeraboxDirectUniversal(teraboxURL string) ([]TeraboxUniversalData, error) {
	client := &http.Client{Timeout: 40 * time.Second}

	logInfo("Mengekstrak files dari terabox", zap.String("url", teraboxURL))

	var files []teraboxFileData
	teraboxCollectFiles(client, teraboxURL, "", &files)

	if len(files) == 0 {
		return nil, fmt.Errorf("tidak ada file ditemukan")
	}

	logInfo("Files berhasil dikumpulkan", zap.Int("total", len(files)))

	var universalData []TeraboxUniversalData
	for i, file := range files {
		logInfo("Memproses file",
			zap.Int("index", i+1),
			zap.Int("total", len(files)),
			zap.String("filename", file.ServerFilename))

		id, err := teraboxGenerateID(teraboxURL)
		if err != nil {
			logWarn("Gagal generate ID", zap.Error(err))
			id = "unknown"
		}

		fileSize := teraboxFormatFileSize(file.Size)
		thumbnail := file.Thumbs.URL3
		streamURL := ""
		downloadURL := file.Dlink

		if file.Dlink != "" {
			streamURL, err = teraboxGenerateStreamURL(file.Dlink)
			if err != nil {
				logWarn("Gagal generate stream URL",
					zap.String("filename", file.ServerFilename),
					zap.Error(err))
				streamURL = ""
			}
		}

		if downloadURL == "" && len(file.LocatedDlinks) > 0 {
			downloadURL = file.LocatedDlinks[0]
		}

		universalData = append(universalData, TeraboxUniversalData{
			ID:          id,
			FileName:    file.ServerFilename,
			FileSize:    fileSize,
			Thumbnail:   thumbnail,
			StreamURL:   streamURL,
			DownloadURL: downloadURL,
		})
	}

	logInfo("Konversi ke universal format selesai", zap.Int("total", len(universalData)))
	return universalData, nil
}
