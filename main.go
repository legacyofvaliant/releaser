package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

var (
	srcSrvUUID string
	dstSrvUUID string
	srcSrvDir  string
	dstSrvDir  string
	keepFiles  []string
)

func init() {
	srcSrvUUID = os.Getenv("SRC_SERVER_UUID")
	if srcSrvUUID == "" {
		log.Fatalf("No source server UUID found")
	}

	dstSrvUUID = os.Getenv("DST_SERVER_UUID")
	if dstSrvUUID == "" {
		log.Fatalf("No destination server UUID found")
	}

	baseDir := os.Getenv("SERVER_BASE_DIR")
	if baseDir == "" {
		baseDir = "/var/lib/pterodactyl/volumes/"
	}

	srcSrvDir = filepath.Join(baseDir, srcSrvUUID)
	dstSrvDir = filepath.Join(baseDir, dstSrvUUID)

	keepFiles = []string{}
	for _, v := range strings.Split(os.Getenv("KEEP_FILES"), ",") {
		if v == "" {
			continue
		}

		keepFiles = append(keepFiles, strings.TrimSpace(v))
	}
}

func main() {
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		log.Fatalf("No token found")
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("Error creating Discord session: %s", err)
	}

	dg.AddHandler(interactionCreate)

	err = dg.Open()
	if err != nil {
		log.Fatalf("Error opening Discord session: %s", err)
	}
	defer dg.Close()

	guilds, err := dg.UserGuilds(1, "", "", false)
	if err != nil {
		log.Fatalf("Error getting guilds: %s", err)
	} else if len(guilds) == 0 {
		log.Fatalf("No guilds found")
	}

	log.Printf("Creating application commands")
	cmd, err := dg.ApplicationCommandCreate(dg.State.User.ID, guilds[0].ID, &discordgo.ApplicationCommand{
		Name:        "copy",
		Description: "Copy server files from one server to another",
	})
	if err != nil {
		log.Fatalf("Error creating application commands: %s", err)
	}

	log.Printf("Bot is now running")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	log.Printf("Removing application commands")
	err = dg.ApplicationCommandDelete(dg.State.User.ID, guilds[0].ID, cmd.ID)
	if err != nil {
		log.Printf("Error deleting application commands: %s", err)
	}

	log.Printf("Bot has been stopped")
}

func interactionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionApplicationCommand {
		command := i.ApplicationCommandData()
		if command.Name == "copy" {
			ch := make(chan bool)
			go copy(ch, true)

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{
						{
							Color:       0xffff00,
							Title:       "Copying server files...",
							Description: ":warning: Do not add any modifications to the server files while copying!",
							Fields: []*discordgo.MessageEmbedField{
								{
									Name:   "Source Server",
									Value:  fmt.Sprintf("`%s`", srcSrvUUID),
									Inline: false,
								},
								{
									Name:   "Destination Server",
									Value:  fmt.Sprintf("`%s`", dstSrvUUID),
									Inline: false,
								},
								{
									Name:   "Keep Files",
									Value:  fmt.Sprintf("```\n%s\n```", strings.Join(keepFiles, "\n")),
									Inline: false,
								},
							},
						},
					},
				},
			})

			success := <-ch
			if success {
				s.ChannelMessageSendEmbed(i.ChannelID, &discordgo.MessageEmbed{
					Color:       0x00ff00,
					Description: ":white_check_mark: Copying has been completed!",
				})
			} else {
				s.ChannelMessageSendEmbed(i.ChannelID, &discordgo.MessageEmbed{
					Color:       0xff0000,
					Description: ":x: Copying has failed!",
				})
			}
		}
	}
}

func copy(success chan bool, delete bool) {
	if _, err := os.Stat(dstSrvDir); os.IsNotExist(err) {
		log.Printf("Destination directory %s does not exist", dstSrvDir)
		success <- false
		return
	}

	if delete {
		err := removeFiles(dstSrvDir)
		if err != nil {
			log.Printf("Error removing destination files: %s", err)
			success <- false
			return
		}
	}

	if _, err := os.Stat(srcSrvDir); os.IsNotExist(err) {
		log.Printf("Source directory %s does not exist", srcSrvDir)
		success <- false
		return
	}

	err := copyFiles(srcSrvDir, dstSrvDir)
	if err != nil {
		log.Printf("Error copying files: %s", err)
		success <- false
		return
	}

	success <- true
}

func removeFiles(dirPath string) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		fullpath := filepath.Join(dirPath, file.Name())

		if !isKeepFile(fullpath) {
			if file.IsDir() {
				err := removeFiles(fullpath)
				if err != nil {
					return err
				}
			}

			os.Remove(fullpath)
		}
	}

	return nil
}

func copyFiles(srcDirPath string, dstDirPath string) error {
	srcFiles, err := os.ReadDir(srcDirPath)
	if err != nil {
		return err
	}

	for _, srcFile := range srcFiles {
		srcFullpath := filepath.Join(srcDirPath, srcFile.Name())
		dstFullpath := filepath.Join(dstDirPath, srcFile.Name())

		if !isKeepFile(dstFullpath) {
			srcFileInfo, err := srcFile.Info()
			if err != nil {
				return err
			}

			if srcFile.IsDir() {
				err := os.MkdirAll(dstFullpath, srcFileInfo.Mode())
				if err != nil {
					return err
				}

				err = copyFiles(srcFullpath, dstFullpath)
				if err != nil {
					return err
				}
			} else {
				data, err := os.ReadFile(srcFullpath)
				if err != nil {
					return err
				}

				err = os.WriteFile(dstFullpath, data, srcFileInfo.Mode())
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func isKeepFile(file string) bool {
	absFile, err := filepath.Abs(file)
	if err != nil {
		log.Printf("Error getting absolute path of %s: %s", file, err)
		return false
	}

	for _, v := range keepFiles {
		absV, err := filepath.Abs(filepath.Join(dstSrvDir, v))
		if err != nil {
			log.Printf("Error getting absolute path of %s: %s", v, err)
			continue
		}

		if absFile == absV {
			return true
		}
	}

	return false
}
