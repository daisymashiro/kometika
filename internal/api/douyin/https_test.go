package douyin

import "testing"

func TestUpgradeHTTPS(t *testing.T) {
	cases := map[string]string{
		"http://sns-v27.rednotecdn.com/stream/1.mp4": "https://sns-v27.rednotecdn.com/stream/1.mp4",
		"https://sns-img-hw.xhscdn.com/cover.jpg":    "https://sns-img-hw.xhscdn.com/cover.jpg",
		"":                                           "",
	}
	for in, want := range cases {
		if got := upgradeHTTPS(in); got != want {
			t.Errorf("upgradeHTTPS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFetchUpgradesToHTTPS(t *testing.T) {
	if got := upgradeHTTPS("http://example.com/x"); got != "https://example.com/x" {
		t.Fatalf("skema tidak diupgrade: %q", got)
	}
}
