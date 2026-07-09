package api

import (
	"bytes"
	"errors"
	"io"
	stdmime "mime"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

type ContentCategory string

const (
	ContentUnknown  ContentCategory = "unknown"
	ContentVideo    ContentCategory = "video"
	ContentAudio    ContentCategory = "audio"
	ContentImage    ContentCategory = "image"
	ContentDocument ContentCategory = "document"
	ContentArchive  ContentCategory = "archive"
	ContentBinary   ContentCategory = "binary"
	ContentFont     ContentCategory = "font"
)

var mimeToExtension = map[string]string{
	// Video
	"video/mp4":        ".mp4",
	"video/x-msvideo":  ".avi",
	"video/x-matroska": ".mkv",
	"video/webm":       ".webm",
	"video/quicktime":  ".mov",
	"video/x-flv":      ".flv",
	"video/3gpp":       ".3gp",
	"video/mpeg":       ".mpeg",
	"video/ogg":        ".ogv",
	"video/x-ms-wmv":   ".wmv",

	// Audio
	"audio/mpeg":      ".mp3",
	"audio/mp4":       ".m4a",
	"audio/ogg":       ".ogg",
	"audio/wav":       ".wav",
	"audio/webm":      ".weba",
	"audio/aac":       ".aac",
	"audio/flac":      ".flac",
	"audio/x-flac":    ".flac",
	"audio/x-wav":     ".wav",
	"audio/x-ms-wma":  ".wma",
	"audio/opus":      ".opus",
	"application/ogg": ".ogg",

	// Image
	"image/jpeg":    ".jpg",
	"image/png":     ".png",
	"image/gif":     ".gif",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
	"image/bmp":     ".bmp",
	"image/x-icon":  ".ico",
	"image/tiff":    ".tiff",
	"image/avif":    ".avif",
	"image/heic":    ".heic",
	"image/heif":    ".heif",

	// Document
	"application/pdf": ".pdf",

	"application/msword": ".doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",

	"application/vnd.ms-excel": ".xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": ".xlsx",

	"application/vnd.ms-powerpoint":                                             ".ppt",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",

	"application/vnd.oasis.opendocument.text":         ".odt",
	"application/vnd.oasis.opendocument.spreadsheet":  ".ods",
	"application/vnd.oasis.opendocument.presentation": ".odp",

	"application/rtf": ".rtf",

	"text/plain":      ".txt",
	"text/csv":        ".csv",
	"text/html":       ".html",
	"text/css":        ".css",
	"text/javascript": ".js",

	"application/javascript": ".js",
	"application/json":       ".json",
	"application/xml":        ".xml",
	"text/xml":               ".xml",

	// Archive
	"application/zip":              ".zip",
	"application/x-rar-compressed": ".rar",
	"application/vnd.rar":          ".rar",
	"application/x-7z-compressed":  ".7z",
	"application/gzip":             ".gz",
	"application/x-gzip":           ".gz",
	"application/x-tar":            ".tar",
	"application/x-bzip2":          ".bz2",
	"application/x-xz":             ".xz",

	// Binary
	"application/octet-stream":                ".bin",
	"application/x-msdownload":                ".exe",
	"application/x-executable":                ".bin",
	"application/x-deb":                       ".deb",
	"application/x-rpm":                       ".rpm",
	"application/vnd.android.package-archive": ".apk",

	// Font
	"font/woff":  ".woff",
	"font/woff2": ".woff2",
	"font/ttf":   ".ttf",
	"font/otf":   ".otf",

	// E-book
	"application/epub+zip":           ".epub",
	"application/x-mobipocket-ebook": ".mobi",
}

var exactMimeToCategory = map[string]ContentCategory{
	// SVG lebih aman dianggap document karena berbasis XML.
	"image/svg+xml": ContentDocument,

	// Audio yang kadang terdeteksi sebagai application/*
	"application/ogg": ContentAudio,

	// Archive
	"application/zip":              ContentArchive,
	"application/x-rar-compressed": ContentArchive,
	"application/vnd.rar":          ContentArchive,
	"application/x-7z-compressed":  ContentArchive,
	"application/gzip":             ContentArchive,
	"application/x-gzip":           ContentArchive,
	"application/x-tar":            ContentArchive,
	"application/x-bzip2":          ContentArchive,
	"application/x-xz":             ContentArchive,

	// Binary
	"application/octet-stream":                ContentBinary,
	"application/x-msdownload":                ContentBinary,
	"application/x-executable":                ContentBinary,
	"application/x-deb":                       ContentBinary,
	"application/x-rpm":                       ContentBinary,
	"application/vnd.android.package-archive": ContentBinary,

	// Document
	"application/pdf": ContentDocument,

	"application/msword": ContentDocument,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ContentDocument,

	"application/vnd.ms-excel": ContentDocument,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": ContentDocument,

	"application/vnd.ms-powerpoint":                                             ContentDocument,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ContentDocument,

	"application/vnd.oasis.opendocument.text":         ContentDocument,
	"application/vnd.oasis.opendocument.spreadsheet":  ContentDocument,
	"application/vnd.oasis.opendocument.presentation": ContentDocument,

	"application/rtf": ContentDocument,

	"application/javascript": ContentDocument,
	"application/json":       ContentDocument,
	"application/xml":        ContentDocument,
	"text/xml":               ContentDocument,

	"application/epub+zip":           ContentDocument,
	"application/x-mobipocket-ebook": ContentDocument,
}

type ContentTypeInfo struct {
	MimeType   string
	Category   ContentCategory
	Extension  string
	ImageBytes []byte
}

func normalizeContentType(ct string) string {
	ct = strings.TrimSpace(ct)
	if ct == "" {
		return ""
	}

	mediaType, _, err := stdmime.ParseMediaType(ct)
	if err == nil {
		return strings.ToLower(mediaType)
	}

	if idx := strings.Index(ct, ";"); idx != -1 {
		ct = ct[:idx]
	}

	return strings.TrimSpace(strings.ToLower(ct))
}

func GetExtensionFromContentType(contentType string) string {
	ct := normalizeContentType(contentType)

	if ext, ok := mimeToExtension[ct]; ok {
		return ext
	}

	return ""
}

func ClassifyContentType(contentType string) ContentCategory {
	ct := normalizeContentType(contentType)
	if ct == "" {
		return ContentUnknown
	}

	if category, ok := exactMimeToCategory[ct]; ok {
		return category
	}

	switch {
	case strings.HasPrefix(ct, "video/"):
		return ContentVideo

	case strings.HasPrefix(ct, "audio/"):
		return ContentAudio

	case strings.HasPrefix(ct, "image/"):
		return ContentImage

	case strings.HasPrefix(ct, "font/"):
		return ContentFont

	case strings.HasPrefix(ct, "text/"):
		return ContentDocument
	}

	if _, ok := mimeToExtension[ct]; ok {
		return ContentDocument
	}

	if strings.Contains(ct, "executable") {
		return ContentBinary
	}

	return ContentUnknown
}

func GetContentTypeInfo(contentType string) ContentTypeInfo {
	ct := normalizeContentType(contentType)

	return ContentTypeInfo{
		MimeType:  ct,
		Category:  ClassifyContentType(ct),
		Extension: GetExtensionFromContentType(ct),
	}
}

func IsSupportedSmartMedia(info ContentTypeInfo) bool {
	switch info.Category {
	case ContentVideo, ContentAudio, ContentImage:
		return true
	default:
		return false
	}
}

// DetectAndClassifyStream membaca awal stream untuk deteksi MIME.
// Tetap zero disk, hanya sample kecil di memory.
func DetectAndClassifyStream(originalReader io.Reader) (ContentTypeInfo, io.Reader, error) {
	const sniffSize = 8192 // 8 KB

	buf := make([]byte, sniffSize)

	n, err := io.ReadFull(originalReader, buf)
	if err != nil &&
		!errors.Is(err, io.ErrUnexpectedEOF) &&
		!errors.Is(err, io.EOF) {
		return ContentTypeInfo{Category: ContentUnknown}, nil, err
	}

	sample := buf[:n]

	fullReader := io.MultiReader(bytes.NewReader(sample), originalReader)

	if n == 0 {
		return ContentTypeInfo{
			MimeType: "",
			Category: ContentUnknown,
		}, fullReader, nil
	}

	detected := mimetype.Detect(sample)

	info := GetContentTypeInfo(detected.String())

	if info.Extension == "" {
		info.Extension = detected.Extension()
	}

	return info, fullReader, nil
}
