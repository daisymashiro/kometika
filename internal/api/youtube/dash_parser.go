package youtube

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// Struktur XML minimalis untuk mengekstrak hanya data yang kita butuhkan dari MPD.
// Ini sangat ringan untuk RAM karena mengabaikan tag XML lain yang tidak relevan.

type MPD struct {
	XMLName xml.Name `xml:"MPD"`
	Periods []Period `xml:"Period"`
}

type Period struct {
	AdaptationSets []AdaptationSet `xml:"AdaptationSet"`
}

type AdaptationSet struct {
	ContentType     string           `xml:"contentType,attr"`
	MimeType        string           `xml:"mimeType,attr"`
	Representations []Representation `xml:"Representation"`
}

type Representation struct {
	Bandwidth uint32   `xml:"bandwidth,attr"`
	BaseURLs  []string `xml:"BaseURL"`
}

// ExtractDashURLs mem-parsing manifest MPD dari byte array menggunakan native encoding/xml
// dan mengembalikan URL video dan audio dengan kualitas/bandwidth tertinggi.
func ExtractDashURLs(manifestBytes []byte) (videoURL, audioURL string, err error) {
	var mpd MPD

	// Parsing raw XML dari bytes
	if err := xml.Unmarshal(manifestBytes, &mpd); err != nil {
		return "", "", fmt.Errorf("gagal parsing MPD XML: %w", err)
	}

	for _, period := range mpd.Periods {
		for _, adaptSet := range period.AdaptationSets {
			contentType := strings.ToLower(adaptSet.ContentType)
			mimeType := strings.ToLower(adaptSet.MimeType)

			var bestURL string
			var maxBW uint32

			// Ambil resolusi/kualitas tertinggi berdasarkan Bandwidth
			for _, rep := range adaptSet.Representations {
				// Gunakan >= untuk menangkap BaseURL meskipun atribut bandwidth = 0
				if rep.Bandwidth >= maxBW {
					maxBW = rep.Bandwidth
					if len(rep.BaseURLs) > 0 && rep.BaseURLs[0] != "" {
						bestURL = rep.BaseURLs[0]
					}
				}
			}

			// Jika tidak ada URL di AdaptationSet ini, lewati
			if bestURL == "" {
				continue
			}

			// Kelompokkan berdasarkan Video atau Audio
			if contentType == "video" || strings.HasPrefix(mimeType, "video/") {
				if videoURL == "" {
					videoURL = bestURL
				}
			} else if contentType == "audio" || strings.HasPrefix(mimeType, "audio/") {
				if audioURL == "" {
					audioURL = bestURL
				}
			}
		}
	}

	if videoURL == "" || audioURL == "" {
		return videoURL, audioURL, fmt.Errorf("gagal mengekstrak URL secara terpisah (punya video: %v, punya audio: %v)", videoURL != "", audioURL != "")
	}

	return videoURL, audioURL, nil
}
