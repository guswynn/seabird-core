package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"gopkg.in/fsnotify.v1"

	seabird "github.com/seabird-irc/seabird-core"
)

func ReadTokenFile(filename string) (map[string]string, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config seabird.ServerConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return config.Tokens, nil
}

func EnvDefault(key string, def string) string {
	if ret, ok := os.LookupEnv(key); ok {
		return ret
	}
	return def
}

func Env(key string) string {
	ret, ok := os.LookupEnv(key)

	if !ok {
		logrus.WithField("var", key).Fatal("Required environment variable not found")
	}

	return ret
}

func main() {
	// Attempt to load from .env if it exists
	_ = godotenv.Load()

	logrus.SetLevel(logrus.InfoLevel)

	nick := Env("SEABIRD_NICK")
	user := EnvDefault("SEABIRD_USER", nick)
	name := EnvDefault("SEABIRD_NAME", user)

	tokensFile, err := filepath.Abs(Env("SEABIRD_TOKEN_FILE"))
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get absolute path of token file")
	}

	tokensDir := filepath.Dir(tokensFile)
	tokensFilename := filepath.Base(tokensFile)

	server, err := seabird.NewServer(seabird.ServerConfig{
		IrcHost:       Env("SEABIRD_IRC_HOST"),
		CommandPrefix: EnvDefault("SEABIRD_COMMAND_PREFIX", "!"),
		BindHost:      EnvDefault("SEABIRD_BIND_HOST", ":11235"),
		EnableWeb:     strings.ToLower(EnvDefault("SEABIRD_ENABLE_WEB", "true")) == "true",
		Nick:          nick,
		User:          user,
		Name:          name,
		Pass:          os.Getenv("SEABIRD_PASS"),
	})
	if err != nil {
		logrus.WithError(err).Fatal("failed to create server")
	}

	tokens, err := ReadTokenFile(tokensFile)
	if err != nil {
		logrus.WithError(err).Fatal("failed to load tokens")
	}
	server.SetTokens(tokens)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	err = watcher.Add(tokensDir)
	if err != nil {
		logrus.Fatal(err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					logrus.Fatal("fswatcher exited early")
				}

				logrus.Debugf("fswatch event: %v", event.Name)

				if filepath.Base(event.Name) != tokensFilename {
					logrus.Debugf(
						"Skipping file event because %q != %q",
						filepath.Base(event.Name), tokensFilename,
					)
					continue
				}

				if event.Op&fsnotify.Write != fsnotify.Write && event.Op&fsnotify.Create != fsnotify.Create {
					logrus.Debug("Skipping file event because event was not Write or Create")
					continue
				}

				logrus.Infof("tokens file modified: %s", event.Name)
				tokens, err := ReadTokenFile(tokensFile)
				if err != nil {
					logrus.WithError(err).Warn("failed to read file")
					continue
				}
				server.SetTokens(tokens)
			case err, ok := <-watcher.Errors:
				if !ok {
					logrus.Fatal("fswatcher exited early")
				}
				logrus.WithError(err).Warn("failed to read file")
			}
		}
	}()

	err = server.Run()
	if err != nil {
		logrus.Fatalf("failed to start server: %v", err)
	}
}