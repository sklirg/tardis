package server

import (
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
	ID string
    ChannelID string
    GuildID string
}

type WelcomeChannel struct {
    GuildID string
    MessageChannelID string
    EmojiChannelID string
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
