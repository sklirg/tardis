package server

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
)

var db *sql.DB

// DiscordServerStore contains the relevant items for discord server management
type DiscordServerStore struct {
}

func init() {
	connectionStr := os.Getenv("DATABASE_URL")

	if connectionStr == "" {
		log.Error("Database URL is empty")
		return
	}

	var err error
	db, err = sql.Open("postgres", connectionStr)

	if err != nil {
		log.WithError(err).Error("Failed to connect to database")
		return
	}

	if err := db.Ping(); err != nil {
		log.WithError(err).Error("Failed to ping database")
		return
	}
}

// HandleDiscordMessage handles a relevant incoming discord message and responds to it
func (srv *DiscordServerStore) HandleDiscordMessage(s *discordgo.Session, m *discordgo.MessageCreate) error {
	logger := log.WithField("handler", "reactrole")
	logger = logger.WithFields(log.Fields{
		"author_id":       m.Author.ID,
		"author_nickname": m.Author.String(),
	})
	tokens := strings.Split(m.Content, " ")
	params := tokens[1:] // @ToDo: Fix allowing spaces by wrapping in ""
	if len(params) < 3 || params[0] == "help" {
		s.ChannelMessageSend(m.ChannelID, ":robot: !reactrole <channel ID> <message ID> <reaction> <role>. Omit <channel ID> if in same channel.")
		return nil
	}

	var channel, message, reaction, roleParam string
	if len(params) == 3 {
		logger.Trace("Using current channel as target for message")
		channel = m.ChannelID
		message = params[0]
		reaction = params[1]
		roleParam = params[2]
	} else {
		channel = params[0]
		message = params[1]
		reaction = params[2]
		roleParam = params[3]
	}
	logger = logger.WithFields(log.Fields{
		"channel_param": channel,
		"message_param": message,
		"emoji_param":   reaction,
		"role_param":    roleParam,
	})

	// Find emoji
	emojiName, err := GetValidEmoji(reaction, m.GuildID, s)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, ":robot: I can't find that emoji. It has to be from this server.")
		return fmt.Errorf("failed to find emoji")
	}
	logger = logger.WithField("emoji", emojiName)
	logger.Debug("Identified emoji")

	// Find role
	var role *discordgo.Role
	_, err = strconv.ParseUint(roleParam, 10, 64)
	roleParamIsID := err == nil // if we got no error parsing the param to int, it is potentially a valid role ID
	logger.WithField("role_param_is_id", roleParamIsID).Debug("Identified role param")

	if roleParamIsID {
		var err error
		if role, err = s.State.Role(m.GuildID, roleParam); err != nil {
			logger.WithError(err).Error("Failed to fetch role by id")
            return fmt.Errorf("Failed to fetch role by ID")
		}
    } else {
		// Role param is a string, most likely the role name. Let's try to find it.
		guild, err := s.State.Guild(m.GuildID)
		if err != nil {
			logger.WithError(err).Error("Failed fetching guild")
			return fmt.Errorf("failed to fetch guild")
		}

		for _, rol := range guild.Roles {
			if strings.ToLower(rol.Name) == strings.ToLower(roleParam) {
				role = rol
				break
			}
		}
		if role == nil {
			logger.Error("Failed finding role")
			return fmt.Errorf("failed to find role")
		}
        logger.WithField("role", role).Debug("Found role")
    }
	logger = logger.WithField("role_id", role.ID).WithField("role_name", role.Name)
	logger.Debug("Identified role")

	userPermissions := 0
	if perms, err := s.State.UserChannelPermissions(m.Author.ID, channel); err != nil {
		logger.WithError(err).Error("Failed to look up user permissions")
	} else {
		userPermissions = perms
	}
	logger.WithField("user_perms", userPermissions).Debug("Identified user permissions")

	if !hasPerms(discordgo.PermissionManageRoles, userPermissions) {
		s.MessageReactionAdd(m.ChannelID, m.ID, "👎")
		logger.Warn("User does not have enough permission to assign role, aborting")
		return fmt.Errorf("user has not enough permissions")
	}

	botPerms := 0
	if perms, err := s.State.UserChannelPermissions(s.State.Ready.User.ID, channel); err != nil {
		logger.WithError(err).Error("Failed to look up my own user permissions")
	} else {
		botPerms = perms
	}

	if !hasPerms(discordgo.PermissionManageRoles, botPerms) {
		s.ChannelMessageSend(m.ChannelID, ":robot: It seems like I don't have the correct permissions to assign this role. I need 'Manage Roles', and that role has to be above the one I am assigning.")
		log.Warn("Bot does not have enough permission to assign role")
		return fmt.Errorf("bot has not enough permissions")
	}

	msg, err := s.ChannelMessage(channel, message)
	if err != nil {
		logger.WithError(err).Error("Failed to find message")
		s.ChannelMessageSend(m.ChannelID, ":robot: I couldn't find that message :(")
		return fmt.Errorf("failed to find requested message")
	}
	msg.GuildID = m.GuildID // this is not set when we retrieve the message

    // All seems good, let's persist this
	// store in db smile
    if err := srv.StoreReactRole(ReactRole{
        Message: &ReactRoleMessage{
            GuildID: msg.GuildID,
            ChannelID: msg.ChannelID,
            ID: msg.ID,
        },
        Role: role.ID,
        Emoji: emojiName,
    }); err != nil {
        return fmt.Errorf("failed to store in db")
    }

	if err := s.MessageReactionAdd(msg.ChannelID, msg.ID, emojiName); err != nil {
		logger.WithError(err).Error("Failed to add reaction to message")
        
		return fmt.Errorf("failed to find message to add reaction to")
	}
	if _, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Okay! Giving %s to users when they click %s on that message in <#%s> (%s).", role.Mention(), reaction, channel, messageLink(msg))); err != nil {
		logger.WithError(err).Error("Failed to send message")
		return fmt.Errorf("failed to send response message")
	}
	// Finally, clean up the request message since it looks ugly AF
	if err := s.ChannelMessageDelete(m.ChannelID, m.ID); err != nil {
		logger.WithError(err).Error("Failed to delete request message")
		return fmt.Errorf("failed to delete request message")
	}
	return nil
}

// GetValidEmoji accepts a string containing exactly an emoji and
// returns a valid identifier to use with the Discord API.
// It will either be the Emoji ID or the UTF-8 emoji.
func GetValidEmoji(emoji, guildID string, s *discordgo.Session) (string, error) {
	if emoji[0] == '<' {
		if s == nil {
			return "", fmt.Errorf("Cannot return a valid emoji without a discordgo.Session")
		}
		parts := strings.Split(emoji, ":")
		id := parts[2]
		id = id[:len(id)-1] // Remove the '>'
		if emoji, err := s.State.Emoji(guildID, id); err == nil {
			return emoji.APIName(), nil
		} else {
			//s.ChannelMessageSend(m.ChannelID, ":robot: I can't find that emoji. It has to be from this server.")
			return "", err
		}
	}
	return emoji, nil
}

func hasPerms(expectedPermission, permissions int) bool {
	anded := expectedPermission & permissions
	r := anded == expectedPermission
	log.WithFields(log.Fields{
		"expected":       expectedPermission,
		"current":        permissions,
		"result":         anded,
		"access_granted": r,
	}).Trace("Comparing permissions")
	return r
}

func messageLink(msg *discordgo.Message) string {
	return fmt.Sprintf("<https://discordapp.com/channels/%s/%s/%s>", msg.GuildID, msg.ChannelID, msg.ID)
}

func Main() {
	log.Info("In server package")
}
