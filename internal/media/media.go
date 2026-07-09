package media

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gotd/td/crypto"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/entity"
	htmlparser "github.com/gotd/td/telegram/message/html"
	//"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// ================= STRUCT =================

type MediaSender struct {
	api *tg.Client
	up  *uploader.Uploader
}

func NewMediaSender(api *tg.Client) *MediaSender {
	up := uploader.NewUploader(api).
		WithThreads(2).
		WithPartSize(512 * 1024)
	return &MediaSender{api: api, up: up}
}

// ================= UTILS =================

func RandomID() (int64, error) {
	return crypto.RandInt64(crypto.DefaultRand())
}

func ExtractMessageID(updates tg.UpdatesClass) (int, error) {
	switch v := updates.(type) {
	case *tg.Updates:
		for _, update := range v.Updates {
			switch u := update.(type) {
			case *tg.UpdateNewMessage:
				return u.Message.GetID(), nil
			case *tg.UpdateNewChannelMessage:
				return u.Message.GetID(), nil
			case *tg.UpdateMessageID:
				return u.ID, nil
			}
		}
	case *tg.UpdateShortSentMessage:
		return v.ID, nil
	}
	return 0, fmt.Errorf("tidak dapat mengekstrak message ID dari response")
}

func GetImageDimensions(data []byte) (w int, h int) {
	if len(data) == 0 {
		return 0, 0
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return config.Width, config.Height
}

// ================= TEXT =================

func (m *MediaSender) SendTextMessage(ctx context.Context, peer tg.InputPeerClass, text string) error {
	sender := message.NewSender(m.api)
	_, err := sender.To(peer).Text(ctx, text)
	return err
}

func (m *MediaSender) SendHTML(ctx context.Context, peer tg.InputPeerClass, htmlText string) error {
	sender := message.NewSender(m.api)
	_, err := sender.To(peer).StyledText(ctx, htmlparser.String(nil, htmlText))
	return err
}

func (m *MediaSender) EditHTML(ctx context.Context, peer tg.InputPeerClass, msgID int, htmlText string) error {
	sender := message.NewSender(m.api)
	_, err := sender.To(peer).Edit(msgID).StyledText(ctx, htmlparser.String(nil, htmlText))
	return err
}

func (m *MediaSender) EditWithMarkup(ctx context.Context, peer tg.InputPeerClass, msgID int, plainText string, markup tg.ReplyMarkupClass) error {
	_, err := m.api.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
		Peer:        peer,
		ID:          msgID,
		Message:     plainText,
		ReplyMarkup: markup,
	})
	return err
}

func (m *MediaSender) AnswerCallback(ctx context.Context, queryID int64, text string, alert bool) {
	req := &tg.MessagesSetBotCallbackAnswerRequest{
		QueryID:   queryID,
		CacheTime: 0,
	}
	if text != "" {
		req.SetMessage(text)
		req.Alert = alert
	}
	_, _ = m.api.MessagesSetBotCallbackAnswer(ctx, req)
}

// ================= VIDEO =================

func (m *MediaSender) SendVideoStream(ctx context.Context, peer tg.InputPeerClass,
	reader io.Reader, filename, caption string,
	replyMarkup tg.ReplyMarkupClass, thumbFile tg.InputFileClass,
	w int, h int,
	replyTo *tg.InputReplyToMessage) (tg.UpdatesClass, error) {

	file, err := m.up.FromReader(ctx, filename, reader)
	if err != nil {
		return nil, fmt.Errorf("upload video gagal: %w", err)
	}

	media := &tg.InputMediaUploadedDocument{
		File:     file,
		MimeType: "video/mp4",
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeVideo{SupportsStreaming: true,
				W: w,
				H: h,
			},
			&tg.DocumentAttributeFilename{FileName: filename},
		},
	}
	if thumbFile != nil {
		media.SetThumb(thumbFile)
	}

	randID, err := RandomID()
	if err != nil {
		return nil, fmt.Errorf("generate random id: %w", err)
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    media,
		Message:  caption,
		RandomID: randID,
	}
	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}
	if replyMarkup != nil {
		req.SetReplyMarkup(replyMarkup)
	}

	return m.api.MessagesSendMedia(ctx, req)
}

// ================= AUDIO =================

func (m *MediaSender) SendAudioStream(ctx context.Context, peer tg.InputPeerClass,
	reader io.Reader, filename, title, performer, caption string,
	replyMarkup tg.ReplyMarkupClass, replyTo *tg.InputReplyToMessage) error {

	file, err := m.up.FromReader(ctx, filename, reader)
	if err != nil {
		return fmt.Errorf("upload audio gagal: %w", err)
	}

	audioAttr := &tg.DocumentAttributeAudio{Duration: 0}
	if title != "" {
		audioAttr.SetTitle(title)
	}
	if performer != "" {
		audioAttr.SetPerformer(performer)
	}

	media := &tg.InputMediaUploadedDocument{
		File:     file,
		MimeType: "audio/mpeg",
		Attributes: []tg.DocumentAttributeClass{
			audioAttr,
			&tg.DocumentAttributeFilename{FileName: filename},
		},
	}

	randID, err := RandomID()
	if err != nil {
		return fmt.Errorf("generate random id: %w", err)
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    media,
		Message:  caption,
		RandomID: randID,
	}
	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}
	if replyMarkup != nil {
		req.SetReplyMarkup(replyMarkup)
	}

	_, err = m.api.MessagesSendMedia(ctx, req)
	return err
}

// ================= VOICE =================

func (m *MediaSender) SendVoiceStream(ctx context.Context, peer tg.InputPeerClass,
	reader io.Reader, filename string,
	replyMarkup tg.ReplyMarkupClass, replyTo *tg.InputReplyToMessage) error {

	file, err := m.up.FromReader(ctx, filename, reader)
	if err != nil {
		return fmt.Errorf("upload voice gagal: %w", err)
	}

	media := &tg.InputMediaUploadedDocument{
		File:     file,
		MimeType: "audio/ogg",
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeAudio{Voice: true},
			&tg.DocumentAttributeFilename{FileName: filename},
		},
	}

	randID, err := RandomID()
	if err != nil {
		return fmt.Errorf("generate random id: %w", err)
	}

	req := &tg.MessagesSendMediaRequest{
		Peer:        peer,
		Media:       media,
		ReplyMarkup: replyMarkup,
		RandomID:    randID,
	}
	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	_, err = m.api.MessagesSendMedia(ctx, req)
	return err
}

// ================= PHOTO =================

func (m *MediaSender) SendPhotoStream(ctx context.Context, peer tg.InputPeerClass,
	reader io.Reader, filename, caption string,
	replyMarkup tg.ReplyMarkupClass, replyTo *tg.InputReplyToMessage) error {

	// 1. Upload file via MessagesUploadMedia
	file, err := m.up.FromReader(ctx, filename, reader)
	if err != nil {
		return fmt.Errorf("upload foto gagal: %w", err)
	}

	// Daftarkan media
	uploadedMedia, err := m.api.MessagesUploadMedia(ctx, &tg.MessagesUploadMediaRequest{
		Peer: peer,
		Media: &tg.InputMediaUploadedPhoto{
			File: file,
		},
	})
	if err != nil {
		return fmt.Errorf("registrasi foto gagal: %w", err)
	}

	// Konversi ke Photo
	msgMedia, ok := uploadedMedia.(*tg.MessageMediaPhoto)
	if !ok {
		return fmt.Errorf("tipe media tidak valid: %T", uploadedMedia)
	}
	photo, ok := msgMedia.Photo.(*tg.Photo)
	if !ok {
		return fmt.Errorf("tipe foto tidak valid: %T", msgMedia.Photo)
	}

	// 2. Kirim dengan InputMediaPhoto
	randID, err := RandomID()
	if err != nil {
		return fmt.Errorf("generate random id gagal: %w", err)
	}

	req := &tg.MessagesSendMediaRequest{
		Peer: peer,
		Media: &tg.InputMediaPhoto{
			ID: &tg.InputPhoto{
				ID:            photo.ID,
				AccessHash:    photo.AccessHash,
				FileReference: photo.FileReference,
			},
		},
		Message:  caption,
		RandomID: randID,
	}
	if replyMarkup != nil {
		req.SetReplyMarkup(replyMarkup)
	}
	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	_, err = m.api.MessagesSendMedia(ctx, req)
	return err
}

// ================= ALBUM =================

// func (m *MediaSender) SendPhotoAlbumStream(ctx context.Context, peer tg.InputPeerClass,
// 	readers []io.Reader, filenames []string, captions []string,
// 	replyTo *tg.InputReplyToMessage) error {
//
// 	sender := message.NewSender(m.api)
// 	builder := sender.To(peer)
//
// 	mediaOptions := make([]message.MultiMediaOption, 0, len(readers))
// 	for i, reader := range readers {
// 		data, err := io.ReadAll(reader)
// 		if err != nil {
// 			return fmt.Errorf("baca data foto ke-%d gagal: %w", i, err)
// 		}
// 		file, err := m.up.FromBytes(ctx, filenames[i], data)
// 		if err != nil {
// 			return fmt.Errorf("upload foto ke-%d gagal: %w", i, err)
// 		}
// 		caption := ""
// 		if i < len(captions) {
// 			caption = captions[i]
// 		}
// 		if caption != "" {
// 			mediaOptions = append(mediaOptions, message.UploadedPhoto(file, styling.Plain(caption)))
// 		} else {
// 			mediaOptions = append(mediaOptions, message.UploadedPhoto(file))
// 		}
// 	}
//
// 	if len(mediaOptions) == 0 {
// 		return fmt.Errorf("tidak ada foto yang berhasil diupload")
// 	}
//
// 	for {
// 		_, err := builder.Album(ctx, mediaOptions[0], mediaOptions[1:]...)
// 		if d, ok := tgerr.AsFloodWait(err); ok {
// 			select {
// 			case <-time.After(d + time.Second):
// 				continue
// 			case <-ctx.Done():
// 				return ctx.Err()
// 			}
// 		}
// 		if err != nil {
// 			return fmt.Errorf("kirim album foto gagal: %w", err)
// 		}
// 		return nil
// 	}
// }

// ================= ALBUM =================

func (m *MediaSender) SendPhotoAlbumStream(ctx context.Context, peer tg.InputPeerClass,
	readers []io.Reader, filenames []string, captions []string,
	replyTo *tg.InputReplyToMessage) error {

	multiMedia := make([]tg.InputSingleMedia, 0, len(readers))

	for i, reader := range readers {
		// Step 1: Upload file mentah ke server Telegram
		file, err := m.up.Upload(ctx, uploader.NewUpload(filenames[i], reader, 0))
		if err != nil {
			return fmt.Errorf("upload foto ke-%d gagal: %w", i, err)
		}

		// Step 2: WAJIB - Daftarkan media dengan perlindungan FLOOD_WAIT
		var uploadedMedia tg.MessageMediaClass
		for {
			result, err := m.api.MessagesUploadMedia(ctx, &tg.MessagesUploadMediaRequest{
				Peer:  peer,
				Media: &tg.InputMediaUploadedPhoto{File: file},
			})
			if d, ok := tgerr.AsFloodWait(err); ok {
				select {
				case <-time.After(d + time.Second):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if err != nil {
				return fmt.Errorf("registrasi media ke-%d gagal: %w", i, err)
			}
			uploadedMedia = result
			break
		}

		// Step 3: Konversi hasil upload menjadi tg.Photo
		msgMedia, ok := uploadedMedia.(*tg.MessageMediaPhoto)
		if !ok {
			return fmt.Errorf("foto ke-%d: tipe media tidak valid %T", i, uploadedMedia)
		}
		photo, ok := msgMedia.Photo.(*tg.Photo)
		if !ok {
			return fmt.Errorf("foto ke-%d: tipe foto tidak valid %T", i, msgMedia.Photo)
		}

		// Siapkan caption (biasanya di gambar pertama)
		caption := ""
		if i < len(captions) {
			caption = captions[i]
		}

		// Generate Random ID (Wajib)
		randID, err := RandomID()
		if err != nil {
			return fmt.Errorf("generate random id gagal: %w", err)
		}

		// Step 4: Susun Single Media menggunakan struct value
		singleMedia := tg.InputSingleMedia{
			Media: &tg.InputMediaPhoto{
				ID: &tg.InputPhoto{
					ID:            photo.ID,
					AccessHash:    photo.AccessHash,
					FileReference: photo.FileReference,
				},
			},
			RandomID: randID,
			Message:  caption,
		}
		multiMedia = append(multiMedia, singleMedia)
	}

	if len(multiMedia) == 0 {
		return fmt.Errorf("tidak ada foto yang berhasil diupload")
	}

	// Step 5: Kirim request MultiMedia dengan ReplyTo (Mendukung Forum Topic)
	req := &tg.MessagesSendMultiMediaRequest{
		Peer:       peer,
		MultiMedia: multiMedia,
	}

	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	// Step 6: Eksekusi pengiriman final dengan perlindungan FLOOD_WAIT
	for {
		_, err := m.api.MessagesSendMultiMedia(ctx, req)
		if d, ok := tgerr.AsFloodWait(err); ok {
			select {
			case <-time.After(d + time.Second):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err != nil {
			return fmt.Errorf("kirim album foto gagal: %w", err)
		}
		return nil
	}
}

// ================= DOCUMENT =================

func (m *MediaSender) SendDocumentStream(ctx context.Context, peer tg.InputPeerClass,
	reader io.Reader, filename, caption string,
	replyMarkup tg.ReplyMarkupClass, replyTo *tg.InputReplyToMessage) error {

	file, err := m.up.FromReader(ctx, filename, reader)
	if err != nil {
		return fmt.Errorf("upload dokumen gagal: %w", err)
	}

	randID, err := RandomID()
	if err != nil {
		return fmt.Errorf("generate random id: %w", err)
	}

	req := &tg.MessagesSendMediaRequest{
		Peer: peer,
		Media: &tg.InputMediaUploadedDocument{
			File:      file,
			ForceFile: true,
			MimeType:  "application/octet-stream",
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{FileName: filename},
			},
		},
		Message:  caption,
		RandomID: randID,
	}
	if replyMarkup != nil {
		req.SetReplyMarkup(replyMarkup)
	}
	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	_, err = m.api.MessagesSendMedia(ctx, req)
	return err
}

func (m *MediaSender) SendDocumentStreamHTML(ctx context.Context, peer tg.InputPeerClass,
	reader io.Reader, filename, htmlCaption string,
	replyMarkup tg.ReplyMarkupClass, replyTo *tg.InputReplyToMessage) error {

	file, err := m.up.FromReader(ctx, filename, reader)
	if err != nil {
		return fmt.Errorf("upload dokumen gagal: %w", err)
	}

	var eb entity.Builder
	if err := htmlparser.HTML(strings.NewReader(htmlCaption), &eb, htmlparser.Options{}); err != nil {
		return fmt.Errorf("parse html caption gagal: %w", err)
	}
	captionText, entities := eb.Complete()

	randID, err := RandomID()
	if err != nil {
		return fmt.Errorf("generate random id: %w", err)
	}

	req := &tg.MessagesSendMediaRequest{
		Peer: peer,
		Media: &tg.InputMediaUploadedDocument{
			File:      file,
			ForceFile: true,
			MimeType:  "application/octet-stream",
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{FileName: filename},
			},
		},
		Message:  captionText,
		RandomID: randID,
	}
	if len(entities) > 0 {
		req.SetEntities(entities)
	}
	if replyMarkup != nil {
		req.SetReplyMarkup(replyMarkup)
	}
	if replyTo != nil {
		req.SetReplyTo(replyTo)
	}

	_, err = m.api.MessagesSendMedia(ctx, req)
	return err
}

func SendHTML(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, htmlText string) error {
	sender := message.NewSender(client)
	_, err := sender.To(peer).StyledText(ctx, htmlparser.String(nil, htmlText))
	return err
}

func EditHTML(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, htmlText string) error {
	sender := message.NewSender(client)
	_, err := sender.To(peer).Edit(msgID).StyledText(ctx, htmlparser.String(nil, htmlText))
	return err
}

func EditWithMarkup(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, msgID int, plainText string, markup tg.ReplyMarkupClass) error {
	_, err := client.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
		Peer:        peer,
		ID:          msgID,
		Message:     plainText,
		ReplyMarkup: markup,
	})
	return err
}

func AnswerCallback(ctx context.Context, client *tg.Client, queryID int64, text string, alert bool) {
	req := &tg.MessagesSetBotCallbackAnswerRequest{
		QueryID:   queryID,
		CacheTime: 0,
	}
	if text != "" {
		req.SetMessage(text)
		req.Alert = alert
	}
	_, _ = client.MessagesSetBotCallbackAnswer(ctx, req)
}

func SendTextMessage(ctx context.Context, client *tg.Client, peer tg.InputPeerClass, text string) error {
	sender := message.NewSender(client)
	_, err := sender.To(peer).Text(ctx, text)
	return err
}
