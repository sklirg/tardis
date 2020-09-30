package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
	"github.com/sklirg/tardis/hots"
	"github.com/sklirg/tardis/server"
)

type tardis struct {
	AramBuilds       *hots.AramBuilds
	ServerManager    server.DiscordServerStore
	DevMode          bool
	DevListenChannel string
}

func main() {
	discordBotToken := os.Getenv("TARDIS_DISCORD_TOKEN")

	state := tardis{
		DevMode: os.Getenv("TARDIS_DEV") != "",
		AramBuilds: &hots.AramBuilds{
			SheetID:    os.Getenv("TARDIS_HOTS_ARAM_SHEET_ID"),
			SheetRange: os.Getenv("TARDIS_HOTS_ARAM_SHEET_RANGE"),
		},
		ServerManager: server.DiscordServerStore{},
	}

	if state.DevMode {
		log.Info("Starting in dev mode")
		log.SetLevel(log.TraceLevel)
	}

	// Set StateEnabled ?

	dg, err := discordConnect(discordBotToken)
	if err != nil {
		fmt.Println("error connecting", err)
		return
	}

	dg.AddHandler(state.messageCreate)
	dg.AddHandler(state.handleReactionAdd)
	dg.AddHandler(state.handleReactionRemove)

	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	log.Info("Bot is now running. Press CTRL-C to exit.")

	log.WithField("state", *dg.State).Debug("State")

	for _, guild := range dg.State.Ready.Guilds {
		// Fetch guild info from database if we have it
		log.WithField("guild_id", guild.ID).Debugf("Connected to '%s'", guild.Name)
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	log.Info("Received interrupt, shutting down.")

	dg.Close()
}

func discordConnect(token string) (*discordgo.Session, error) {
	client, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Println("Error setting up session: ", err)
		return nil, err
	}

	return client, nil
}

func (tardis *tardis) messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.SessionID {
		return
	}
	logger := log.WithFields(log.Fields{
		"author_id": m.Author.ID,
		"author":    m.Author.String(),
	})

	if len(m.Content) == 0 {
		return
	}

	if m.Content[0] != '!' {
		return
	}

	tokens := strings.Split(m.Content[1:], " ")

	if len(tokens) == 0 {
		return
	}

	trigger := tokens[0]

	if tardis.DevMode && !(trigger == "listen" || m.ChannelID == tardis.DevListenChannel) {
		// In DevMode and received message in a channel I don't listen to, so skip
		// But will allow the keyword 'listen' through
		return
	}
	logger = logger.WithField("trigger", trigger)

	switch trigger {
	case "aram", "hots":
		{
			tardis.AramBuilds.HandleDiscordMessage(s, m)
		}
	case "listen":
		{
			if !tardis.DevMode {
				// If we receive the `listen` trigger while not in DevMode we don't care
				return
			}
			tardis.DevListenChannel = m.ChannelID
			s.ChannelMessageSend(m.ChannelID, ":robot: :construction: Listening to this channel")
		}
	case "reactrole":
		go tardis.ServerManager.HandleDiscordMessage(s, m)
	default:
		{
			if tardis.DevMode {
				logger.Debug("Received unknown trigger")
			}
		}
	}
}

func (t *tardis) handleReactionAdd(s *discordgo.Session, reaction *discordgo.MessageReactionAdd) {
	if roles, err := t.ServerManager.GetReactRolesForMessage(server.ReactRoleMessage{GuildID: reaction.GuildID, ChannelID: reaction.ChannelID, ID: reaction.MessageID}); err == nil && roles != nil {
		log.Debug("Adding roles to user")
		for _, rr := range roles {
			if reaction.Emoji.APIName() != rr.Emoji {
				continue
			}
			if err := s.GuildMemberRoleAdd(reaction.GuildID, reaction.UserID, rr.Role); err != nil {
				log.WithError(err).WithField("rr.Role_id", rr.Role).WithField("user_id", reaction.UserID).Error("Failed to add rr.Role to user")
			}
		}
	}
}

func (t *tardis) handleReactionRemove(s *discordgo.Session, reaction *discordgo.MessageReactionRemove) {
	if roles, err := t.ServerManager.GetReactRolesForMessage(server.ReactRoleMessage{GuildID: reaction.GuildID, ChannelID: reaction.ChannelID, ID: reaction.MessageID}); err == nil && roles != nil {
		log.Debug("Removing roles to user")
		for _, rr := range roles {
			if reaction.Emoji.APIName() != rr.Emoji {
				continue
			}
			if err := s.GuildMemberRoleRemove(reaction.GuildID, reaction.UserID, rr.Role); err != nil {
				log.WithError(err).WithField("role_id", rr.Role).WithField("user", reaction.UserID).Error("Failed to remove role from user")
			}
		}
	}
}

func handleHelp(s *discordgo.Session, m *discordgo.MessageCreate) {
	fieldTexts := [][]string{
		[]string{"hots", "aliases: aram | find build guides for HotS ARAM matches"},
	}

	fields := make([]*discordgo.MessageEmbedField, 0)

	for _, data := range fieldTexts {
		field := discordgo.MessageEmbedField{
			Name:  data[0],
			Value: data[1],
		}
		fields = append(fields, &field)
	}

	msg := discordgo.MessageEmbed{
		URL:         "https://github.com/sklirg/tardis",
		Title:       "TARDIS",
		Description: "For feature requests and help, click the title (link).",
		Fields:      fields,
	}

	_, err := s.ChannelMessageSendEmbed(m.ChannelID, &msg)
	if err != nil {
		fmt.Println("Failed to send help message :(")
	}
}
