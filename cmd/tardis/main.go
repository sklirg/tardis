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
)

type tardis struct {
	AramBuilds       *hots.AramBuilds
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
	}

	if state.DevMode {
		log.Info("Starting in dev mode")
		log.SetLevel(log.TraceLevel)
	}

	dg, err := discordConnect(discordBotToken)
	if err != nil {
		fmt.Println("error connecting", err)
		return
	}

	dg.AddHandler(state.messageCreate)

	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	log.Info("Bot is now running. Press CTRL-C to exit.")
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
	default:
		{
			if tardis.DevMode {
				logger.Debug("Received unknown trigger")
			}
		}
	}
}
