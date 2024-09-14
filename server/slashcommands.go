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
	Name    string
	ID      string
	Command *discordgo.ApplicationCommand
	store   *DiscordServerStore
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
		store: store,
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

func (cmd *ApplicationCommand) GetAction(event *discordgo.InteractionCreate) string {
	actionString := ""
	switch event.Data.Type() {
	case discordgo.InteractionApplicationCommand:
		data := event.ApplicationCommandData()
		actionString = data.ID
	case discordgo.InteractionMessageComponent:
		data := event.MessageComponentData()
		actionString = data.CustomID
	}

	components := strings.Split(actionString, ";")
	return strings.Split(components[len(components)-1], "=")[0]
}

func (cmd *ApplicationCommand) Respond(s *discordgo.Session, event *discordgo.InteractionCreate) error {
	log.Info("Responding to interaction")
	interaction := event.Interaction

	switch event.Type {
	case discordgo.InteractionApplicationCommand:
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
								CustomID: fmt.Sprintf("%s;r", cmd.ID),
							},
						},
					},
				},
			},
		}
		return s.InteractionRespond(interaction, &response)

	case discordgo.InteractionMessageComponent:
		data := event.MessageComponentData()
		action := cmd.GetAction(event)
		logger := log.WithFields(log.Fields{
			"action": action,
		})
		logger.Info("Respond message component")
		switch action {
		case "r":
			roles := make([]string, 0)
			for _, role := range data.Resolved.Roles {
				roles = append(roles, role.ID)
			}
			roleIDs := strings.Join(roles, ",")
			logger.Info("Role collected, prompting for emoji")

			// TODO: deal with cancelling this handler
			_ = s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageReactionAdd) {
				// FIXME implement this check:
				//if m.MessageID != originalMessageID

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
				customID := fmt.Sprintf("%s;%s;%s;r=%s;e=%s", cmd.ID, m.ChannelID, m.MessageID, roleIDs, emojiID)

				if msg, err := s.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{
					Components: &[]discordgo.MessageComponent{
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.Button{
									CustomID: customID,
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
				}); err != nil {
					logger.WithError(err).WithField("msg", msg).Error("failed to create button massage")
				}

			})

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
									// URL:   fmt.Sprintf("https://discord.com/channels/%s/%s/%s", data.),
								},
							},
						},
					},
				},
			}
			return s.InteractionRespond(interaction, &response)

		case "e":
			log.Info("Saving role reaction")

			channelID := strings.Split(data.CustomID, ";")[1]
			messageID := strings.Split(data.CustomID, ";")[2]

			roleComponent := strings.Split(data.CustomID, ";")[3]
			roleList := strings.Split(roleComponent, "=")[1]
			roleID := strings.Split(roleList, ",")[0]

			emojiComponent := strings.Split(data.CustomID, ";")[4]
			emoji := strings.Split(emojiComponent, "=")[1]

			logger = logger.WithFields(log.Fields{
				"channel_id": channelID,
				"message_id": messageID,
				"role_id":    roleID,
				"emoji":      emoji,
			})

			if err := cmd.store.StoreReactRole(ReactRole{
				Message: &ReactRoleMessage{
					GuildID:   interaction.GuildID,
					ChannelID: channelID,
					ID:        messageID,
				},
				Role:  roleID,
				Emoji: emoji,
			}); err != nil {
				return fmt.Errorf("failed to store reaction role in db")
			}

			if !IsUnicodeEmoji(emoji) {
				emojiID, err := s.State.Emoji(interaction.GuildID, emoji)
				if err != nil {
					logger.WithError(err).Error("failed to get emoji")
				}
				emoji = emojiID.APIName()
			}

			if err := s.MessageReactionsRemoveEmoji(channelID, messageID, emoji); err != nil {
				logger.WithError(err).Errorf("Failed to clear message of reaction %s", emoji)
			}
			if err := s.MessageReactionAdd(channelID, messageID, emoji); err != nil {
				logger.WithError(err).Errorf("Failed to react to message with reaction %s", emoji)
			}

			v := discordgo.InteractionResponseChannelMessageWithSource
			response := discordgo.InteractionResponse{
				Type: v,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf(":+1: Granting %s when user clicks on %s below this message: %s/%s", roleID, emoji, channelID, messageID),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			}
			if err := s.InteractionRespond(interaction, &response); err != nil {
				logger.WithError(err).Error("failed to acknowledge emoji add flow")
			}
		}
	}
	return nil
}
