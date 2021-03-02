package coder

import (
	"context"
    "errors"
	"fmt"
	"io"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/network"
	log "github.com/sirupsen/logrus"
)

var UnsupportedLanguage = errors.New("unsupported programming language")

func Run(s *discordgo.Session, m *discordgo.MessageCreate) error {
	log.Debug("running some code smile")
	//cli, err := client.NewEnvClient()
	defaultHeaders := map[string]string{"User-Agent": "engine-api-client-1.0"}
	cli, err := client.NewClient("unix:///var/run/docker.sock", "v1.22", nil, defaultHeaders)

	if err != nil {
		log.WithError(err).Error("Failed to init docker client")
	}
	ctx := context.Background()

	foo, err := cli.Info(ctx)
	if err != nil {
		log.WithError(err).Error("Failed to get cli.Info")
		return err
	}
	log.Infof("Foo: %v", foo)

	code, err := parseCode(m.Content)
    if err != nil {
        return err
    }
    
    // Fetch image
    if _, err := cli.ImagePull(ctx, fmt.Sprintf("docker.io/library/%s", code.Image.Image), types.ImagePullOptions{}); err != nil {
        log.WithError(err).Error("failed to pull image")
        return err
    }

	conf := container.Config{
		Image: code.Image.Image,
		Cmd:   code.CreateCmd(),
	}
	hostConf := container.HostConfig{
		AutoRemove: true,
	}
	netConf := network.NetworkingConfig{}
	name := ""
	container, err := cli.ContainerCreate(ctx, &conf, &hostConf, &netConf, name)
	if err != nil {
		log.WithError(err).Error("Failed to create container")
		return err
	}

	startOptions := types.ContainerStartOptions{}
	if err := cli.ContainerStart(ctx, container.ID, startOptions); err != nil {
		log.WithError(err).Error("failed to start container")
		return err
	}

	status, err := cli.ContainerWait(ctx, container.ID)
	if err != nil {
		log.WithError(err).WithField("status", status).Error("Failed to wait for container exit")
		return err
	}

	log.Debug("container wait done")

    // stdout
	stdoutReader, err := cli.ContainerLogs(ctx, container.ID, types.ContainerLogsOptions{
		ShowStdout: true,
	})
	if err != nil {
		log.WithError(err).Error("failed to get container logs")
		return err
	}

	stdoutLogs, err := io.ReadAll(stdoutReader)
	if err != nil && err != io.EOF {
		log.WithError(err).Error("failed to read from logs reader")
		return err
	}
	defer stdoutReader.Close()
	log.Debugf("Logs: %s", string(stdoutLogs))

    // stderr
	stderrReader, err := cli.ContainerLogs(ctx, container.ID, types.ContainerLogsOptions{
		ShowStderr: true,
	})
	if err != nil {
		log.WithError(err).Error("failed to get container logs")
		return err
	}

	stderrLogs, err := io.ReadAll(stderrReader)
	if err != nil && err != io.EOF {
		log.WithError(err).Error("failed to read from logs reader")
		return err
	}
	defer stderrReader.Close()
	log.Debugf("Logs (err): %s", string(stderrLogs))

    lines := strings.Join(code.Lines, "\n")
    responseMessage := discordgo.MessageEmbed{
        Title: fmt.Sprintf("Coder"),
        Description: fmt.Sprintf("Parsed %s code:\n```%s\n%s\n```", code.Language, code.Language, lines),
        Fields: []*discordgo.MessageEmbedField{},
    }
    output := string(stdoutLogs)
    if output == "" {
        output = "_empty_"
    }
    responseMessage.Fields = append(responseMessage.Fields, &discordgo.MessageEmbedField{
            Name: ":technologist: Output (stdout)",
            Value: output,
            Inline: false,
    })
    if string(stderrLogs) != "" {
        responseMessage.Fields = append(responseMessage.Fields, &discordgo.MessageEmbedField{
                Name: ":x: Errors (stderr)",
                Value: fmt.Sprintf("```\n%s\n```", string(stderrLogs)),
                Inline: false,
        })
    }
    
	_, err = s.ChannelMessageSendEmbed(m.ChannelID, &responseMessage)
	if err != nil {
		log.WithError(err).Error("Failed to reply with output")
	}

	return nil
}

type Code struct {
	Lines    []string
	Language string
	Image    *CodeImage
}

type CodeImage struct {
	Image string
	Cmd   []string
}

func parseCode(msg string) (*Code, error) {
	logger := log.WithField("msg", msg)
	logger.Debug("parsing code from message")

	//scan until ```
	c := strings.Split(msg, "```")
	// 0 is before the ```,
	// so 1 will be inside it.
	// if there is an even number of ```, we have more code blocks maybe
    // @TODO ^

    if len(c) <= 1 {
        return nil, fmt.Errorf("could not parse any code")
    }

	code1 := c[1]
	for i, line := range strings.Split(code1, "\n") {
		logger.WithField("i", i).Tracef("Code: %s", line)
	}

	// language will always be before the first newline
	lines := strings.Split(code1, "\n")
	lang, err := parseLang(lines[0])
    if err != nil {
        return nil, err
    }

    if len(lines) <= 1 {
        return nil, fmt.Errorf("no lines of code to run")
    }

    if strings.ReplaceAll(lang, " ", "") == "" {
        return nil, fmt.Errorf("unsure of what language this code is, try adding the language after the triple backticks, e.g. ```python")
    }

	logger.WithFields(log.Fields{
		"lines": lines[1:],
		"lang":  lang,
		"image": langToImage(lang),
	}).Debug("returning parsed codes")

	return &Code{
		Lines:    lines[1:],
		Language: lang,
		Image:    langToImage(lang),
	}, nil
}

func parseLang(msg string) (string, error) {
    switch msg {
        case "python": {
            return "python", nil
        }
        case "node", "js", "javascript": {
            return "javascript", nil
        }
        default: {
            return "", UnsupportedLanguage
        }
    }
}

func langToImage(msg string) *CodeImage {
	switch msg {
	case "python":
		{
			return &CodeImage{
				Image: "python:3",
				Cmd:   []string{"python", "-c"},
			}
		}
    case "javascript": {
        {
            return &CodeImage{
                Image: "node:lts",
                Cmd: []string{"node", "-e"},
            }
        }
    }
	default: return nil
	}
}

func (code *Code) CreateCmd() (cmd []string) {
    cmd = []string{}
    switch code.Language {
        case "python": {
            cmd = append(cmd, code.Image.Cmd...)
        }
        case "javascript": {
            cmd = append(cmd, code.Image.Cmd...)
        }
    }
    cmd = append(cmd, strings.Join(code.Lines, "\n"))
    return cmd
}
