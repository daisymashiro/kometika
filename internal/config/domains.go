package config

import "strings"

// PlatformDomain menyimpan daftar domain untuk satu platform
type PlatformDomain struct {
	Name    string   // Nama platform: "tiktok", "instagram", dll.
	Domains []string // Daftar domain/subdomain yang dikenali
}

// Daftar semua platform yang didukung
var platforms = []PlatformDomain{
	{
		Name: "tiktok",
		Domains: []string{
			"tiktok.com",
			"vm.tiktok.com",
		},
	},
	{
		Name: "instagram",
		Domains: []string{
			"instagram.com",
			"instagr.am",
		},
	},
	{
		Name: "facebook",
		Domains: []string{
			"facebook.com",
			"fb.watch",
			"fb.gg",
			"fb.com",
		},
	},
	{
		Name: "lulustream",
		Domains: []string{
			"luluvid.com",
			"lulustream.com",
		},
	},
	{
		Name: "terabox",
		Domains: []string{
			"terabox.app",
			"terabox.com",
			"terabox.link",
			"terabox.fun",
			"1024tera.com",
			"teraboxshare.com",
		},
	},
	{
		Name: "mediafire",
		Domains: []string{
			"mediafire.com",
			"mediafires.co",
			"mediafire.io",
			"mediafire.net",
			"mediafire.org",
		},
	},
	{
		Name: "aceimg",
		Domains: []string{
			"aceimg.com",
		},
	},
	{
		Name: "twitter",
		Domains: []string{
			"twitter.com",
			"x.com",
			"t.co",
		},
	},
	// Tambahkan platform lain di sini
}

// IsSupportedURL mengecek apakah URL termasuk dalam salah satu platform yang didukung
func IsSupportedURL(url string) bool {
	lower := strings.ToLower(url)
	for _, p := range platforms {
		for _, d := range p.Domains {
			if strings.Contains(lower, d) {
				return true
			}
		}
	}
	return false
}

// IsPlatformURL mengecek apakah URL termasuk platform tertentu
func IsPlatformURL(url, platformName string) bool {
	lower := strings.ToLower(url)
	for _, p := range platforms {
		if p.Name == platformName {
			for _, d := range p.Domains {
				// Gunakan boundary check, bukan string.Contains
				// Cari domain sebagai word boundary (preceded by :// atau /)
				if strings.Contains(lower, "://"+d) ||
					strings.Contains(lower, "/"+d) ||
					strings.HasPrefix(lower, d) {
					return true
				}
			}
		}
	}
	return false
}

// DetectPlatform mengembalikan nama platform dari URL
func DetectPlatform(url string) string {
	lower := strings.ToLower(url)
	// Sort by domain length (longest first) untuk prioritas yang benar
	for _, p := range platforms {
		for _, d := range p.Domains {
			if strings.Contains(lower, "://"+d) ||
				strings.Contains(lower, "/"+d) ||
				strings.HasPrefix(lower, d) {
				return p.Name
			}
		}
	}
	return ""
}

// IsTeraboxLink adalah wrapper untuk IsPlatformURL(url, "terabox")
func IsTeraboxLink(url string) bool {
	return IsPlatformURL(url, "terabox")
}

// GetAllPlatforms mengembalikan daftar semua nama platform
func GetAllPlatforms() []string {
	names := make([]string, len(platforms))
	for i, p := range platforms {
		names[i] = p.Name
	}
	return names
}
