package cache

import (
	"sync"
	"time"
)

/*━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  AUDIO CACHE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━*/

type AudioInfo struct {
	AudioURL  string
	Title     string
	MusicName string
	ExpiresAt time.Time
}

var (
	audioStore = sync.Map{}
	defaultTTL = 10 * time.Minute
)

func SetAudio(videoID string, audioURL, title, musicName string) {
	audioStore.Store(videoID, AudioInfo{
		AudioURL:  audioURL,
		Title:     title,
		MusicName: musicName,
		ExpiresAt: time.Now().Add(defaultTTL),
	})
}

func GetAudio(videoID string) (audioURL, title, musicName string, ok bool) {
	val, ok := audioStore.Load(videoID)
	if !ok {
		return "", "", "", false
	}
	info := val.(AudioInfo)
	if time.Now().After(info.ExpiresAt) {
		audioStore.Delete(videoID)
		return "", "", "", false
	}
	return info.AudioURL, info.Title, info.MusicName, true
}

func DeleteAudio(videoID string) {
	audioStore.Delete(videoID)
}

/*━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  CHANNEL CACHE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━*/

type ChannelInfo struct {
	ID         int64
	AccessHash int64
	Title      string
	Username   string
	SavedAt    time.Time
}

var (
	channelStore = sync.Map{}
)

func SaveChannel(id, accessHash int64, title, username string) {
	channelStore.Store(id, ChannelInfo{
		ID:         id,
		AccessHash: accessHash,
		Title:      title,
		Username:   username,
		SavedAt:    time.Now(),
	})
}

func GetChannel(id int64) (*ChannelInfo, bool) {
	val, ok := channelStore.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*ChannelInfo), true
}

func GetChannelAccessHash(id int64) (int64, bool) {
	info, ok := GetChannel(id)
	if !ok {
		return 0, false
	}
	return info.AccessHash, true
}

func DeleteChannel(id int64) {
	channelStore.Delete(id)
}

func ListAllChannels() []*ChannelInfo {
	var channels []*ChannelInfo
	channelStore.Range(func(key, value interface{}) bool {
		channels = append(channels, value.(*ChannelInfo))
		return true
	})
	return channels
}

func ClearOldChannels() int {
	var toDelete []int64
	cutoff := time.Now().Add(-24 * time.Hour)

	channelStore.Range(func(key, value interface{}) bool {
		info := value.(*ChannelInfo)
		if info.SavedAt.Before(cutoff) {
			toDelete = append(toDelete, info.ID)
		}
		return true
	})

	for _, id := range toDelete {
		channelStore.Delete(id)
	}

	return len(toDelete)
}
