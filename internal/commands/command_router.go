package commands

import (
	"context"
	"fmt"

	"github.com/gotd/td/telegram/message"
	htmlparser "github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"

	"mybot/internal/api"
)

// CommandRouter menangani routing command ke handler yang sesuai
type CommandRouter struct {
	client      *tg.Client
	rootID      int64
	botUsername string
	logger      *zap.Logger
}

// NewCommandRouter membuat instance baru CommandRouter
func NewCommandRouter(client *tg.Client, rootID int64, botUsername string, logger *zap.Logger) *CommandRouter {
	return &CommandRouter{
		client:      client,
		rootID:      rootID,
		botUsername: botUsername,
		logger:      logger,
	}
}

// RouteCommand mengarahkan command ke handler yang sesuai
func (r *CommandRouter) RouteCommand(ctx context.Context, msg *tg.Message, entities tg.Entities, user *tg.User, cmd string, args []string) error {
	switch cmd {
	case "dl":
		return r.handleDL(ctx, msg, entities, args)
	case "gdn":
		return r.handleGDN(ctx, msg, entities)
	case "goroutine":
		return r.handleGoroutine(ctx, msg, entities)
	case "ping":
		return r.handlePing(ctx, msg, entities)
	case "uptime":
		return r.handleUptime(ctx, msg, entities, user)
	case "vnstat":
		return r.handleVnstat(ctx, msg, entities, user)
	case "speedtest":
		return r.handleSpeedtest(ctx, msg, entities, user)
	case "start":
		return r.handleStart(ctx, msg, entities, user)
	case "fiture":
		return r.handleFeatures(ctx, msg, entities)
	case "liststatus":
		return r.handleListStatus(ctx, msg, entities)
	case "on":
		return r.handleOn(ctx, msg, entities, args, user)
	case "off":
		return r.handleOff(ctx, msg, entities, args, user)
	case "getid", "groupinfo":
		return r.handleGetID(ctx, msg, entities, user)
	default:
		return nil
	}
}

func (r *CommandRouter) handleDL(ctx context.Context, msg *tg.Message, entities tg.Entities, args []string) error {
	if len(args) < 1 {
		return nil
	}
	url := args[0]
	return HandleGroupDL(ctx, r.client, msg, entities, url, r.logger)
}

func (r *CommandRouter) handleGDN(ctx context.Context, msg *tg.Message, entities tg.Entities) error {
	return HandleGDNCommand(ctx, r.client, msg, entities, r.logger)
}

func (r *CommandRouter) handleGoroutine(ctx context.Context, msg *tg.Message, entities tg.Entities) error {
	// Hanya di private chat
	if _, isPrivate := msg.PeerID.(*tg.PeerUser); !isPrivate {
		peer, _ := GetPeerFromMessage(ctx, r.client, msg, entities)
		if peer != nil {
			sender := message.NewSender(r.client)
			_, _ = sender.To(peer).Reply(msg.ID).Text(ctx, "❌ Command ini hanya dapat digunakan di Private Chat (DM) dengan bot.")
		}
		return nil
	}
	return HandleGoroutine(ctx, r.client, msg, entities, r.logger)
}

func (r *CommandRouter) handlePing(ctx context.Context, msg *tg.Message, entities tg.Entities) error {
	peer, err := GetPeerFromMessage(ctx, r.client, msg, entities)
	if err != nil {
		return err
	}
	sender := message.NewSender(r.client)
	_, err = sender.To(peer).Reply(msg.ID).Text(ctx, "🏓 Pong!")
	return err
}

func (r *CommandRouter) handleUptime(ctx context.Context, msg *tg.Message, entities tg.Entities, user *tg.User) error {
	// Cek akses root
	if user == nil || user.ID != r.rootID {
		peer, _ := GetPeerFromMessage(ctx, r.client, msg, entities)
		if peer != nil {
			sender := message.NewSender(r.client)
			_, _ = sender.To(peer).Reply(msg.ID).Text(ctx, "  Akses Ditolak.\nHanya Root Admin yang diizinkan menggunakan fitur pemantauan server.")
			return nil
		}
	}

	peer, err := GetPeerFromMessage(ctx, r.client, msg, entities)
	if err != nil {
		return err
	}

	// Ambil informasi sistem
	infoMsg, err := api.GetSystemInfo()
	if err != nil {
		r.logger.Warn("Gagal mengambil system info", zap.Error(err))
		infoMsg = "⚠️ Gagal mengambil informasi sistem."
	}

	sender := message.NewSender(r.client)
	_, err = sender.To(peer).Reply(msg.ID).StyledText(ctx, htmlparser.String(nil, infoMsg))
	if err != nil {
		// Fallback ke plain text jika HTML parsing gagal
		_, err = sender.To(peer).Reply(msg.ID).Text(ctx, infoMsg)
	}
	return err
}

func (r *CommandRouter) handleVnstat(ctx context.Context, msg *tg.Message, entities tg.Entities, user *tg.User) error {
	// Cek akses root
	if user == nil || user.ID != r.rootID {
		peer, _ := GetPeerFromMessage(ctx, r.client, msg, entities)
		if peer != nil {
			sender := message.NewSender(r.client)
			_, _ = sender.To(peer).Reply(msg.ID).Text(ctx, "❌ Akses Ditolak.\nHanya Root Admin yang diizinkan menggunakan fitur pemantauan server.")
		}
		return nil
	}
	return HandleVnstat(ctx, r.client, msg, entities, r.logger)
}

func (r *CommandRouter) handleSpeedtest(ctx context.Context, msg *tg.Message, entities tg.Entities, user *tg.User) error {
	// Cek akses root
	if user == nil || user.ID != r.rootID {
		peer, _ := GetPeerFromMessage(ctx, r.client, msg, entities)
		if peer != nil {
			sender := message.NewSender(r.client)
			_, _ = sender.To(peer).Reply(msg.ID).Text(ctx, "❌ Akses Ditolak.\nHanya Root Admin yang diizinkan menggunakan fitur ini.")
		}
		return nil
	}
	return HandleSpeedtest(ctx, r.client, msg, entities, r.logger)
}

func (r *CommandRouter) handleStart(ctx context.Context, msg *tg.Message, entities tg.Entities, user *tg.User) error {
	return HandleStart(ctx, r.client, msg, entities, user, r.logger)
}

func (r *CommandRouter) handleFeatures(ctx context.Context, msg *tg.Message, entities tg.Entities) error {
	return HandleFeaturesCommand(ctx, r.client, msg, entities, r.logger)
}

func (r *CommandRouter) handleListStatus(ctx context.Context, msg *tg.Message, entities tg.Entities) error {
	return HandleListStatusCommand(ctx, r.client, msg, entities, r.logger)
}

func (r *CommandRouter) handleOn(ctx context.Context, msg *tg.Message, entities tg.Entities, args []string, user *tg.User) error {
	if user == nil || user.ID != r.rootID {
		peer, _ := GetPeerFromMessage(ctx, r.client, msg, entities)
		if peer != nil {
			sender := message.NewSender(r.client)
			_, _ = sender.To(peer).Reply(msg.ID).Text(ctx, "  Hanya owner yang dapat menggunakan command ini.")
		}
		return nil
	}
	return HandleFeatureOnCommand(ctx, r.client, msg, entities, args, r.logger)
}

func (r *CommandRouter) handleOff(ctx context.Context, msg *tg.Message, entities tg.Entities, args []string, user *tg.User) error {
	if user == nil || user.ID != r.rootID {
		peer, _ := GetPeerFromMessage(ctx, r.client, msg, entities)
		if peer != nil {
			sender := message.NewSender(r.client)
			_, _ = sender.To(peer).Reply(msg.ID).Text(ctx, "  Hanya owner yang dapat menggunakan command ini.")
		}
		return nil
	}
	return HandleFeatureOffCommand(ctx, r.client, msg, entities, args, r.logger)
}

func (r *CommandRouter) handleGetID(ctx context.Context, msg *tg.Message, entities tg.Entities, user *tg.User) error {
	// Cek akses root
	if user == nil || user.ID != r.rootID {
		peer, _ := GetPeerFromMessage(ctx, r.client, msg, entities)
		if peer != nil {
			sender := message.NewSender(r.client)
			_, _ = sender.To(peer).Reply(msg.ID).Text(ctx, "❌ Akses Ditolak.\nHanya Root Admin yang diizinkan menggunakan fitur ini.")
		}
		return nil
	}

	peer, err := GetPeerFromMessage(ctx, r.client, msg, entities)
	if err != nil {
		return nil
	}

	var info string
	switch p := msg.PeerID.(type) {
	case *tg.PeerUser:
		userData, ok := entities.Users[p.UserID]
		if ok {
			info = fmt.Sprintf("👤 <b>User Info</b>\n\n"+
				"<b>ID:</b> <code>%d</code>\n"+
				"<b>Username:</b> %s\n"+
				"<b>Nama:</b> %s",
				userData.ID,
				getUsername(userData),
				getDisplayName(userData))
		}
	case *tg.PeerChat:
		info = fmt.Sprintf("💬 <b>Group Chat Info</b>\n\n<b>Chat ID:</b> <code>%d</code>", p.ChatID)
	case *tg.PeerChannel:
		channelData, ok := entities.Channels[p.ChannelID]
		if ok {
			info = fmt.Sprintf("📢 <b>Channel/Supergroup Info</b>\n\n"+
				"<b>ID:</b> <code>%d</code>\n"+
				"<b>Title:</b> %s\n"+
				"<b>Username:</b> %s\n"+
				"<b>Access Hash:</b> <code>%d</code>",
				channelData.ID,
				channelData.Title,
				getChannelUsername(channelData),
				channelData.AccessHash)
		}
	}

	if info != "" {
		sender := message.NewSender(r.client)
		_, err = sender.To(peer).Reply(msg.ID).StyledText(ctx, htmlparser.String(nil, info))
		return err
	}

	return nil
}

func getUsername(user *tg.User) string {
	if user.Username != "" {
		return "@" + user.Username
	}
	return "(tidak ada)"
}

func getDisplayName(user *tg.User) string {
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	return name
}

func getChannelUsername(channel *tg.Channel) string {
	if channel.Username != "" {
		return "@" + channel.Username
	}
	return "(tidak ada)"
}
