package discord

import (
	"fmt"

	"github.com/KoukeNeko/KoukeBOT/internal/infra/logger"
	"github.com/bwmarrin/discordgo"
)

var logBot = logger.New("discord", logger.Magenta)

// Bot wraps a discordgo.Session with lifecycle management.
type Bot struct {
	session             *discordgo.Session
	personaID           string   // active persona for this bot instance
	commandIDs          []string // registered slash command IDs (for cleanup)
	memberIntentEnabled bool
	presenceIntent      bool
}

type NewBotOptions struct {
	EnableMemberIntent   bool
	EnablePresenceIntent bool
}

// NewBot creates a Discord bot session (does not open the connection yet).
func NewBot(token, personaID string, options NewBotOptions) (*Bot, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	// We only need message events
	intents := discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent
	if options.EnableMemberIntent {
		intents |= discordgo.IntentsGuildMembers
	}
	if options.EnablePresenceIntent {
		intents |= discordgo.IntentsGuildPresences
	}
	session.Identify.Intents = intents

	return &Bot{
		session:             session,
		personaID:           personaID,
		memberIntentEnabled: options.EnableMemberIntent,
		presenceIntent:      options.EnablePresenceIntent,
	}, nil
}

// Session returns the underlying discordgo session for handler registration.
func (b *Bot) Session() *discordgo.Session {
	return b.session
}

// PersonaID returns the active persona.
func (b *Bot) PersonaID() string {
	return b.personaID
}

// SetPersonaID switches the active persona.
func (b *Bot) SetPersonaID(id string) {
	b.personaID = id
}

func (b *Bot) MemberIntentEnabled() bool {
	return b.memberIntentEnabled
}

func (b *Bot) PresenceIntentEnabled() bool {
	return b.presenceIntent
}

// RegisterCommands registers slash commands globally.
func (b *Bot) RegisterCommands(commands []*discordgo.ApplicationCommand) error {
	appID := b.session.State.User.ID

	for _, cmd := range commands {
		registered, err := b.session.ApplicationCommandCreate(appID, "", cmd)
		if err != nil {
			return fmt.Errorf("register command %q: %w", cmd.Name, err)
		}
		b.commandIDs = append(b.commandIDs, registered.ID)
		logBot.Logf("slash command: /%s", cmd.Name)
	}

	return nil
}

// UnregisterCommands removes all registered slash commands.
func (b *Bot) UnregisterCommands() {
	appID := b.session.State.User.ID
	for _, cmdID := range b.commandIDs {
		if err := b.session.ApplicationCommandDelete(appID, "", cmdID); err != nil {
			logBot.Warnf("failed to unregister command %s: %v", cmdID, err)
		}
	}
}

// Start opens the websocket connection to Discord.
func (b *Bot) Start() error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open discord connection: %w", err)
	}
	logBot.Logf("connected as %s#%s", b.session.State.User.Username, b.session.State.User.Discriminator)
	return nil
}

// Stop gracefully closes the Discord connection.
func (b *Bot) Stop() error {
	logBot.Log("closing connection...")
	return b.session.Close()
}

// SendMessage sends a text message to a channel.
func (b *Bot) SendMessage(channelID, content string) error {
	_, err := b.session.ChannelMessageSend(channelID, content)
	return err
}

// SendTyping triggers the "bot is typing..." indicator.
func (b *Bot) SendTyping(channelID string) error {
	return b.session.ChannelTyping(channelID)
}

// SendMessageWithReturn sends a message and returns the Message object.
func (b *Bot) SendMessageWithReturn(channelID, content string) (*discordgo.Message, error) {
	return b.session.ChannelMessageSend(channelID, content)
}

// SendMessageReply sends a message as a reply to another message.
func (b *Bot) SendMessageReply(channelID, content, replyToMessageID string) (*discordgo.Message, error) {
	return b.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: content,
		Reference: &discordgo.MessageReference{
			MessageID: replyToMessageID,
			ChannelID: channelID,
		},
	})
}

// EditMessage edits an existing message.
func (b *Bot) EditMessage(channelID, messageID, newContent string) error {
	_, err := b.session.ChannelMessageEdit(channelID, messageID, newContent)
	return err
}
