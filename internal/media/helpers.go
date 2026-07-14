package media

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"mybot/internal/api"
	"mybot/internal/log"
)

func (m *MediaSender) SendSmartMedia(
	ctx context.Context,
	peer tg.InputPeerClass,
	targetURL string,
	thumbnailURL string,
	caption string,
	replyMarkup tg.ReplyMarkupClass,
	replyTo *tg.InputReplyToMessage,
) (tg.UpdatesClass, error) {
	rawStream, _, err := api.GetVideoStream(ctx, targetURL)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil stream: %w", err)
	}
	defer rawStream.Close()

	info, fullStream, err := api.DetectAndClassifyStream(rawStream)
	if err != nil {
		return nil, fmt.Errorf("gagal mendeteksi tipe file: %w", err)
	}

	if !api.IsSupportedSmartMedia(info) {
		return nil, fmt.Errorf(
			"konten bukan media yang didukung: mime=%s category=%s",
			info.MimeType,
			info.Category,
		)
	}

	// Khusus image:
	// Proses langsung ke bytes dan simpan ke struct info
	// untuk dikirim via m.up.FromBytes() agar ukuran file presisi.
	if info.Category == api.ContentImage {
		photoBytes, err := ProcessAndValidateImageBytes(fullStream, nil)
		if err != nil {
			return nil, fmt.Errorf("gagal memproses image: %w", err)
		}

		// Simpan bytes untuk nanti digunakan di SendDynamicStream
		info.ImageBytes = photoBytes
		info.MimeType = "image/jpeg"
		info.Extension = ".jpg"
		info.Category = api.ContentImage
	}

	if info.MimeType == "" {
		info.MimeType = "application/octet-stream"
	}

	ext := info.Extension
	if ext == "" {
		ext = ".bin"
	}

	filename := "download" + ext

	var thumb tg.InputFileClass

	defaultThumbURL := "https://4kwallpapers.com/images/wallpapers/kawaii-cat-girl-5120x2880-26545.png"

	tryProcessThumb := func(url string) tg.InputFileClass {
		if url == "" {
			return nil
		}

		thumbBytes, err := api.GetThumbnail(ctx, url)
		if err != nil || len(thumbBytes) == 0 {
			return nil
		}

		uploadedThumb, err := m.UploadThumbnail(ctx, thumbBytes)
		if err != nil {
			return nil
		}

		return uploadedThumb
	}

	// Thumbnail custom paling berguna untuk video/audio.
	// Untuk image tidak wajib karena image-nya sendiri sudah menjadi file JPEG.
	if info.Category == api.ContentVideo || info.Category == api.ContentAudio {
		thumb = tryProcessThumb(thumbnailURL)

		if thumb == nil {
			thumb = tryProcessThumb(defaultThumbURL)
		}
	}

	return m.SendDynamicStream(
		ctx,
		peer,
		fullStream,
		info,
		filename,
		caption,
		replyMarkup,
		replyTo,
		thumb,
	)
}

func (m *MediaSender) SendDynamicStream(
	ctx context.Context,
	peer tg.InputPeerClass,
	reader io.Reader,
	info api.ContentTypeInfo,
	filename string,
	caption string,
	replyMarkup tg.ReplyMarkupClass,
	replyTo *tg.InputReplyToMessage,
	thumbFile tg.InputFileClass,
) (tg.UpdatesClass, error) {

	var file tg.InputFileClass
	var err error

	// Fix PHOTO_SAVE_FILE_INVALID:
	// Untuk image, pastikan ukuran file terdeteksi secara pasti via FromBytes
	if info.Category == api.ContentImage && len(info.ImageBytes) > 0 {
		file, err = m.up.FromBytes(ctx, filename, info.ImageBytes)
	} else {
		file, err = m.up.FromReader(ctx, filename, reader)
	}

	if err != nil {
		return nil, fmt.Errorf("upload media gagal: %w", err)
	}

	var media tg.InputMediaClass

	switch info.Category {
	case api.ContentImage:
		// Upload dan daftarkan foto terlebih dahulu
		uploadedMedia, err := m.api.MessagesUploadMedia(ctx, &tg.MessagesUploadMediaRequest{
			Peer: peer,
			Media: &tg.InputMediaUploadedPhoto{
				File: file,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("registrasi foto gagal: %w", err)
		}
		msgMedia, ok := uploadedMedia.(*tg.MessageMediaPhoto)
		if !ok {
			return nil, fmt.Errorf("tipe media tidak valid untuk foto")
		}
		photo, ok := msgMedia.Photo.(*tg.Photo)
		if !ok {
			return nil, fmt.Errorf("tipe foto tidak valid")
		}
		media = &tg.InputMediaPhoto{
			ID: &tg.InputPhoto{
				ID:            photo.ID,
				AccessHash:    photo.AccessHash,
				FileReference: photo.FileReference,
			},
		}
	default:
		// Untuk video, audio, dan lainnya gunakan InputMediaUploadedDocument
		doc := &tg.InputMediaUploadedDocument{
			File:     file,
			MimeType: info.MimeType,
		}

		if thumbFile != nil {
			doc.SetThumb(thumbFile)
		}

		var attrs []tg.DocumentAttributeClass

		switch info.Category {
		case api.ContentVideo:
			videoAttr := &tg.DocumentAttributeVideo{}
			if info.MimeType == "video/mp4" {
				videoAttr.SupportsStreaming = true
			}
			videoAttr.Duration = (48 * time.Hour).Seconds()
			attrs = append(attrs, videoAttr)

		case api.ContentAudio:
			attrs = append(attrs, &tg.DocumentAttributeAudio{})
		}

		attrs = append(attrs, &tg.DocumentAttributeFilename{
			FileName: filename,
		})

		doc.Attributes = attrs
		media = doc
	}

	randID, err := RandomID()
	if err != nil {
		return nil, fmt.Errorf("generate random id: %w", err)
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:        peer,
		Media:       media,
		Message:     caption,
		RandomID:    randID,
		ReplyMarkup: replyMarkup,
	}

	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	for {
		res, err := m.api.MessagesSendMedia(ctx, req)
		if err != nil {
			if d, ok := tgerr.AsFloodWait(err); ok {
				warnMsg := fmt.Sprintf(
					"Bot terkena limit Telegram! Menahan proses selama %v detik.",
					d.Seconds(),
				)

				log.LogWarn(
					ctx,
					"MediaSender.SendDynamicStream",
					warnMsg,
					"File: "+filename,
				)

				select {
				case <-time.After(d + time.Second):
					continue

				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}

			return nil, fmt.Errorf("kirim media gagal: %w", err)
		}

		return res, nil
	}
}

func (m *MediaSender) UploadThumbnail(ctx context.Context, data []byte) (tg.InputFileClass, error) {
	if len(data) == 0 {
		return nil, nil
	}

	return m.up.FromBytes(ctx, "thumb.jpg", data)
}

