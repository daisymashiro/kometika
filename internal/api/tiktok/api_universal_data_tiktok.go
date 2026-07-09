package tiktok

// UniversalTikTokData adalah struktur data standar untuk semua API
// type UniversalTikTokData struct {
// 	ID        string // Video ID
// 	Title     string // Judul video
// 	VideoURL  string // URL video tanpa watermark
// 	AudioURL  string // URL audio (opsional)
// 	MusicName string // Nama musik/audio
// 	Duration  int    // Durasi dalam detik (jika tersedia)
// 	IsAlbum   bool   // true jika konten berupa foto
// 	ImageURLs []string
// }

type UniversalTikTokData struct {
	ID        string   // Video ID (atau ID konten)
	Title     string   // Judul konten
	VideoURL  string   // URL video tanpa watermark (khusus video)
	AudioURL  string   // URL audio (opsional)
	IsAlbum   bool     // true jika konten berupa foto (album)
	ImageURLs []string // daftar URL gambar (jika IsAlbum true)
	CoverURL  string   // thumbnail / cover (untuk video maupun album)
}
