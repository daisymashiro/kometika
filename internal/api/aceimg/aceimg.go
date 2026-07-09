package aceimg

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

type MediaResult struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

func Extract(rawURL string) (*MediaResult, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("gagal parse URL: %w", err)
	}

	f := parsed.Query().Get("f")
	if f == "" {
		return nil, fmt.Errorf("parameter 'f' tidak ditemukan")
	}

	ext := strings.TrimPrefix(filepath.Ext(f), ".")
	id := strings.TrimSuffix(f, filepath.Ext(f))

	return &MediaResult{
		ID:   id,
		URL:  "https://cdn.aceimg.com/" + f,
		Type: ext,
	}, nil
}
