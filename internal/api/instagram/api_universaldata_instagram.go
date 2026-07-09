package instagram

type UniversalInstagramData struct {
	ID        string   `json:"id"`         // ID numerik dari shortcode (CRC32)
	Title     string   `json:"title"`      // caption (jika tersedia, default kosong)
	AudioURL  string   `json:"audio_url"`  // URL audio/mp3 (jika ada)
	VideoURL  string   `json:"video_url"`  // URL video (jika ada)
	IsAlbum   bool     `json:"is_album"`   // true jika multiple image/carousel
	ImageURLs []string `json:"image_urls"` // daftar URL gambar (untuk foto/album)
	CoverURL  string   `json:"cover_url"`  // URL thumbnail/cover
}
