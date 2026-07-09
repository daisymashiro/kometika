package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/gotd/td/tg"
)

type ShortenerResponse struct {
	UrlHost  string `json:"urlhost"`
	UrlShort string `json:"urlshort"`
}

// GetUserAccessHash mengambil AccessHash user dari Telegram
func GetUserAccessHash(ctx context.Context, api *tg.Client, userID int64) (int64, error) {
	users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUser{UserID: userID}})
	if err != nil || len(users) == 0 {
		return 0, fmt.Errorf("gagal mendapatkan user: %v", err)
	}
	user, ok := users[0].(*tg.User)
	if !ok {
		return 0, fmt.Errorf("bukan tipe User")
	}
	return user.AccessHash, nil
}

func ShortenURL(longURL string) string {
	// Encode URL
	apiURL := "https://short.embul.workers.dev/api?url=" + url.QueryEscape(longURL)

	resp, err := http.Get(apiURL)
	if err != nil {
		return longURL
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return longURL
	}

	var data ShortenerResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return longURL
	}

	if data.UrlShort != "" {
		return data.UrlShort
	}

	return longURL
}
