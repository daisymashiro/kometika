package facebook

type FacebookUniversalVideoData struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	VidioURL string `json:"video_url"` // High Quality Video URL
	AudioURL string `json:"audio_url"` // Selalu kosong karena API tidak menyediakan audio terpisah
	CoverURL string `json:"cover_url"`
}
