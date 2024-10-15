CREATE TABLE welcome_channel (
    message_channel VARCHAR(32),
    emoji_channel VARCHAR(32),
    guild VARCHAR(32) UNIQUE,
    PRIMARY KEY(guild, message_channel, emoji_channel)
);
