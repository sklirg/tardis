package server

import (
	"encoding/json"
	"errors"

	log "github.com/sirupsen/logrus"

	// Imported for side effects
	_ "github.com/lib/pq"
)

type ReactRole struct {
	Message *ReactRoleMessage
	Emoji   string
	Role    string
	id      int
}

type ReactRoleMessage struct {
	ID        string
	ChannelID string
	GuildID   string
}

type WelcomeChannel struct {
	GuildID          string
	MessageChannelID string
	EmojiChannelID   string
}

func (srv *DiscordServerStore) StoreReactRole(rr ReactRole) error {
	if db == nil {
		log.Error("Database is nil!")
		return errors.New("not connected to database")
	}

	log.Debug("Inserting ReactRoleMessage in DB")
	_, err := db.Query("INSERT INTO reaction_messages (guild, channel, id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING", rr.Message.GuildID, rr.Message.ChannelID, rr.Message.ID)
	if err != nil {
		log.WithError(err).Error("Failed to insert reaction messages")
		return err
	}

	log.Debug("Inserting ReactRole in DB")
	_, err = db.Query("INSERT INTO reaction_message_reactions (message_guild, message_channel, message_id, reaction, role) VALUES ($1, $2, $3, $4, $5)", rr.Message.GuildID, rr.Message.ChannelID, rr.Message.ID, rr.Emoji, rr.Role)
	if err != nil {
		log.WithError(err).Error("Failed to insert reaction messages")
		return err
	}

	return nil
}

func (srv *DiscordServerStore) GetReactRolesForMessage(rm ReactRoleMessage) ([]*ReactRole, error) {
	if db == nil {
		log.Error("Database is nil!")
		return nil, errors.New("not connected to database")
	}

	rows, err := db.Query("SELECT id, reaction, role FROM reaction_message_reactions WHERE message_guild = $1 AND message_channel = $2 AND message_id = $3", rm.GuildID, rm.ChannelID, rm.ID)
	if err != nil {
		log.WithError(err).Error("Failed to SELECT")
	}

	roles := make([]*ReactRole, 0) // len of res?
	for rows.Next() {
		rr := ReactRole{
			Message: &rm,
		}
		if err := rows.Scan(&rr.id, &rr.Emoji, &rr.Role); err != nil {
			log.WithError(err).Error("Failed to Scan() reaction role messages reactions")
			return nil, err
		}
		roles = append(roles, &rr)
	}
	if len(roles) == 0 {
		log.Trace("Found no reactrolemessagereactions for message")
		return nil, nil
	}
	return roles, nil
}

func (srv *DiscordServerStore) StoreWelcomeChannel(w WelcomeChannel) error {
	if db == nil {
		log.Error("Database is nil!")
		return errors.New("not connected to database")
	}

	log.Debug("Inserting Welcome Channel in DB")
	_, err := db.Query("INSERT INTO welcome_channel (guild, message_channel, emoji_channel) VALUES ($1, $2, $3) ON CONFLICT (guild) DO UPDATE SET message_channel = $2", w.GuildID, w.MessageChannelID, w.EmojiChannelID)
	if err != nil {
		log.WithError(err).Error("Failed to insert welcome channel")
		return err
	}

	return nil
}

func (srv *DiscordServerStore) GetWelcomeChannel(guildID string) (*WelcomeChannel, error) {
	if db == nil {
		log.Error("Database is nil!")
		return nil, errors.New("not connected to database")
	}

	log.Debug("Fetching welcome channel from DB")
	rows, err := db.Query("SELECT guild, message_channel, emoji_channel FROM welcome_channel WHERE guild = $1", guildID)
	if err != nil {
		log.WithError(err).Error("Failed to fetch welcome channel from DB")
		return nil, err
	}

	for rows.Next() {
		w := WelcomeChannel{}
		if err := rows.Scan(&w.GuildID, &w.MessageChannelID, &w.EmojiChannelID); err != nil {
			log.WithError(err).Error("Failed to scan database row")
			return &w, err
		}
		return &w, nil
	}
	return nil, nil
}

func (srv *DiscordServerStore) CreateReactRoleInteractionProgress(wip *ReactRoleInteraction) (string, error) {
	if db == nil {
		log.Error("Database is nil!")
		return "", errors.New("not connected to database")
	}

	log.Debug("Creating interaction in progress in DB")

	data, err := json.Marshal(wip)
	if err != nil {
		log.WithError(err).Error("failed to create placeholder react role interaction in progress")
	}

	rows, err := db.Query("INSERT INTO interaction_in_progress (data) VALUES($1) RETURNING id", data)
	if err != nil {
		log.WithError(err).Error("Failed to create interaction in progress")
		return "", err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			log.WithError(err).Error("Failed to scan database row")
			return id, err
		}
		return id, nil
	}

	return "", nil
}

func (srv *DiscordServerStore) GetReactRoleInteractionProgress(id string) (*ReactRoleInteraction, error) {
	if db == nil {
		log.Error("Database is nil!")
		return nil, errors.New("not connected to database")
	}

	log.WithField("id", id).Debug("Getting interaction in progress from DB")
	rows, err := db.Query("SELECT id, data FROM interaction_in_progress WHERE id = $1", id)
	if err != nil {
		log.WithError(err).Error("Failed to get interaction in progress")
		return nil, err
	}
	for rows.Next() {
		var id string
		var j []byte
		var data ReactRoleInteraction
		if err := rows.Scan(&id, &j); err != nil {
			log.WithError(err).Error("Failed to scan database row")
			return nil, err
		}
		err := json.Unmarshal(j, &data)
		data.ID = id
		if err != nil {
			log.WithError(err).Error("failed to unmarshal interaction in prgoress")
		}
		log.WithFields(log.Fields{
			"interaction": data,
		}).Debug("Got this interaction in progress")
		return &data, nil
	}

	return nil, nil
}

func (srv *DiscordServerStore) StoreReactRoleInteractionProgress(interaction *ReactRoleInteraction) error {
	if db == nil {
		log.Error("Database is nil!")
		return errors.New("not connected to database")
	}

	data, _ := json.Marshal(interaction)

	log.Debug("Inserting interaction in progress in DB")
	_, err := db.Query(`
                INSERT INTO interaction_in_progress
                        (id, data)
                VALUES ($1, $2)
                ON CONFLICT (id)
                DO UPDATE SET data = $2`,
		interaction.ID, data)
	if err != nil {
		log.WithError(err).Error("Failed to insert interaction in progress")
		return err
	}

	return nil
}

type ReactRoleInteraction struct {
	ID           string `json:"id"`
	ChannelID    string `json:"channel_id"`
	MessageID    string `json:"message_id"`
	EmojiID      string `json:"emoji_id"`
	RoleID       string `json:"role_id"`
	emojiHandler func()
}

type ReactRoleAction int

const (
	Initial ReactRoleAction = iota + 1
	RoleSelect
	EmojiSelect
	Confirm
)

func (i *ReactRoleInteraction) GetAction() ReactRoleAction {
	if i.ChannelID == "" || i.MessageID == "" || i.RoleID == "" {
		return RoleSelect
	} else if i.EmojiID == "" {
		return EmojiSelect
	} else {
		return Confirm
	}
}
