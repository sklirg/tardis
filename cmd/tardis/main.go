package tardis

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
	"github.com/sklirg/tardis/coder"
	"github.com/sklirg/tardis/hots"
	"github.com/sklirg/tardis/server"
)

type tardis struct {
	AramBuilds       *hots.AramBuilds
	ServerManager    server.DiscordServerStore
	DevMode          bool
	DevListenChannel string
	DevGuildID       string
	WelcomeChannel   map[string]*server.WelcomeChannel
	Commands         *server.ApplicationCommands
	dg               *discordgo.Session

	cleanUpMissingMembers bool
}

func Run() {
	discordBotToken := os.Getenv("TARDIS_DISCORD_TOKEN")
	applicationID := os.Getenv("TARDIS_APPLICATION_ID")

	applicationCommands := server.ApplicationCommands{Commands: make(map[string]*server.ApplicationCommand)}

	state := tardis{
		DevMode:    os.Getenv("TARDIS_DEV") != "",
		DevGuildID: os.Getenv("TARDIS_DEV_GUILD"),
		AramBuilds: &hots.AramBuilds{
			SheetID:    os.Getenv("TARDIS_HOTS_ARAM_SHEET_ID"),
			SheetRange: os.Getenv("TARDIS_HOTS_ARAM_SHEET_RANGE"),
		},
		ServerManager: server.DiscordServerStore{},
		Commands:      &applicationCommands,

		cleanUpMissingMembers: false,
	}

	if state.DevMode {
		log.Info("Starting in dev mode")
		if state.DevGuildID != "" {
			log.WithField("guild_id", state.DevGuildID).Info("DevMode with Developer Guild enabled")
		}
		log.SetLevel(log.TraceLevel)
	}

	state.WelcomeChannel = make(map[string]*server.WelcomeChannel)

	dg, err := discordConnect(discordBotToken)
	if err != nil {
		fmt.Println("error connecting", err)
		return
	}

	state.dg = dg

	dg.AddHandler(state.messageCreate)
	dg.AddHandler(state.handleReactionAdd)
	dg.AddHandler(state.handleReactionRemove)
	dg.AddHandler(state.handleMemberJoin)
	dg.AddHandler(state.handleApplicationCommands)
	dg.AddHandler(state.handleMemberChunk)
	dg.AddHandler(state.handleGuildReady)

	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	log.Info("Bot is now running. Press CTRL-C to exit.")

	for _, guild := range dg.State.Ready.Guilds {
		// Fetch guild info from database if we have it
		log.WithField("guild_id", guild.ID).Debugf("Connected to '%s'", guild.Name)
	}

	log.Infof("Registering %d application commands", len(server.AvailableApplicationCommands(nil)))

	for _, guild := range dg.State.Ready.Guilds {
		for _, applicationCommand := range server.AvailableApplicationCommands(&state.ServerManager) {
			logger := log.WithField("application_name", applicationCommand.Name)
			registered, err := dg.ApplicationCommandCreate(applicationID, guild.ID, applicationCommand.Command)
			if err != nil {
				logger.WithError(err).Errorf("Failed to register command: %v", applicationCommand.Command)
			} else {
				logger.WithField("id", registered.ID).Info("Registered command")
				applicationCommand.ID = registered.ID
				state.Commands.Commands[registered.ID] = applicationCommand
			}
		}
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	log.Info("Received interrupt, shutting down.")

	for _, guild := range dg.State.Ready.Guilds {
		for id := range state.Commands.Commands {
			if err := dg.ApplicationCommandDelete(applicationID, guild.ID, id); err != nil {
				log.WithError(err).Warn("failed to delete application")
			}
		}
	}

	dg.Close()
}

func discordConnect(token string) (*discordgo.Session, error) {
	client, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Println("Error setting up session: ", err)
		return nil, err
	}

	client.Identify = discordgo.Identify{
		Token: token,
		Properties: discordgo.IdentifyProperties{
			OS:      "linux",
			Browser: "tardis",
			Device:  "tardis",
		},
		Intents: discordgo.IntentsAllWithoutPrivileged | discordgo.IntentsGuildMembers | discordgo.IntentsGuildMessages,
	}

	client.StateEnabled = true
	client.State.TrackMembers = true
	client.State.TrackRoles = true

	return client, nil
}

func (tardis *tardis) handleGuildReady(s *discordgo.Session, _ *discordgo.Ready) {
	log.Info("Received guilds ready event")

	for _, guild := range s.State.Guilds {
		tardis.dg.State.GuildAdd(guild)
		log.WithField("guild_id", guild.ID).Info("Requesting guild members")
		if err := s.RequestGuildMembers(guild.ID, "", 0, "", false); err != nil {
			log.WithError(err).Error("failed to request guild members")
			continue
		}
	}
}

func (tardis *tardis) handleMemberChunk(_ *discordgo.Session, c *discordgo.GuildMembersChunk) {
	log.Infof("Got guild member chunk %d of %d (%d members)", c.ChunkIndex+1, c.ChunkCount, len(c.Members))

	for _, member := range c.Members {
		if err := tardis.dg.State.MemberAdd(member); err != nil {
			log.WithError(err).Error("failed to add guild member")
			break
		}
	}

	if c.ChunkIndex == c.ChunkCount-1 {
		if err := tardis.syncReactionRoles(c.GuildID); err != nil {
			log.WithError(err).Error("failed to sync reaction roles")
		}
	}
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

	tokens := strings.Split(strings.ReplaceAll(m.Content[1:], "\n", " "), " ")

	if len(tokens) == 0 {
		return
	}

	trigger := tokens[0]

	if tardis.DevMode {
		if !(trigger == "listen" || m.ChannelID == tardis.DevListenChannel || m.GuildID == tardis.DevGuildID) {
			// In DevMode and received message in a channel I don't listen to, so skip
			// But will allow the keyword 'listen' through
			return
		}
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
	case "setwelcomechannel":
		{
			canSet := false
			for _, role := range m.Member.Roles {
				log.WithField("role", role).Debug("User has role")
				if role == "697835126733799476" || role == "173844144609951744" {
					canSet = true
					break
				}
			}
			if !canSet {
				return
			}
			w := server.WelcomeChannel{
				GuildID:          m.GuildID,
				MessageChannelID: m.ChannelID,
			}
			if len(tokens) >= 2 {
				if tokens[1][0] == '<' {
					chanID := tokens[1][2 : len(tokens[1])-1]
					log.WithField("channel_id", chanID).WithField("token", tokens[1]).Debug("Looking up emoji channel")
					if c, err := s.Channel(chanID); err == nil && c != nil {
						log.WithField("channel", c.Name).Debug("Found emoji channel")
						w.EmojiChannelID = c.ID
					} else {
						log.WithError(err).WithField("channel", c).Debug("Failed to lookup emoji channel")
					}
				}
			}
			if err := tardis.ServerManager.StoreWelcomeChannel(w); err != nil {
				s.MessageReactionAdd(m.ChannelID, m.ID, "👎")
				s.ChannelMessageSend(m.ChannelID, ":robot: Failed to set welcome channel.")
			} else {
				s.MessageReactionAdd(m.ChannelID, m.ID, "👍")
				tardis.WelcomeChannel[w.GuildID] = &w
			}
		}
	case "run":
		{
			if err := coder.Run(s, m); err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf(":x: Something went wrong: %s", err))
				coder.SendHelp(s, m)
			}
		}
	default:
		{
			if tardis.DevMode {
				logger.Debug("Received unknown trigger")
			}
		}
	}
}

func (t *tardis) handleReactionAdd(s *discordgo.Session, reaction *discordgo.MessageReactionAdd) {
	if reaction.UserID == s.State.User.ID {
		return
	}
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
	if reaction.UserID == s.State.SessionID {
		return
	}
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

func (t *tardis) handleMemberJoin(s *discordgo.Session, join *discordgo.GuildMemberAdd) {
	log.Infof("Handling member join, %s, at %s", join.DisplayName(), join.JoinedAt)
	guildID := join.GuildID
	ch := t.WelcomeChannel[guildID]
	if ch == nil {
		welcomeChan, err := t.ServerManager.GetWelcomeChannel(guildID)
		if err != nil {
			return
		}
		if welcomeChan == nil {
			log.Error("Could not find a channel to write welcomes to")
			return
		}
		t.WelcomeChannel[guildID] = welcomeChan
		ch = welcomeChan
	}
	if ch.EmojiChannelID == "" {
		// Skip if we don't have a stored emoji channel
		return
	}
	s.ChannelMessageSend(ch.MessageChannelID, fmt.Sprintf("Welcome, %s! The channel you are looking for might be hidden, or appear locked, but you can access them after you've clicked the appropriate emoji-reaction below the message in <#%s>. (tip: click on the link to go directly to that message)", join.Mention(), ch.EmojiChannelID))
}

func handleHelp(s *discordgo.Session, m *discordgo.MessageCreate) {
	fieldTexts := [][]string{
		{"hots", "aliases: aram | find build guides for HotS ARAM matches"},
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

func (tardis *tardis) handleApplicationCommands(s *discordgo.Session, event *discordgo.InteractionCreate) {
	tardis.Commands.HandleApplicationCommands(s, event)
}

func (tardis *tardis) syncReactionRoles(guildID string) error {
	logger := log.WithField("func", "syncReactionRoles")
	logger.Info("Syncing reaction roles")

	roles, err := tardis.ServerManager.GetReactionRoles()
	if err != nil {
		return err
	}

	for _, role := range roles {
		logger = logger.WithField("role_id", role.Role)

		paginationEnd := ""
		users := make([]*discordgo.User, 0)
		for {
			reactions, err := tardis.dg.MessageReactions(role.Message.ChannelID, role.Message.ID, role.Emoji, 100, "", paginationEnd)
			if err != nil {
				logger.Error("failed to get reactions for emoji")
				break
			}
			logger.Infof("found %d %s reactions on %s, last one: %s, pageEnd: %s", len(reactions), role.Emoji, role.Message.ID, reactions[len(reactions)-1].ID, paginationEnd)
			users = append(users, reactions...)

			if len(reactions) < 100 || paginationEnd == reactions[len(reactions)-1].ID {
				break
			}
			paginationEnd = reactions[len(reactions)-1].ID
		}

		for _, user := range users {
			logger = logger.WithFields(log.Fields{
				"user_display_name": user.String(),
				"user_id":           user.ID,
			})
			member, err := tardis.dg.State.Member(guildID, user.ID)
			if err != nil {
				logger.WithError(err).Warnf("Failed getting guild member, they might not be a member any more")

				// Clean up reactions from the missing members
				if tardis.cleanUpMissingMembers {
					if err := tardis.dg.MessageReactionRemove(role.Message.ChannelID, role.Message.ID, role.Emoji, user.ID); err != nil {
						logger.WithError(err).Error("Failed to clean up emoji for probably not a member any more")
					}
				}
				continue
			}

			// Figure out if user already has the role,
			hasRole := false
			for _, roleID := range member.Roles {
				if roleID == role.Role {
					hasRole = true
					break
				}
			}
			// if so, we can skip adding it.
			if hasRole {
				continue
			}

			logger.Info("Adding role to user")

			if err := tardis.dg.GuildMemberRoleAdd(role.Message.GuildID, user.ID, role.Role); err != nil {
				logger.WithError(err).Error("failed to sync role for user")
			}
		}

	}
	return nil
}
