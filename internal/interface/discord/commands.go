package discord

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/KoukeNeko/KoukeBOT/internal/app"
	discordbot "github.com/KoukeNeko/KoukeBOT/internal/infra/discord"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/store"
)

// SlashCommands manages Discord slash command registration and handling.
type SlashCommands struct {
	bot            *discordbot.Bot
	personaService *app.PersonaService
	chatService    *app.ChatService
	convStore      store.ConversationRepository
}

func NewSlashCommands(
	bot *discordbot.Bot,
	personaService *app.PersonaService,
	chatService *app.ChatService,
	convStore store.ConversationRepository,
) *SlashCommands {
	return &SlashCommands{
		bot:            bot,
		personaService: personaService,
		chatService:    chatService,
		convStore:      convStore,
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

	if err := sc.convStore.Delete(context.Background(), channelID, personaID); err != nil {
		log.Printf("reset error [%s]: %v", channelID, err)
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
	sb.WriteString("📊 **System Status**\n")
	sb.WriteString(fmt.Sprintf("**角色:** %s (`%s`)\n", persona.Config.Meta.Name, persona.Config.Meta.ID))
	sb.WriteString(fmt.Sprintf("**版本:** %s\n", persona.Config.Meta.Version))
	sb.WriteString(fmt.Sprintf("**Anchors:** %d 組\n", len(persona.Anchors)))
	sb.WriteString(fmt.Sprintf("**Lore:** %d bytes\n", len(persona.Lore)))
	sb.WriteString(fmt.Sprintf("**Temperature:** %.2f\n", persona.Config.Generation.Temperature))
	sb.WriteString(fmt.Sprintf("**Max Tokens:** %d\n", persona.Config.Generation.MaxTokens))

	respondEphemeral(s, i, sb.String())
}

func (sc *SlashCommands) handleReload(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if err := sc.personaService.Reload(); err != nil {
		respondEphemeral(s, i, "❌ Reload 失敗: "+err.Error())
		return
	}

	count := len(sc.personaService.GetAll())
	respondEphemeral(s, i, fmt.Sprintf("🔄 已重新載入 %d 個角色。", count))
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
		log.Printf("interaction respond error: %v", err)
	}
}
