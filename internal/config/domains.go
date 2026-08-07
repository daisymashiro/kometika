package config

import "strings"

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
			"freeterabox.com", "teraboxapp.com", "1024terabox.com", "mirrobox.com",
			"nephobox.com", "4funbox.com", "dubox.com", "gibibox.com", "momerybox.com", "tibibox.com",
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
		Name: "douyin",
		Domains: []string{
			"douyin.com",
			"v.douyin.com",
			"iesdouyin.com",
			// Domain yang benar-benar ditangani scraper (RedNote/Xiaohongshu)
			"xiaohongshu.com",
			"xhslink.com",
			"rednote.com",
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

// checkDomainMatch adalah fungsi sentral untuk menghindari false-positive (seperti terabox.com terbaca x.com)
// dan tetap mendeteksi subdomain (seperti www.instagram.com atau m.facebook.com)
func checkDomainMatch(url, domain string) bool {
	lower := strings.ToLower(url)
	// Memaksa pencocokan dengan pembatas yang jelas
	return strings.Contains(lower, "://"+domain) || // cocok: https://x.com
		strings.Contains(lower, "."+domain) || // cocok: https://www.x.com (menolak terabox.com karena butuh titik sebelum x)
		strings.Contains(lower, "/"+domain) || // cocok: domain.com/x.com
		strings.HasPrefix(lower, domain) // cocok: x.com/status/123
}

// IsSupportedURL mengecek apakah URL termasuk dalam salah satu platform yang didukung
func IsSupportedURL(url string) bool {
	for _, p := range platforms {
		for _, d := range p.Domains {
			// Perbaikan: Gunakan checkDomainMatch agar tidak tabrakan
			if checkDomainMatch(url, d) {
				return true
			}
		}
	}
	return false
}

// IsPlatformURL mengecek apakah URL termasuk platform tertentu
func IsPlatformURL(url, platformName string) bool {
	for _, p := range platforms {
		if p.Name == platformName {
			for _, d := range p.Domains {
				// Perbaikan: Gunakan checkDomainMatch
				if checkDomainMatch(url, d) {
					return true
				}
			}
		}
	}
	return false
}

// DetectPlatform mengembalikan nama platform dari URL
func DetectPlatform(url string) string {
	// Sort by domain length (longest first) untuk prioritas yang benar jika diperlukan
	for _, p := range platforms {
		for _, d := range p.Domains {
			// Perbaikan: Gunakan checkDomainMatch
			if checkDomainMatch(url, d) {
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
