package server

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
)

type ApplicationCommands struct {
	Commands map[string]*ApplicationCommand
}

type ApplicationCommand struct {
	Name     string
	ID       string
	Command  *discordgo.ApplicationCommand
	store    *DiscordServerStore
	inFlight map[string]*ReactRoleInteraction
}

func AvailableApplicationCommands(store *DiscordServerStore) []*ApplicationCommand {
	var adminCommandPerm int64
	adminCommandPerm = discordgo.PermissionManageServer
	commands := make([]*ApplicationCommand, 0)
	commands = append(commands, &ApplicationCommand{
		Name: "reactionroleregister",
		Command: &discordgo.ApplicationCommand{
			Name:                     "reactionroleregister",
			Version:                  "1",
			DefaultMemberPermissions: &adminCommandPerm,
			Type:                     discordgo.MessageApplicationCommand,
		},
		store:    store,
		inFlight: make(map[string]*ReactRoleInteraction),
	})

	log.WithField("available_commands", len(commands)).Info("Listing available commands")

	return commands
}

func (app *ApplicationCommands) HandleApplicationCommands(s *discordgo.Session, event *discordgo.InteractionCreate) {
	logger := log.WithFields(log.Fields{
		"interaction_id":   event.Interaction.AppID,
		"interaction_type": event.Interaction.Type,
	})
	logger.Info("Handling application command")

	interaction := event.Interaction
	logger.Debugf("interaction: %+v", interaction)

	for id, app := range app.Commands {
		interactionID := app.GetID(event)
		if interactionID == id {
			logger.WithField("application_name", app.Name).Debug("this is the app")
			if err := app.Respond(s, event); err != nil {
				log.WithError(err).Error("failed to respond")
			}
		}
	}
}

func (cmd *ApplicationCommand) GetID(event *discordgo.InteractionCreate) string {
	switch event.Type {
	// When the app is triggered (by clicking msg -> apps -> this app)
	case discordgo.InteractionApplicationCommand:
		data := event.ApplicationCommandData()
		return data.ID
	case discordgo.InteractionMessageComponent:
		data := event.MessageComponentData()
		return strings.Split(data.CustomID, ";")[0]
	}

	return ""
}

func (cmd *ApplicationCommand) GetAction(event *discordgo.InteractionCreate) ReactRoleAction {
	var interactionInProgress *ReactRoleInteraction
	var err error

	switch event.Data.Type() {
	case discordgo.InteractionApplicationCommand:
		data := event.ApplicationCommandData()
		interactionInProgress, err = cmd.store.GetReactRoleInteractionProgress(data.ID)
		if err != nil {
			log.WithError(err).Error("Failed to get action")
		}
	case discordgo.InteractionMessageComponent:
		id := strings.Split(event.MessageComponentData().CustomID, ";")[1]
		interactionInProgress, err = cmd.store.GetReactRoleInteractionProgress(id)
		if err != nil {
			log.WithError(err).Error("Failed to get action")
		}
	}

	return interactionInProgress.GetAction()
}

func (cmd *ApplicationCommand) GetInProgressID(event *discordgo.InteractionCreate) string {
	var interactionInProgress *ReactRoleInteraction
	var err error

	switch event.Data.Type() {
	case discordgo.InteractionApplicationCommand:
		data := event.ApplicationCommandData()
		interactionInProgress, err = cmd.store.GetReactRoleInteractionProgress(data.ID)
		if err != nil {
			log.WithError(err).Error("Failed to get current int progress id")
		}
	case discordgo.InteractionMessageComponent:
		id := strings.Split(event.MessageComponentData().CustomID, ";")[1]
		log.WithField("id", id).Debug("Got ID")
		interactionInProgress, err = cmd.store.GetReactRoleInteractionProgress(id)
		if err != nil {
			log.WithError(err).Error("Failed to get current int progress id")
		}
	}

	return interactionInProgress.ID
}

func (cmd *ApplicationCommand) Respond(s *discordgo.Session, event *discordgo.InteractionCreate) error {
	log.Info("Responding to interaction")
	interaction := event.Interaction

	switch event.Type {
	case discordgo.InteractionApplicationCommand:
		data := interaction.ApplicationCommandData()
		wip := ReactRoleInteraction{
			ChannelID: event.ChannelID,
			MessageID: data.TargetID,
		}
		id, err := cmd.store.CreateReactRoleInteractionProgress(&wip)
		wip.ID = id
		if err != nil {
			log.WithError(err).Error("failed to create interaction in progress")
		}
		v := discordgo.InteractionResponseChannelMessageWithSource
		response := discordgo.InteractionResponse{
			Type: v,
			Data: &discordgo.InteractionResponseData{
				Content: "Adding a reaction to this message which allows users to get the selected role when clicking it. First, select a role:",
				Flags:   discordgo.MessageFlagsEphemeral,
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.SelectMenu{
								MenuType: discordgo.RoleSelectMenu,
								CustomID: fmt.Sprintf("%s;%s", cmd.ID, wip.ID),
							},
						},
					},
				},
			},
		}

		return s.InteractionRespond(interaction, &response)

	case discordgo.InteractionMessageComponent:
		logger := log.WithField("event", "messagelol")
		logger.Info("Respond message component")

		data := event.MessageComponentData()

		id := cmd.GetInProgressID(event)
		logger = logger.WithFields(log.Fields{
			"in_progress_id": id,
		})

		var emojiHandlerCancel func()
		if cmd.inFlight[id] != nil {
			emojiHandlerCancel = cmd.inFlight[id].emojiHandler
		}
		wip, err := cmd.store.GetReactRoleInteractionProgress(id)
		if err != nil {
			return err
		}
		cmd.inFlight[id] = wip
		cmd.inFlight[id].emojiHandler = emojiHandlerCancel

		action := cmd.GetAction(event)
		logger = log.WithFields(log.Fields{
			"action": action,
		})

		switch action {
		case RoleSelect:
			roles := make([]string, 0)
			for _, role := range data.Resolved.Roles {
				roles = append(roles, role.ID)
			}
			roleIDs := strings.Join(roles, ",")
			wip.RoleID = roleIDs
			if err := cmd.store.StoreReactRoleInteractionProgress(wip); err != nil {
				return err
			}

			logger.Info("Role collected, prompting for emoji")

			handler := s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageReactionAdd) {
				cmd.roleReactInteractionEmojiHandler(s, m, event)
			})
			cmd.inFlight[id].emojiHandler = handler

			v := discordgo.InteractionResponseChannelMessageWithSource
			response := discordgo.InteractionResponse{
				Type: v,
				Data: &discordgo.InteractionResponseData{
					Content: "Add the emoji you want to use to the original message (where you want the user to click)",
					Flags:   discordgo.MessageFlagsEphemeral,
					Components: []discordgo.MessageComponent{
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.Button{
									Label:    "Add an emoji",
									Style:    discordgo.SecondaryButton,
									CustomID: "Nothing",
									Disabled: true,
								},
							},
						},
					},
				},
			}

			return s.InteractionRespond(interaction, &response)

			// case EmojiSelect:
			// Handled by roleReactInteractionEmojiHandler

		case Confirm:
			logger = logger.WithFields(log.Fields{
				"channel_id": wip.ChannelID,
				"message_id": wip.MessageID,
				"role_id":    wip.RoleID,
				"emoji":      wip.EmojiID,
			})
			logger.Info("Saving role reaction")

			if err := cmd.store.StoreReactRole(ReactRole{
				Message: &ReactRoleMessage{
					GuildID:   interaction.GuildID,
					ChannelID: wip.ChannelID,
					ID:        wip.MessageID,
				},
				Role:  wip.RoleID,
				Emoji: wip.EmojiID,
			}); err != nil {
				logger.WithError(err).Error("failed to store emoji in db")
				return fmt.Errorf("failed to store reaction role in db")
			}

			if !IsUnicodeEmoji(wip.EmojiID) {
				emojiID, err := s.State.Emoji(interaction.GuildID, wip.EmojiID)
				if err != nil {
					logger.WithError(err).Error("failed to get emoji")
				}
				wip.EmojiID = emojiID.APIName()
			}

			if err := s.MessageReactionsRemoveEmoji(wip.ChannelID, wip.MessageID, wip.EmojiID); err != nil {
				logger.WithError(err).Errorf("Failed to clear message of reaction %s", wip.EmojiID)
			}
			if err := s.MessageReactionAdd(wip.ChannelID, wip.MessageID, wip.EmojiID); err != nil {
				logger.WithError(err).Errorf("Failed to react to message with reaction %s", wip.EmojiID)
			}

			v := discordgo.InteractionResponseChannelMessageWithSource
			response := discordgo.InteractionResponse{
				Type: v,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf(":+1: Granting %s when user clicks on %s below this message: %s/%s", wip.RoleID, wip.EmojiID, wip.ChannelID, wip.MessageID),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			}
			if err := s.InteractionRespond(interaction, &response); err != nil {
				logger.WithError(err).Error("failed to acknowledge emoji add flow")
			}
			wip.emojiHandler()
		}
	}
	return nil
}

func (cmd *ApplicationCommand) roleReactInteractionEmojiHandler(s *discordgo.Session, m *discordgo.MessageReactionAdd, event *discordgo.InteractionCreate) {
	id := cmd.GetInProgressID(event)
	interactionInProgress, err := cmd.store.GetReactRoleInteractionProgress(id)
	if err != nil {
		log.WithError(err).Error("failed to get interaction in progress")
	}
	logger := log.WithFields(log.Fields{
		"in_progress_id": id,
	})

	if m.ChannelID != interactionInProgress.ChannelID || m.MessageID != interactionInProgress.MessageID {
		return
	}

	logger.Debug("Handling emoji/message selector")

	buttonText := "Add this emoji"
	buttonStyle := discordgo.PrimaryButton
	buttonEnabled := true

	emojiID := m.Emoji.ID
	if emojiID == "" {
		emojiID = m.Emoji.Name
	}

	if !GuildHasEmoji(emojiID, event.GuildID, s) {
		buttonText = "That emoji is invalid! Pick another one."
		buttonStyle = discordgo.DangerButton
		buttonEnabled = false
	}

	msg, err := s.InteractionResponseEdit(event.Interaction, &discordgo.WebhookEdit{
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						CustomID: fmt.Sprintf("%s;%s", cmd.ID, interactionInProgress.ID),
						Emoji: &discordgo.ComponentEmoji{
							Name:     m.Emoji.Name,
							ID:       m.Emoji.ID,
							Animated: m.Emoji.Animated,
						},
						Disabled: !buttonEnabled,
						Label:    buttonText,
						Style:    buttonStyle,
					},
				},
			},
		},
	})
	if err != nil {
		logger.WithError(err).WithField("msg", msg).Error("failed to create button massage")
		return
	}

	interactionInProgress.EmojiID = emojiID // TODO: use APIName() here
	cmd.store.StoreReactRoleInteractionProgress(interactionInProgress)
}
