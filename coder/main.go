package coder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/network"
	log "github.com/sirupsen/logrus"
)

var UnsupportedLanguage = errors.New("unsupported programming language")
var TimeoutError = errors.New("code execution timeout")

func dockerRun(ctx context.Context, msg string) (<-chan *Code, error) {
	c := make(chan *Code, 1)

	defaultHeaders := map[string]string{"User-Agent": "engine-api-client-1.0"}
	cli, err := client.NewClient("unix:///var/run/docker.sock", "v1.22", nil, defaultHeaders)

	if err != nil {
		log.WithError(err).Error("Failed to init docker client")
		c <- nil
		return c, err
	}

	foo, err := cli.Info(ctx)
	if err != nil {
		log.WithError(err).Error("Failed to get cli.Info")
		c <- nil
		return c, err
	}
	log.Infof("Foo: %v", foo)

	code, err := parseCode(msg)
	if err != nil {
		c <- nil
		return c, err
	}

	// Fetch image
	imagePull, err := cli.ImagePull(ctx, fmt.Sprintf("docker.io/library/%s", code.Image.Image), types.ImagePullOptions{})
	if err != nil {
		c <- nil
		log.WithError(err).Error("failed to pull image")
		return c, err
	}
	content, err := ioutil.ReadAll(imagePull)
	if err != nil {
		log.WithError(err).Error("failed to read pulled docker image")
		return c, err
	}
	log.Tracef("image len: %d", len(content))

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
		c <- nil
		return c, err
	}

	startOptions := types.ContainerStartOptions{}
	if err := cli.ContainerStart(ctx, container.ID, startOptions); err != nil {
		log.WithError(err).Error("failed to start container")
		return c, err
	}
	defer func() {
		// Make sure to clean up containers
		deadline, timeout := ctx.Deadline()
		log.WithFields(log.Fields{
			"container_id": container.ID,
			"deadline":     deadline,
			"now":          time.Now(),
			"err":          ctx.Err(),
			"timeout":      timeout,
		}).Trace("calling containerkill func")
		if ctx.Err() != nil {
			log.WithField("container_id", container.ID).Debug("calling deferred container cleanup func")
			if err := cli.ContainerKill(context.Background(), container.ID, "SIGKILL"); err != nil {
				log.WithError(err).Error("failed to kill container")
			} else {
				log.Debug("successfully killed container")
			}
		}
	}()

	status, err := cli.ContainerWait(ctx, container.ID)
	if err != nil {
		_, timedOut := ctx.Deadline()
		log.WithError(err).WithField("context_timeout", timedOut).WithField("status", status).Error("Failed to wait for container exit")
		c <- nil
		return c, err
	}

	log.Debug("container wait done")

	// stdout
	stdoutReader, err := cli.ContainerLogs(ctx, container.ID, types.ContainerLogsOptions{
		ShowStdout: true,
	})
	if err != nil {
		log.WithError(err).Error("failed to get container logs")
		c <- nil
		return c, err
	}

	stdoutLogs, err := io.ReadAll(stdoutReader)
	if err != nil && err != io.EOF {
		log.WithError(err).Error("failed to read from logs reader")
		c <- nil
		return c, err
	}
	defer stdoutReader.Close()
	code.Stdout = strings.TrimSpace(string(stdoutLogs))
	log.Debugf("Logs: '%s'", code.Stdout)

	// stderr
	stderrReader, err := cli.ContainerLogs(ctx, container.ID, types.ContainerLogsOptions{
		ShowStderr: true,
	})
	if err != nil {
		log.WithError(err).Error("failed to get container logs")
		c <- nil
		return c, err
	}

	stderrLogs, err := io.ReadAll(stderrReader)
	if err != nil && err != io.EOF {
		log.WithError(err).Error("failed to read from logs reader")
		c <- nil
		return c, err
	}
	defer stderrReader.Close()
	code.Stderr = strings.TrimSpace(string(stderrLogs))
	log.Debugf("Logs (err): '%s'", code.Stderr)

	log.Debug("done creating code struct")

	log.Debug("posting code struct on chan")

	c <- code

	log.Debug("posted code struct on chan")

	return c, nil
}

func Run(s *discordgo.Session, m *discordgo.MessageCreate) error {
	log.Debug("running some code smile")
	if err := s.MessageReactionAdd(m.ChannelID, m.ID, "🚀"); err != nil {
		log.WithError(err).Error("failed to add start-code reaction")
	}
	//cli, err := client.NewEnvClient()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	ch, err := dockerRun(ctx, m.Content)

	var code *Code
	select {
	case code = <-ch:
		{
			log.Debug("got val from ch")
		}
	case <-ctx.Done():
		{
			log.WithError(ctx.Err()).Debug("select done with error")
		}
	}

	if err != nil {
		log.WithError(err).Error("dockerRun has err")
		return err
	}

	if err := ctx.Err(); err != nil {
		log.WithError(err).Error("ctx has err")
		s.MessageReactionAdd(m.ChannelID, m.ID, "⏰")
		return TimeoutError
	}

	lines := strings.Join(code.Lines, "\n")
	responseMessage := discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Coder"),
		Description: fmt.Sprintf("Parsed %s code:\n```%s\n%s\n```", code.Language, code.Language, lines),
		Fields:      []*discordgo.MessageEmbedField{},
	}
	output := strings.TrimSpace(string(code.Stdout))
	if len(output) == 0 {
		output = "_empty_"
	}
	responseMessage.Description = fmt.Sprintf("%s\n\n:technologist: Output (stdout):\n```%s\n%s```", responseMessage.Description, code.Language, output)
	log.Debugf("code.Stderr = '%#v'", code.Stderr)
	if len(code.Stderr) > 0 {
		responseMessage.Fields = append(responseMessage.Fields, &discordgo.MessageEmbedField{
			Name:   ":x: Errors (stderr)",
			Value:  fmt.Sprintf("```\n%s\n```", strings.TrimSpace(string(code.Stderr))),
			Inline: false,
		})
	}

	_, err = s.ChannelMessageSendEmbed(m.ChannelID, &responseMessage)
	if err != nil {
		log.WithError(err).Error("Failed to reply with output")
		s.ChannelMessageSend(m.ChannelID, ":robot: Failed to send reply. :( Maybe the output is too long?")
	}

	return nil
}

type Code struct {
	Lines    []string
	Language string
	Image    *CodeImage
	Stdout   string
	Stderr   string
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
	case "python":
		{
			return "python", nil
		}
	case "node", "js", "javascript":
		{
			return "javascript", nil
		}
	default:
		{
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
	case "javascript":
		{
			{
				return &CodeImage{
					Image: "node:lts",
					Cmd:   []string{"node", "-e"},
				}
			}
		}
	default:
		return nil
	}
}

func (code *Code) CreateCmd() (cmd []string) {
	cmd = []string{}
	switch code.Language {
	case "python":
		{
			cmd = append(cmd, code.Image.Cmd...)
		}
	case "javascript":
		{
			cmd = append(cmd, code.Image.Cmd...)
		}
	}
	cmd = append(cmd, strings.Join(code.Lines, "\n"))
	return cmd
}
