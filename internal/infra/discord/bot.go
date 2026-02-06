package discord

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

// Bot wraps a discordgo.Session with lifecycle management.
type Bot struct {
	session   *discordgo.Session
	personaID string // which persona this bot instance serves
}

// NewBot creates a Discord bot session (does not open the connection yet).
func NewBot(token, personaID string) (*Bot, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	// We only need message events
	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent

	return &Bot{
		session:   session,
		personaID: personaID,
	}, nil
}

// Session returns the underlying discordgo session for handler registration.
func (b *Bot) Session() *discordgo.Session {
	return b.session
}

// PersonaID returns the persona this bot serves.
func (b *Bot) PersonaID() string {
	return b.personaID
}

// Start opens the websocket connection to Discord.
func (b *Bot) Start() error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open discord connection: %w", err)
	}
	log.Printf("discord bot connected as %s#%s", b.session.State.User.Username, b.session.State.User.Discriminator)
	return nil
}

// Stop gracefully closes the Discord connection.
func (b *Bot) Stop() error {
	log.Println("closing discord connection...")
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
