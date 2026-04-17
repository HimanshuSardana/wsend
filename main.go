package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"google.golang.org/protobuf/proto"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
)

var client *whatsmeow.Client

func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Printf("Received a message from %s: %s\n", v.Info.Chat, v.Message.GetConversation())
	case *events.LoggedOut:
		fmt.Println("Logged out, please re-scan the QR code")
		qrChan, _ := client.GetQRChannel(context.Background())
		err := client.Connect()
		if err != nil {
			fmt.Printf("Connection error: %v\n", err)
			return
		}
		for e := range qrChan {
			if e.Event == "code" {
				displayQRCode(e.Code)
			} else {
				fmt.Printf("Login event: %s\n", e.Event)
			}
		}
	}
}

func displayQRCode(code string) {
	config := qrterminal.Config{
		Level:      qrterminal.M,
		Writer:     os.Stdout,
		HalfBlocks: true,
	}
	qrterminal.GenerateWithConfig(code, config)
	fmt.Println("\nScan this QR code with WhatsApp on your phone")
	fmt.Println("Or press Ctrl+C to exit")
}

func main() {
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	ctx := context.Background()
		// Determine the path to the database file dynamically based on the binary location
	exePath, _ := os.Executable()
	dirPath := filepath.Dir(exePath)
	dbPath := filepath.Join(dirPath, "store.db")
	if customPath := os.Getenv("WSEND_DB_PATH"); customPath != "" {
		dbPath = customPath
	}
	container, err := sqlstore.New(ctx, "sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "DEBUG", true)
	client = whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(eventHandler)

	if client.Store.ID == nil {
		fmt.Println("New device - scanning QR code...")
		qrChan, _ := client.GetQRChannel(context.Background())
		err := client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				displayQRCode(evt.Code)
			} else {
				fmt.Printf("Login event: %s\n", evt.Event)
			}
		}
	} else {
		fmt.Println("Already logged in, connecting...")
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		fmt.Println("Connected!")
	}

	if len(os.Args) > 2 {
		command := os.Args[1]
		if command == "send" && len(os.Args) >= 4 {
			phoneNumber := os.Args[2]
			message := os.Args[3]
			sendMessage(phoneNumber, message)
		} else if command == "send-image" && len(os.Args) >= 4 {
			phoneNumber := os.Args[2]
			filePath := os.Args[3]
			caption := ""
			if len(os.Args) > 4 {
				caption = os.Args[4]
			}
			sendMedia(phoneNumber, filePath, caption, whatsmeow.MediaImage)
		} else if command == "send-video" && len(os.Args) >= 4 {
			phoneNumber := os.Args[2]
			filePath := os.Args[3]
			caption := ""
			if len(os.Args) > 4 {
				caption = os.Args[4]
			}
			sendMedia(phoneNumber, filePath, caption, whatsmeow.MediaVideo)
		} else if command == "send-audio" && len(os.Args) >= 4 {
			phoneNumber := os.Args[2]
			filePath := os.Args[3]
			sendMedia(phoneNumber, filePath, "", whatsmeow.MediaAudio)
		} else if command == "send-file" && len(os.Args) >= 4 {
			phoneNumber := os.Args[2]
			filePath := os.Args[3]
			caption := ""
			if len(os.Args) > 4 {
				caption = os.Args[4]
			}
			sendMedia(phoneNumber, filePath, caption, whatsmeow.MediaDocument)
		} else {
	// Determine contacts file path dynamically
	exePath, _ := os.Executable()
	dirPath := filepath.Dir(exePath)
	contactsFile := filepath.Join(dirPath, "contacts.txt")
	if customPath := os.Getenv("CONTACTS_FILE_PATH"); customPath != "" {
		contactsFile = customPath
	}
	fmt.Printf("Resolved contacts.txt path: %s\n", contactsFile)
	if _, err := os.Stat(contactsFile); err != nil {
		fmt.Printf("Contacts file not found: %s\n", contactsFile)
		os.Exit(1)
	}

	printUsage()
		}
		return
	}

	printUsage()
}

func printUsage() {
	fmt.Println("\nUsage:")
	fmt.Println("  ./wsend                    - Login/scan QR code")
	fmt.Println("  ./wsend send <phone> <msg>   - Send text message")
	fmt.Println("  ./wsend send-image <phone> <file> [caption] - Send image")
	fmt.Println("  ./wsend send-video <phone> <file> [caption] - Send video")
	fmt.Println("  ./wsend send-audio <phone> <file>   - Send audio")
	fmt.Println("  ./wsend send-file <phone> <file> [caption]  - Send document")
	fmt.Println("\nExamples:")
	fmt.Println("  ./wsend send 1234567890 \"Hello!\"")
	fmt.Println("  ./wsend send-image 1234567890 photo.jpg \"Check this out\"")
	fmt.Println("  ./wsend send-video 1234567890 video.mp4")
	fmt.Println("  ./wsend send-audio 1234567890 voice.ogg")
	fmt.Println("  ./wsend send-file 1234567890 document.pdf")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
	fmt.Println("Disconnected")
}

func sendMessage(phoneNumber, message string) {
	recipient := types.NewJID(phoneNumber, "s.whatsapp.net")

	_, err := client.SendMessage(context.Background(), recipient, &waE2E.Message{
		Conversation: &message,
	})
	if err != nil {
		fmt.Printf("Error sending message: %v\n", err)
	} else {
		fmt.Printf("Message sent to %s: %s\n", recipient, message)
	}

	client.Disconnect()
}

func sendMedia(phoneNumber, filePath, caption string, mediaType whatsmeow.MediaType) {
	recipient := types.NewJID(phoneNumber, "s.whatsapp.net")

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return
	}

	uploaded, err := client.Upload(context.Background(), data, mediaType)
	if err != nil {
		fmt.Printf("Error uploading media: %v\n", err)
		return
	}

	mimeType := detectMimeType(filePath)
	fileName := filepath.Base(filePath)

	switch mediaType {
	case whatsmeow.MediaImage:
		msg := &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      proto.String(mimeType),
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:   uploaded.FileSHA256,
				FileLength:   proto.Uint64(uploaded.FileLength),
			},
		}
		if caption != "" {
			msg.ImageMessage.Caption = proto.String(caption)
		}
		_, err = client.SendMessage(context.Background(), recipient, msg)
	case whatsmeow.MediaVideo:
		msg := &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      proto.String(mimeType),
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:   uploaded.FileSHA256,
				FileLength:   proto.Uint64(uploaded.FileLength),
			},
		}
		if caption != "" {
			msg.VideoMessage.Caption = proto.String(caption)
		}
		_, err = client.SendMessage(context.Background(), recipient, msg)
	case whatsmeow.MediaAudio:
		msg := &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      proto.String(mimeType),
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:   uploaded.FileSHA256,
				FileLength:   proto.Uint64(uploaded.FileLength),
			},
		}
		_, err = client.SendMessage(context.Background(), recipient, msg)
	case whatsmeow.MediaDocument:
		msg := &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      proto.String(mimeType),
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:   uploaded.FileSHA256,
				FileLength:   proto.Uint64(uploaded.FileLength),
				FileName:     proto.String(fileName),
			},
		}
		if caption != "" {
			msg.DocumentMessage.Caption = proto.String(caption)
		}
		_, err = client.SendMessage(context.Background(), recipient, msg)
	}

	if err != nil {
		fmt.Printf("Error sending media: %v\n", err)
	} else {
		fmt.Printf("Media sent to %s: %s\n", recipient, filePath)
	}

	client.Disconnect()
}

func detectMimeType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".3gp":
		return "video/3gpp"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg; codecs=opus"
	case ".pdf":
		return "application/pdf"
	case ".doc", ".docx":
		return "application/msword"
	case ".xls", ".xlsx":
		return "application/vnd.ms-excel"
	case ".zip":
		return "application/zip"
	default:
		data, _ := os.ReadFile(filePath)
		return http.DetectContentType(data)
	}
}