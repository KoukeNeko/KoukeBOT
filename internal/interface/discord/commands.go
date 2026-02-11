package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

var logCmd = logger.New("discord", logger.Magenta)

// SlashCommands manages Discord slash command registration and handling.
type SlashCommands struct {
	bot             *discordbot.Bot
	personaService  *app.PersonaService
	chatService     *app.ChatService
	convStore       store.ConversationRepository
	imageStore      *store.ImageStore
	providerManager *app.ProviderManager
}

func NewSlashCommands(
	bot *discordbot.Bot,
	personaService *app.PersonaService,
	chatService *app.ChatService,
	convStore store.ConversationRepository,
	imageStore *store.ImageStore,
	providerManager *app.ProviderManager,
) *SlashCommands {
	return &SlashCommands{
		bot:             bot,
		personaService:  personaService,
		chatService:     chatService,
		convStore:       convStore,
		imageStore:      imageStore,
		providerManager: providerManager,
	}
}

// Commands returns the slash command definitions.
func (sc *SlashCommands) Commands() []*discordgo.ApplicationCommand {
	// Build persona choices from loaded personas
	choices := sc.buildPersonaChoices()

	return []*discordgo.ApplicationCommand{
		{
			Name:        "persona",
			Description: "切換角色 persona",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "id",
					Description: "角色 ID",
					Required:    true,
					Choices:     choices,
				},
			},
		},
		{
			Name:        "reset",
			Description: "清除目前頻道的對話紀錄",
		},
		{
			Name:        "status",
			Description: "顯示目前的角色與系統狀態",
		},
		{
			Name:        "reload",
			Description: "重新載入所有角色設定檔",
		},
	}
}

// Register registers slash commands and the interaction handler.
func (sc *SlashCommands) Register() error {
	sc.bot.Session().AddHandler(sc.onInteraction)

	if err := sc.bot.RegisterCommands(sc.Commands()); err != nil {
		return fmt.Errorf("register slash commands: %w", err)
	}

	return nil
}

func (sc *SlashCommands) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()

	switch data.Name {
	case "persona":
		sc.handlePersona(s, i, data)
	case "reset":
		sc.handleReset(s, i)
	case "status":
		sc.handleStatus(s, i)
	case "reload":
		sc.handleReload(s, i)
	default:
		respondEphemeral(s, i, "Unknown command.")
	}
}

func (sc *SlashCommands) handlePersona(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	personaID := data.Options[0].StringValue()

	persona, exists := sc.personaService.Get(personaID)
	if !exists {
		respondEphemeral(s, i, fmt.Sprintf("❌ 找不到角色: `%s`", personaID))
		return
	}

	sc.bot.SetPersonaID(personaID)
	respondEphemeral(s, i, fmt.Sprintf("✅ 角色切換為 **%s** (`%s` v%s)",
		persona.Config.Meta.Name, personaID, persona.Config.Meta.Version))
}

func (sc *SlashCommands) handleReset(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID
	personaID := sc.bot.PersonaID()
	ctx := context.Background()

	// Clean up stored images before deleting conversation
	if sc.imageStore != nil {
		if conv, err := sc.convStore.Get(ctx, channelID, personaID); err == nil {
			var keys []string
			for _, msg := range conv.Messages {
				for _, ref := range msg.ImageRefs {
					keys = append(keys, ref.StorageKey)
				}
			}
			if len(keys) > 0 {
				sc.imageStore.DeleteKeys(keys)
				logCmd.Logf("reset [%s]: cleaned up %d image(s)", channelID, len(keys))
			}
		}
	}

	if err := sc.convStore.Delete(ctx, channelID, personaID); err != nil {
		logCmd.Errorf("reset [%s]: %v", channelID, err)
		respondEphemeral(s, i, "❌ 清除失敗: "+err.Error())
		return
	}

	respondEphemeral(s, i, "🗑️ 對話紀錄已清除。")
}

func (sc *SlashCommands) handleStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	persona, exists := sc.personaService.Get(sc.bot.PersonaID())
	if !exists {
		respondEphemeral(s, i, "⚠️ 目前無載入角色。")
		return
	}

	var sb strings.Builder
	sb.WriteString("📊 **System Status**\n\n")
	sb.WriteString(fmt.Sprintf("**角色:** %s (`%s`)\n", persona.Config.Meta.Name, persona.Config.Meta.ID))
	sb.WriteString(fmt.Sprintf("**版本:** %s\n", persona.Config.Meta.Version))
	sb.WriteString(fmt.Sprintf("**Anchors:** %d 組\n", len(persona.Anchors)))
	sb.WriteString(fmt.Sprintf("**Lore:** %d bytes\n", len(persona.Lore)))
	sb.WriteString(fmt.Sprintf("**Temperature:** %.2f\n", persona.Config.Generation.Temperature))
	sb.WriteString(fmt.Sprintf("**Max Tokens:** %d\n", persona.Config.Generation.MaxTokens))

	// Add OAuth pool quota information if available
	if sc.providerManager != nil {
		if pool := sc.providerManager.GetPool(); pool != nil && pool.Size() > 0 {
		sb.WriteString("\n**🔐 OAuth Pool Status**\n")
		entries := pool.Entries()
		for _, entry := range entries {
			maskedEmail := maskEmail(entry.Email)
			status := "✅"
			if entry.Status == "exhausted" {
				status = "⏳"
			}
			if entry.Current {
				status += " [使用中]"
			}

			sb.WriteString(fmt.Sprintf("%s `%s`", status, maskedEmail))
			if entry.RequestCount > 0 {
				sb.WriteString(fmt.Sprintf(" • 24h: %d", entry.RequestCount))
			}
			sb.WriteString("\n")
		}
		}
	}

	respondEphemeral(s, i, sb.String())
}

// maskEmail hides part of the email address for privacy.
// Example: "example@gmail.com" -> "e***e@g***l.com"
func maskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return email // Invalid email, return as-is
	}

	local := parts[0]
	domain := parts[1]

	// Mask local part (e.g., "example" -> "e***e")
	maskedLocal := local
	if len(local) > 2 {
		maskedLocal = string(local[0]) + "***" + string(local[len(local)-1])
	} else if len(local) == 2 {
		maskedLocal = string(local[0]) + "*"
	}

	// Mask domain part (e.g., "gmail.com" -> "g***l.com")
	domainParts := strings.Split(domain, ".")
	if len(domainParts) >= 2 {
		name := domainParts[0]
		maskedName := name
		if len(name) > 2 {
			maskedName = string(name[0]) + "***" + string(name[len(name)-1])
		} else if len(name) == 2 {
			maskedName = string(name[0]) + "*"
		}
		domainParts[0] = maskedName
		domain = strings.Join(domainParts, ".")
	}

	return maskedLocal + "@" + domain
}

func (sc *SlashCommands) handleReload(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if err := sc.personaService.Reload(); err != nil {
		respondEphemeral(s, i, "❌ Reload 失敗: "+err.Error())
		return
	}

	// Refresh slash command options so `/persona` choices reflect newly loaded personas.
	if err := sc.bot.RegisterCommands(sc.Commands()); err != nil {
		respondEphemeral(s, i, "⚠️ 角色已重載，但刷新 Discord 指令失敗: "+err.Error())
		return
	}

	count := len(sc.personaService.GetAll())
	respondEphemeral(s, i, fmt.Sprintf("🔄 已重新載入 %d 個角色，並更新 /persona 選單。", count))
}

func (sc *SlashCommands) buildPersonaChoices() []*discordgo.ApplicationCommandOptionChoice {
	personas := sc.personaService.GetAll()
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(personas))

	for id, p := range personas {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  fmt.Sprintf("%s (%s)", p.Config.Meta.Name, id),
			Value: id,
		})
	}

	return choices
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		logCmd.Errorf("interaction respond: %v", err)
	}
}
