package hots

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sklirg/tardis/datasources"
)

// AramBuild is an ARAM build for a specific hero.
type AramBuild struct {
	Hero      string
	Abilities []string
}

// AramBuilds is a collection of ARAM builds.
// The wrapper exists to set a LastSync attribute to avoid
// fetching new buils all the time.
type AramBuilds struct {
	once        sync.Once
	SheetID     string
	SheetRange  string
	LastSync    time.Time
	Builds      *map[string]*AramBuild
	HeroAliases map[string]string
}

func readHeroAliasesMap() map[string]string {
	data, err := ioutil.ReadFile("./heroaliases.json")

	if err != nil {
		fmt.Println("Failed to read heroes alias map. Skipping.")
		return make(map[string]string)
	}

	var aliases map[string]string
	err = json.Unmarshal(data, &aliases)
	if err != nil {
		fmt.Println("Failed to unmarshal heroes map to json")
		return make(map[string]string)
	}

	return aliases
}

func writeHeroAliasesMap(updated map[string]string, update bool) error {
	var existing map[string]string
	if update {
		existing = readHeroAliasesMap()
	} else {
		existing = updated
	}

	for k, v := range updated {
		existing[k] = v
	}

	data, err := json.Marshal(&existing)

	if err != nil {
		fmt.Println("Failed to marshal heroes aliases data to json")
		return err
	}

	s, err := os.Stat("./heroaliases.json")

	if err != nil {
		fmt.Println("Failed to stat heroaliases file", err)
		return err
	}

	ioutil.WriteFile("./heroaliases.json", []byte(data), s.Mode())

	return nil
}

func fetchAramBuilds(sheetID, sheetRange string) map[string]*AramBuild {
	build := datasources.GoogleDriveSheets{}

	data := build.FetchSheetData(sheetID, sheetRange)

	builds := make(map[string]*AramBuild)

	for _, r := range data {
		b := AramBuild{}
		if len(r) == 0 {
			continue
		}
		b.Hero = r[0]
		b.Abilities = r[1:]

		builds[b.Hero] = &b
	}

	return builds
}

// GetAramBuild gets the ARAM build guide for a hero
// It will check the supplied hero name against a list of known
// aliases for the heroes
func (b *AramBuilds) GetAramBuild(h string, force bool) (*AramBuild, error) {
	hero, _ := b.GetHeroName(h)

	if b == nil || b.Builds == nil {
		return nil, fmt.Errorf("builds are not defined, try running '!hots _sync' maybe?")
	}

	for _, b := range *b.Builds {
		if strings.ToLower(b.Hero) == strings.ToLower(hero) {
			return b, nil
		}
	}

	return nil, fmt.Errorf("failed to find build for '%s'", hero)
}

// GetHeroName tries to get the hero name from a map of aliases
// If it doesn't exist, it will return the original name together
// with an error.
func (b *AramBuilds) GetHeroName(hero string) (string, error) {
	for k, h := range b.HeroAliases {
		if hero == h {
			return hero, nil
		}
		if hero == k {
			return h, nil
		}
	}

	return hero, fmt.Errorf("hero '%s' missing in heroes map", hero)
}

// HandleDiscordMessage handles discord messages to craft replies based on
// the tokens of a message.
func (b *AramBuilds) HandleDiscordMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	b.once.Do(func() {
		builds := fetchAramBuilds(b.SheetID, b.SheetRange)
		b.Builds = &builds
		b.LastSync = time.Now()
		b.HeroAliases = readHeroAliasesMap()
	})

	tokens := strings.Split(m.Content[1:], " ")

	switch tokens[0] {
	case "aram", "hots":
		if len(tokens) == 1 {
			s.ChannelMessageSend(m.ChannelID, ":warning: Error: Add the hero name you want to look up.")
			return
		}

		// Magic keyword to re-fetch the builds
		if tokens[1] == "_sync" {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(":robot: Okay, I'll sync (last sync at %s).", b.LastSync.Format(time.RFC3339)))
			builds := fetchAramBuilds(b.SheetID, b.SheetRange)
			b.Builds = &builds
			b.LastSync = time.Now()
			s.ChannelMessageSend(m.ChannelID, ":robot: Done!")
			return
		}

		// Magic keyword for adding alias for hero
		if tokens[1] == "alias" {
			msg := b.handleAliasEdit(m.Content)
			s.ChannelMessageSend(m.ChannelID, msg)
			return
		}

		msg, err := b.handleAramMessage(strings.Join(tokens[1:], " "))

		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(":warning: Failed to lookup build. ('error: %s')", err))
			return
		}

		_, err = s.ChannelMessageSendEmbed(m.ChannelID, msg)

		if err != nil {
			fmt.Println("Failed to send ARAM message")
		}
		return
	}
}

func (b *AramBuilds) handleAramMessage(h string) (*discordgo.MessageEmbed, error) {

	hero, _ := b.GetHeroName(h)

	build, err := b.GetAramBuild(hero, false)
	if err != nil {
		return nil, err
	}

	fields := make([]*discordgo.MessageEmbedField, 0)

	level := 1.0
	for _, v := range build.Abilities {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Level %.0f", math.Floor(level)),
			Value:  v,
			Inline: true,
		})
		level += 3.0 + (1.0 / 6.0)
	}

	return &discordgo.MessageEmbed{
		URL:         fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/", b.SheetID),
		Title:       "HOTS ARAM Builds",
		Description: hero,
		Color:       0x40c7eb,
		Fields:      fields,
	}, nil
}

func (b *AramBuilds) handleAliasEdit(msg string) string {
	help := ":information_source: specify either 'add' or 'remove' followed by `alias=heroname`, e.g. `anub=anub'arak` or `ll=li li`"

	tokens := strings.Split(msg[1:], " ")
	if len(tokens) <= 3 {
		return help
	}

	switch strings.ToLower(tokens[2]) {
	case "add":
		{
			aliasTokens := strings.Split(strings.Join(tokens[3:], " "), "=")
			if len(aliasTokens) != 2 {
				return fmt.Sprintf(":warning: invalid number of arguments, expected 2, got %d", len(aliasTokens))
			}
			alias := strings.TrimSpace(aliasTokens[0])
			hero := strings.TrimSpace(aliasTokens[1])
			aliases := readHeroAliasesMap()
			aliases[alias] = hero
			b.HeroAliases = aliases
			writeHeroAliasesMap(aliases, true)
			return ":robot: Done!"
		}
	case "remove":
		{
			alias := strings.TrimSpace(tokens[3])
			aliases := readHeroAliasesMap()
			delete(aliases, alias)
			b.HeroAliases = aliases
			writeHeroAliasesMap(aliases, false)
			return ":robot: Done!"
		}
	case "help":
		{

			return help
		}
	default:
		{
			return fmt.Sprintf("Didn't understand '%s' as 'add' or 'remove'.\n", tokens[2]) + help
		}
	}
}
