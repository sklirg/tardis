CREATE TABLE servers (
    id VARCHAR(32) PRIMARY KEY
);

CREATE TABLE users (
    id VARCHAR(32) PRIMARY KEY
);

CREATE TABLE reaction_messages (
    channel VARCHAR(32),
    id VARCHAR(32),
    guild VARCHAR(32),
    PRIMARY KEY(guild, channel, id)
);

CREATE TABLE reaction_message_reactions (
    id SERIAL,
    message_guild VARCHAR(32),
    message_channel VARCHAR(32),
    message_id VARCHAR(32),
    reaction TEXT,
    role VARCHAR(32),
    CONSTRAINT reaction_message_reactions_message FOREIGN KEY (message_guild, message_channel, message_id) REFERENCES reaction_messages(guild, channel, id),
    CONSTRAINT reaction_message_channel_message_role_uniquer UNIQUE (message_channel, message_id, reaction)
);
