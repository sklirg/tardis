package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/sklirg/tardis/hots"
)

type tardis struct {
	AramBuilds *hots.AramBuilds
}

func main() {
	discordBotToken := os.Getenv("TARDIS_DISCORD_TOKEN")

	state := tardis{
		AramBuilds: &hots.AramBuilds{
			SheetID:    os.Getenv("TARDIS_HOTS_ARAM_SHEET_ID"),
			SheetRange: os.Getenv("TARDIS_HOTS_ARAM_SHEET_RANGE"),
		},
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

	fmt.Println("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

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

	switch tokens[0] {
	case "aram", "hots":
		{
			tardis.AramBuilds.HandleDiscordMessage(s, m)
		}
	}
}
